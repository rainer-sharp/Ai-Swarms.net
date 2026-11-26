// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Rainer Sestig. Kommerzielle Lizenz: siehe COMMERCIAL.md.

package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rainer-sharp/Ai-Swarms.net/internal/jobs"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/orchestrator"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/protocol"
)

// fakeJobs: Backend mit Aufträgen; „hängt“ bleibt laufend, alles andere ist
// sofort fertig.
type fakeJobs struct {
	fakeBackend
	store       *jobs.Store
	coordinator string
}

func (f *fakeJobs) SubmitJob(req orchestrator.Request, model, coordinator string) (jobs.Job, error) {
	switch coordinator {
	case "", "peer-ok":
	case "kaputt":
		return jobs.Job{}, fmt.Errorf("%w: keine peer-id", jobs.ErrInvalid)
	default:
		return jobs.Job{}, fmt.Errorf("%w: nicht verbunden", jobs.ErrCoordinator)
	}
	f.coordinator = coordinator
	j := jobs.Job{ID: jobs.NewID(), Model: model, Coordinator: coordinator, Status: protocol.JobRunning, Created: time.Now()}
	f.store.Add(j, nil)
	if req.Messages[0].Content != "hängt" {
		j, _ = f.store.Update("", j.ID, func(x *jobs.Job) {
			x.Status, x.Content = protocol.JobDone, "antwort"
			x.Answer = &orchestrator.Answer{TaskID: "t1", Pattern: req.Pattern}
		})
	}
	return j, nil
}

func (f *fakeJobs) Job(_ context.Context, id string) (jobs.Job, error) {
	j, ok := f.store.Get("", id)
	if !ok {
		return j, jobs.ErrUnknown
	}
	if j.Coordinator != "" && j.Status == protocol.JobRunning {
		return j, fmt.Errorf("%w: nicht erreichbar", jobs.ErrCoordinator)
	}
	return j, nil
}

func (f *fakeJobs) ListJobs() []jobs.Job { return f.store.List("") }

func (f *fakeJobs) CancelJob(_ context.Context, id string) (jobs.Job, bool, error) {
	j, ok := f.store.Get("", id)
	if !ok {
		return j, false, jobs.ErrUnknown
	}
	if protocol.JobFinal(j.Status) {
		return j, f.store.Remove("", id), nil
	}
	j, _ = f.store.Cancel("", id)
	return j, false, nil
}

func TestJobsAPI(t *testing.T) {
	b := &fakeJobs{store: jobs.Open("")}
	srv := httptest.NewServer(Handler(b))
	defer srv.Close()
	call := func(method, path, body string, header map[string]string) (int, map[string]any) {
		req, _ := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
		for k, v := range header {
			req.Header.Set(k, v)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var m map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&m)
		return resp.StatusCode, m
	}
	chat := func(text string) string {
		return `{"model":"swarmainet/decompose","messages":[{"role":"user","content":"` + text + `"}]}`
	}

	code, m := call("POST", "/v1/jobs", chat("frage"), nil)
	if code != http.StatusAccepted || m["object"] != "job" || m["coordinator"] != "local" || m["result"] != nil {
		t.Fatalf("%d %v", code, m)
	}
	id := m["id"].(string)
	code, m = call("GET", "/v1/jobs/"+id, "", nil)
	res, _ := m["result"].(map[string]any)
	if code != 200 || m["status"] != "done" || res == nil || res["object"] != "chat.completion" || res["model"] != "swarmainet/decompose" {
		t.Fatalf("%d %v", code, m)
	}
	if msg := res["choices"].([]any)[0].(map[string]any)["message"].(map[string]any); msg["content"] != "antwort" {
		t.Fatalf("antwort: %v", msg)
	}

	// Entfernt koordiniert, Koordinator gerade nicht erreichbar: letzter Stand mit Hinweis.
	code, m = call("POST", "/v1/jobs", chat("hängt"), map[string]string{"X-SwarmAiNet-Coordinator": "peer-ok"})
	if code != http.StatusAccepted || m["coordinator"] != "peer-ok" || b.coordinator != "peer-ok" {
		t.Fatalf("%d %v", code, m)
	}
	running := m["id"].(string)
	if code, m = call("GET", "/v1/jobs/"+running, "", nil); code != 200 || m["status"] != "running" || m["coordinator_error"] == nil {
		t.Fatalf("%d %v", code, m)
	}
	if code, m = call("GET", "/v1/jobs", "", nil); code != 200 || len(m["data"].([]any)) != 2 {
		t.Fatalf("liste %d %v", code, m)
	}

	// Abbrechen und löschen.
	if code, m = call("DELETE", "/v1/jobs/"+running, "", nil); code != 200 || m["status"] != "canceled" {
		t.Fatalf("abbruch %d %v", code, m)
	}
	if code, _ = call("DELETE", "/v1/jobs/"+running, "", nil); code != http.StatusNoContent {
		t.Fatalf("löschen %d", code)
	}
	if code, _ = call("GET", "/v1/jobs/"+running, "", nil); code != http.StatusNotFound {
		t.Fatalf("gelöscht %d", code)
	}

	// Fehler.
	for _, c := range []struct {
		coord string
		want  int
	}{{"kaputt", 400}, {"weg", 502}} {
		if code, _ = call("POST", "/v1/jobs", chat("x"), map[string]string{"X-SwarmAiNet-Coordinator": c.coord}); code != c.want {
			t.Errorf("%s: %d, erwartet %d", c.coord, code, c.want)
		}
	}
	if code, _ = call("POST", "/v1/jobs", `{"model":"swarmainet","messages":[]}`, nil); code != 400 {
		t.Errorf("ohne nachrichten: %d", code)
	}

	// Ein Backend ohne Aufträge hat keine /v1/jobs.
	plain := httptest.NewServer(Handler(&fakeBackend{}))
	defer plain.Close()
	resp, err := http.Post(plain.URL+"/v1/jobs", "application/json", strings.NewReader(chat("x")))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusAccepted {
		t.Fatal("/v1/jobs ohne JobBackend")
	}
}
