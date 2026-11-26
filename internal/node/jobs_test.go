// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Rainer Sestig und Mitwirkende.

package node

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rainer-sharp/Ai-Swarms.net/internal/config"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/identity"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/jobs"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/orchestrator"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/protocol"
)

// waitJob wartet, bis der eigene Auftrag abgeschlossen ist.
func waitJob(t *testing.T, n testNode, id string) jobs.Job {
	t.Helper()
	var j jobs.Job
	waitFor(t, func() bool {
		var err error
		j, err = n.Job(t.Context(), id)
		if err != nil && !errors.Is(err, jobs.ErrCoordinator) {
			t.Fatal(err)
		}
		return protocol.JobFinal(j.Status)
	})
	return j
}

// TestJobs: Ein Auftrag läuft im Hintergrund, auf diesem Knoten oder bei
// einem vertrauten Koordinator, der ihn mit seinen Modellen bearbeitet; das
// Ergebnis wird später abgeholt. Ein Knoten ohne coordinate_for_others lehnt
// ab, local verlässt den Knoten nie.
func TestJobs(t *testing.T) {
	ctx := t.Context()
	idA, _ := identity.New()
	idB, _ := identity.New()
	idC, _ := identity.New()
	a := newTestNodeWith(t, ctx, idA, "eigen", "mistral", nil, 8000, false, nil, idB, idC)
	b := newTestNodeWith(t, ctx, idB, "koord", "qwen", nil, 8000, false, func(c *config.Config) {
		c.Cooperation.CoordinateForOthers = true
	}, idA)
	c := newTestNodeWith(t, ctx, idC, "ohne", "llama", nil, 8000, false, nil, idA)
	for _, x := range []testNode{b, c} {
		if err := a.P2P.Connect(ctx, x.P2P.Addrs()[0]); err != nil {
			t.Fatal(err)
		}
	}
	question := orchestrator.Request{Messages: []protocol.ChatMessage{{Role: "user", Content: "Hallo"}},
		Pattern: orchestrator.PatternSingle, Privacy: protocol.PrivacyTrusted}

	// Auf diesem Knoten: mit dem eigenen Modell.
	j, err := a.SubmitJob(question, "swarmainet/single", "", "")
	if err != nil || j.Status != protocol.JobRunning || j.Coordinator != "" {
		t.Fatalf("%v %+v", err, j)
	}
	if j = waitJob(t, a, j.ID); j.Status != protocol.JobDone || j.Content == "" || j.Answer == nil || a.calls.Load() != 1 {
		t.Fatalf("lokal: %+v, aufrufe %d", j, a.calls.Load())
	}

	// Beim Koordinator: Er bearbeitet den Auftrag (sein Modell ist das einzige,
	// das er kennt; a bietet seins nicht an).
	j, err = a.SubmitJob(question, "swarmainet/single", idB.ID.String(), "")
	if err != nil || j.Coordinator != idB.ID.String() {
		t.Fatalf("%v %+v", err, j)
	}
	j = waitJob(t, a, j.ID)
	if j.Status != protocol.JobDone || j.Content == "" || j.Answer == nil || b.calls.Load() != 1 || a.calls.Load() != 1 {
		t.Fatalf("entfernt: %+v, aufrufe a %d b %d", j, a.calls.Load(), b.calls.Load())
	}
	if got := b.jobs.List(idA.ID.String()); len(got) != 1 || got[0].ID != j.ID || len(b.ListJobs()) != 0 {
		t.Fatalf("beim koordinator: %+v", got)
	}

	// Kennt der Koordinator den Auftrag nicht mehr (Neustart), ist er gescheitert.
	j2, err := a.SubmitJob(question, "", idB.ID.String(), "")
	if err != nil {
		t.Fatal(err)
	}
	b.jobs.Remove(idA.ID.String(), j2.ID)
	// Der Auftrag lief womöglich schon zu Ende; dann ist er abgeschlossen, sonst unbekannt.
	if j2 = waitJob(t, a, j2.ID); j2.Status == protocol.JobFailed && !strings.Contains(j2.Error, "kennt den auftrag nicht") {
		t.Fatalf("%+v", j2)
	}

	// Abgeschlossene Aufträge löscht DELETE.
	if _, removed, err := a.CancelJob(ctx, j.ID); err != nil || !removed {
		t.Fatalf("löschen: %v %v", removed, err)
	}
	if _, err := a.Job(ctx, j.ID); !errors.Is(err, jobs.ErrUnknown) {
		t.Fatalf("gelöscht: %v", err)
	}

	// Ablehnungen.
	if _, err := a.SubmitJob(question, "", idC.ID.String(), ""); !errors.Is(err, jobs.ErrCoordinator) || !strings.Contains(err.Error(), "coordinate_for_others") {
		t.Fatalf("ohne coordinate_for_others: %v", err)
	}
	local := question
	local.Privacy = protocol.PrivacyLocal
	if _, err := a.SubmitJob(local, "", idB.ID.String(), ""); !errors.Is(err, orchestrator.ErrPrivacy) {
		t.Fatalf("local: %v", err)
	}
	if _, err := a.SubmitJob(question, "", "kein-peer", ""); !errors.Is(err, jobs.ErrInvalid) {
		t.Fatalf("ungültige peer-id: %v", err)
	}
	other, _ := identity.New()
	if _, err := a.SubmitJob(question, "", other.ID.String(), ""); !errors.Is(err, jobs.ErrCoordinator) {
		t.Fatalf("unbekannter koordinator: %v", err)
	}
	// Nur der Auftraggeber sieht seinen Auftrag beim Koordinator.
	if st := (jobCoordinator{b.Node}).JobStatus(idC.ID.String(), j.ID); st.Status != protocol.JobUnknown {
		t.Fatalf("fremder sieht auftrag: %+v", st)
	}
}

