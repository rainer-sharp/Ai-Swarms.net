// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Rainer Sestig. Kommerzielle Lizenz: siehe COMMERCIAL.md.

package protocol

import (
	"encoding/json"
	"time"
)

// Asynchrone Aufträge (spec/cooperation.md § 7): Ein Auftraggeber gibt einen
// ganzen Auftrag an einen vertrauten Peer, der ihn koordiniert, und holt das
// Ergebnis später ab. Der Koordinator sieht den Auftrag im Klartext.

// JobRequest: der Auftrag, wie ihn das Gateway des Auftraggebers annahm,
// mit dessen Standardwerten ergänzt.
type JobRequest struct {
	Messages          []ChatMessage   `json:"messages"`
	Pattern           string          `json:"pattern,omitempty"` // leer: der Koordinator wählt
	PrivacyClass      PrivacyClass    `json:"privacy_class"`
	Skill             string          `json:"skill,omitempty"`
	Agents            []string        `json:"agents,omitempty"`
	Tools             json.RawMessage `json:"tools,omitempty"`
	ToolChoice        json.RawMessage `json:"tool_choice,omitempty"`
	MaxTokens         int             `json:"max_tokens,omitempty"`
	MaxSubtasks       int             `json:"max_subtasks,omitempty"`
	Deadline          time.Time       `json:"deadline"`
	PseudonymizeTerms []string        `json:"pseudonymize_terms,omitempty"`
	Trace             bool            `json:"trace,omitempty"`
	// Substitute: gewünschter Ersatzkoordinator (Peer-ID oder "auto");
	// leer: keiner. Er bekommt Prüfpunkte mit dem Auftrag im Klartext.
	Substitute string `json:"substitute,omitempty"`
}

type JobSubmitPayload struct {
	JobID   string     `json:"job_id"`
	Request JobRequest `json:"request"`
}

// JobQueryPayload: Abfrage (JOB_STATUS) oder Abbruch (JOB_CANCEL).
type JobQueryPayload struct {
	JobID string `json:"job_id"`
}

// Zustände eines asynchronen Auftrags.
const (
	JobRunning  = "running"
	JobDone     = "done"
	JobFailed   = "failed"
	JobCanceled = "canceled"
	JobRejected = "rejected" // nur als Antwort auf JOB_SUBMIT
	JobUnknown  = "unknown"  // Abfrage zu einem unbekannten Auftrag
)

// JobStatusPayload: Antwort des Koordinators. Bei done trägt sie die Antwort:
// Text, Werkzeug-Aufrufe und die Metadaten (das Feld swarmainet der
// Chat-Antwort) als JSON.
type JobStatusPayload struct {
	JobID     string          `json:"job_id"`
	Status    string          `json:"status"`
	Error     string          `json:"error,omitempty"`
	Content   string          `json:"content,omitempty"`
	ToolCalls []ToolCall      `json:"tool_calls,omitempty"`
	Answer    json.RawMessage `json:"answer,omitempty"`
	Finished  time.Time       `json:"finished,omitzero"`
	// Substitute: Ersatzkoordinator, der Prüfpunkte hält; bei ihm fragt der
	// Auftraggeber nach, wenn der Koordinator nicht antwortet.
	Substitute string `json:"substitute,omitempty"`
}

// LeaseCheckpointPayload: Prüfpunkt eines Auftrags für den Ersatzkoordinator.
// Owner: Auftraggeber; leer, wenn der Koordinator selbst der Auftraggeber ist.
// Plan und Results: bei decompose der Plan und die fertigen Teilergebnisse.
type LeaseCheckpointPayload struct {
	JobID   string            `json:"job_id"`
	Owner   string            `json:"owner,omitempty"`
	Epoch   int               `json:"epoch"`
	Expiry  time.Time         `json:"expiry"`
	Request JobRequest        `json:"request"`
	Plan    json.RawMessage   `json:"plan,omitempty"`
	Results map[string]string `json:"results,omitempty"`
}

// LeaseRenewPayload verlängert den Lease bis Expiry.
type LeaseRenewPayload struct {
	JobID  string    `json:"job_id"`
	Owner  string    `json:"owner,omitempty"`
	Epoch  int       `json:"epoch"`
	Expiry time.Time `json:"expiry"`
}

// LeaseResultPayload: Der Auftrag ist abgeschlossen; der Ersatz hält das
// Ergebnis für den Auftraggeber bereit und übernimmt nicht mehr.
type LeaseResultPayload struct {
	JobID  string           `json:"job_id"`
	Owner  string           `json:"owner,omitempty"`
	Epoch  int              `json:"epoch"`
	Result JobStatusPayload `json:"result"`
}

// LeaseAckPayload: Antwort des Ersatzes. TakenOver: Er hat den Auftrag mit
// Epoch übernommen; der bisherige Koordinator bricht ab.
type LeaseAckPayload struct {
	JobID     string `json:"job_id"`
	OK        bool   `json:"ok"`
	TakenOver bool   `json:"taken_over,omitempty"`
	Epoch     int    `json:"epoch,omitempty"`
	Error     string `json:"error,omitempty"`
}

// JobFinal: Der Auftrag ist abgeschlossen; der Zustand ändert sich nicht mehr.
func JobFinal(status string) bool {
	return status == JobDone || status == JobFailed || status == JobCanceled
}
