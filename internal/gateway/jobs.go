// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Rainer Sestig. Kommerzielle Lizenz: siehe COMMERCIAL.md.

package gateway

import (
	"context"
	"errors"
	"net/http"

	"github.com/rainer-sharp/Ai-Swarms.net/internal/jobs"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/orchestrator"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/protocol"
)

// Asynchrone Aufträge (spec/cooperation.md § 7): POST /v1/jobs nimmt eine
// Anfrage im Format der Chat-API an und antwortet sofort mit der Kennung;
// GET /v1/jobs/{id} liefert Zustand und, wenn fertig, die Antwort im Format
// der Chat-API; DELETE bricht ab oder löscht.

// JobBackend: optional; ohne gibt es keine /v1/jobs.
type JobBackend interface {
	// SubmitJob: coordinator ist die Peer-ID eines vertrauten Peers, der den
	// Auftrag koordiniert; leer: dieser Knoten. substitute: Ersatzkoordinator
	// (Peer-ID oder auto), der bei einem Ausfall übernimmt; leer: keiner.
	SubmitJob(req orchestrator.Request, model, coordinator, substitute string) (jobs.Job, error)
	// Job: Ist der Koordinator nicht erreichbar, kommt der letzte bekannte
	// Stand mit einem Fehler.
	Job(ctx context.Context, id string) (jobs.Job, error)
	ListJobs() []jobs.Job
	// CancelJob bricht einen laufenden Auftrag ab oder löscht einen
	// abgeschlossenen (removed).
	CancelJob(ctx context.Context, id string) (j jobs.Job, removed bool, err error)
}

func jobStatusCode(err error) int {
	switch {
	case errors.Is(err, jobs.ErrUnknown):
		return http.StatusNotFound
	case errors.Is(err, jobs.ErrInvalid), errors.Is(err, orchestrator.ErrPrivacy):
		return http.StatusBadRequest
	}
	return http.StatusBadGateway
}

// jobJSON: Darstellung eines Auftrags; withResult: mit der Antwort.
func jobJSON(j jobs.Job, withResult bool, coordErr error) map[string]any {
	m := map[string]any{"id": j.ID, "object": "job", "status": j.Status, "created": j.Created.Unix(), "model": j.Model,
		"coordinator": "local"}
	if j.Coordinator != "" {
		m["coordinator"] = j.Coordinator
	}
	if j.Substitute != "" {
		m["substitute"] = j.Substitute
	}
	if !j.Finished.IsZero() {
		m["finished"] = j.Finished.Unix()
	}
	if j.Error != "" {
		m["error"] = j.Error
	}
	if coordErr != nil {
		m["coordinator_error"] = coordErr.Error()
	}
	if withResult && j.Status == protocol.JobDone && j.Answer != nil {
		ans := *j.Answer
		ans.Content, ans.ToolCalls = j.Content, j.ToolCalls
		m["result"] = completion(&ans, j.Model, j.Finished)
	} else if withResult && j.Answer != nil {
		m["swarmainet"] = j.Answer // Ablauf und Einschränkungen auch ohne Ergebnis
	}
	return m
}

func handleJobs(mux *http.ServeMux, b JobBackend) {
	mux.HandleFunc("POST /v1/jobs", func(w http.ResponseWriter, r *http.Request) {
		req, oreq, ok := parseChat(w, r)
		if !ok {
			return
		}
		j, err := b.SubmitJob(oreq, req.Model, r.Header.Get("X-SwarmAiNet-Coordinator"), r.Header.Get("X-SwarmAiNet-Substitute"))
		if err != nil {
			writeErr(w, jobStatusCode(err), err)
			return
		}
		writeJSON(w, http.StatusAccepted, jobJSON(j, false, nil))
	})
	mux.HandleFunc("GET /v1/jobs", func(w http.ResponseWriter, _ *http.Request) {
		data := []map[string]any{}
		for _, j := range b.ListJobs() {
			data = append(data, jobJSON(j, false, nil))
		}
		writeJSON(w, http.StatusOK, map[string]any{"object": "list", "data": data})
	})
	mux.HandleFunc("GET /v1/jobs/{id}", func(w http.ResponseWriter, r *http.Request) {
		j, err := b.Job(r.Context(), r.PathValue("id"))
		if err != nil && j.ID == "" {
			writeErr(w, jobStatusCode(err), err)
			return
		}
		writeJSON(w, http.StatusOK, jobJSON(j, true, err))
	})
	mux.HandleFunc("DELETE /v1/jobs/{id}", func(w http.ResponseWriter, r *http.Request) {
		j, removed, err := b.CancelJob(r.Context(), r.PathValue("id"))
		switch {
		case err != nil:
			writeErr(w, jobStatusCode(err), err)
		case removed:
			w.WriteHeader(http.StatusNoContent)
		default:
			writeJSON(w, http.StatusOK, jobJSON(j, false, nil))
		}
	})
}