// TestJobCancel: Ein laufender Auftrag lässt sich abbrechen, auch beim
// Koordinator.
func TestJobCancel(t *testing.T) {
	ctx := t.Context()
	idA, _ := identity.New()
	idB, _ := identity.New()
	// Ein Backend, das erst antwortet, wenn der Auftrag abgebrochen ist.
	hang := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]string{{"id": "koord"}}})
			return
		}
		// Erst nach dem Lesen des Bodys merkt der Server, wenn der Client abbricht.
		_, _ = io.Copy(io.Discard, r.Body)
		<-r.Context().Done()
	}))
	t.Cleanup(hang.Close)
	a := newTestNodeWith(t, ctx, idA, "eigen", "mistral", nil, 8000, false, nil, idB)
	b := newTestNodeWith(t, ctx, idB, "koord", "qwen", nil, 8000, false, func(c *config.Config) {
		c.Cooperation.CoordinateForOthers = true
		c.Backends[0].URL = hang.URL + "/v1"
	}, idA)
	if err := a.P2P.Connect(ctx, b.P2P.Addrs()[0]); err != nil {
		t.Fatal(err)
	}
	question := orchestrator.Request{Messages: []protocol.ChatMessage{{Role: "user", Content: "Hallo"}},
		Pattern: orchestrator.PatternSingle, Privacy: protocol.PrivacyTrusted}
	j, err := a.SubmitJob(question, "", idB.ID.String(), "")
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	j, removed, err := a.CancelJob(ctx, j.ID)
	if err != nil || removed || j.Status != protocol.JobCanceled {
		t.Fatalf("%v %v %+v", err, removed, j)
	}
	if got, _ := b.jobs.Get(idA.ID.String(), j.ID); got.Status != protocol.JobCanceled {
		t.Fatalf("beim koordinator: %+v", got)
	}
	waitFor(t, func() bool { return b.jobs.RunningFor("") == 0 })
}
