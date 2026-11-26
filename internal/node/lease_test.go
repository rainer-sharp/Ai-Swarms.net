// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Rainer Sestig. Kommerzielle Lizenz: siehe COMMERCIAL.md.

package node

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rainer-sharp/Ai-Swarms.net/internal/config"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/identity"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/orchestrator"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/protocol"
)

// hangBackend antwortet auf Chat-Anfragen erst, wenn der Client abbricht.
func hangBackend(t *testing.T, model string) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]string{{"id": model}}})
			return
		}
		_, _ = io.Copy(io.Discard, r.Body) // sonst merkt der Server den Abbruch nicht
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)
	return srv
}

// fastLease verkürzt die Zeiten des Leases für einen Test.
func fastLease(t *testing.T) {
	d, r, w := leaseDuration, leaseRenew, leaseWatch
	leaseDuration, leaseRenew, leaseWatch = 600*time.Millisecond, 100*time.Millisecond, 50*time.Millisecond
	t.Cleanup(func() { leaseDuration, leaseRenew, leaseWatch = d, r, w })
}

// leaseTrio: Auftraggeber r (ohne Modell), Koordinator c (Modell hängt) und
// Ersatz s (Modell antwortet), alle einander vertraut und verbunden.
func leaseTrio(t *testing.T) (r, c, s testNode, idS *identity.Identity) {
	ctx := t.Context()
	idR, _ := identity.New()
	idC, _ := identity.New()
	idS, _ = identity.New()
	hang := hangBackend(t, "koord")
	coordinate := func(cfg *config.Config) { cfg.Cooperation.CoordinateForOthers = true }
	r = newTestNodeWith(t, ctx, idR, "eigen", "mistral", nil, 8000, false, nil, idC, idS)
	c = newTestNodeWith(t, ctx, idC, "koord", "qwen", nil, 8000, false, func(cfg *config.Config) {
		coordinate(cfg)
		cfg.Backends[0].URL = hang.URL + "/v1"
	}, idR, idS)
	s = newTestNodeWith(t, ctx, idS, "ersatz", "llama", nil, 8000, false, coordinate, idR, idC)
	for _, pair := range [][2]testNode{{r, c}, {r, s}, {c, s}} {
		if err := pair[0].P2P.Connect(ctx, pair[1].P2P.Addrs()[0]); err != nil {
			t.Fatal(err)
		}
	}
	return r, c, s, idS
}

var leaseHello = orchestrator.Request{Messages: []protocol.ChatMessage{{Role: "user", Content: "Hallo"}},
	Pattern: orchestrator.PatternSingle, Privacy: protocol.PrivacyTrusted}

// TestLeaseTakeover: Der Koordinator fällt aus; der Ersatz übernimmt nach
// Ablauf des Leases, und der Auftraggeber holt das Ergebnis dort ab.
func TestLeaseTakeover(t *testing.T) {
	fastLease(t)
	r, c, s, idS := leaseTrio(t)
	j, err := r.SubmitJob(leaseHello, "", c.P2P.ID().String(), idS.ID.String())
	if err != nil {
		t.Fatal(err)
	}
	// Der Ersatz hält den Prüfpunkt, und der Auftraggeber erfährt davon.
	waitFor(t, func() bool {
		got, _ := r.Job(t.Context(), j.ID)
		return got.Substitute == idS.ID.String()
	})
	if l := s.heldLease(r.P2P.ID().String(), j.ID); l == nil || l.epoch != 1 || l.taken {
		t.Fatalf("prüfpunkt beim ersatz: %+v", l)
	}

	_ = c.Close() // Koordinator fällt aus
	j = waitJob(t, r, j.ID)
	if j.Status != protocol.JobDone || j.Content == "" || s.calls.Load() == 0 {
		t.Fatalf("nach übernahme: %+v, aufrufe ersatz %d", j, s.calls.Load())
	}
	if got, _ := s.jobs.Get(r.P2P.ID().String(), j.ID); got.Epoch != 2 || got.Status != protocol.JobDone {
		t.Fatalf("beim ersatz: %+v", got)
	}
}

// TestLeaseCoordinatorReturns: Der Ersatz hat übernommen, während der
// Koordinator noch lief (Netztrennung). Bei der nächsten Verlängerung erfährt
// der Koordinator davon und bricht ab; der Auftraggeber bekommt das Ergebnis
// des Ersatzes.
func TestLeaseCoordinatorReturns(t *testing.T) {
	fastLease(t)
	r, c, s, idS := leaseTrio(t)
	owner := r.P2P.ID().String()
	j, err := r.SubmitJob(leaseHello, "", c.P2P.ID().String(), idS.ID.String())
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return s.heldLease(owner, j.ID) != nil })
	// Netztrennung nachstellen: Der Lease läuft beim Ersatz ab.
	s.heldMu.Lock()
	s.held[owner+"/"+j.ID].expiry = time.Now().Add(-time.Second)
	s.heldMu.Unlock()
	// Der Koordinator hat abgebrochen und verweist auf den Ersatz.
	waitFor(t, func() bool { got, _ := c.jobs.Get(owner, j.ID); return got.Coordinator == idS.ID.String() })
	if j = waitJob(t, r, j.ID); j.Status != protocol.JobDone || s.calls.Load() == 0 {
		t.Fatalf("%+v", j)
	}
}

// TestLeaseResultAndCancel: Ist der Auftrag beim Koordinator fertig, hält der
// Ersatz das Ergebnis und übernimmt nicht. Ein Abbruch beim Ersatz verhindert
// die Übernahme.
func TestLeaseResultAndCancel(t *testing.T) {
	fastLease(t)
	r, c, s, idS := leaseTrio(t)
	owner := r.P2P.ID().String()

	// Ergebnis: Ein lokaler Auftrag von c mit Ersatz s; c bricht ihn ab, das
	// Ergebnis (canceled) geht an s.
	jc, err := c.SubmitJob(leaseHello, "", "", idS.ID.String())
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return s.heldLease(c.P2P.ID().String(), jc.ID) != nil })
	if _, _, err := c.CancelJob(t.Context(), jc.ID); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		l := s.heldLease(c.P2P.ID().String(), jc.ID)
		s.heldMu.Lock()
		defer s.heldMu.Unlock()
		return l.result != nil && l.result.Status == protocol.JobCanceled
	})
	time.Sleep(2 * leaseDuration)
	if l := s.heldLease(c.P2P.ID().String(), jc.ID); l.taken {
		t.Fatal("ersatz übernimmt einen abgeschlossenen auftrag")
	}

	// Abbruch beim Ersatz, während der Koordinator nicht erreichbar ist.
	j, err := r.SubmitJob(leaseHello, "", c.P2P.ID().String(), idS.ID.String())
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		got, _ := r.Job(t.Context(), j.ID)
		return got.Substitute != ""
	})
	_ = c.Close()
	if st := (jobCoordinator{s.Node}).CancelJob(owner, j.ID); st.Status != protocol.JobCanceled {
		t.Fatalf("abbruch beim ersatz: %+v", st)
	}
	time.Sleep(2 * leaseDuration)
	if l := s.heldLease(owner, j.ID); l.taken {
		t.Fatal("ersatz übernimmt einen abgebrochenen auftrag")
	}
}
