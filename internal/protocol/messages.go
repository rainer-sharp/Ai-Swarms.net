// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Rainer Sestig. Kommerzielle Lizenz: siehe COMMERCIAL.md.

package protocol

import (
	"encoding/json"
	"errors"
	"slices"
	"time"
)

// Freigabeklassen (spec/privacy-classes.md). Die Reihenfolge ist relevant:
// eine Delegation darf nur gleich streng oder strenger sein.
type PrivacyClass string

const (
	PrivacyLocal   PrivacyClass = "local"
	PrivacyTrusted PrivacyClass = "trusted"
	PrivacyReduced PrivacyClass = "reduced"
	PrivacyPublic  PrivacyClass = "public"
)

func (p PrivacyClass) rank() int {
	switch p {
	case PrivacyLocal:
		return 0
	case PrivacyTrusted:
		return 1
	case PrivacyReduced:
		return 2
	case PrivacyPublic:
		return 3
	}
	return -1
}

func (p PrivacyClass) Valid() bool { return p.rank() >= 0 }

// AtLeastAsStrict meldet, ob p mindestens so streng ist wie q.
func (p PrivacyClass) AtLeastAsStrict(q PrivacyClass) bool { return p.rank() <= q.rank() }

type Role string

const (
	RoleRouter      Role = "router"
	RolePlanner     Role = "planner"
	RoleSpecialist  Role = "specialist"
	RoleVerifier    Role = "verifier"
	RoleSynthesizer Role = "synthesizer"
	RoleTool        Role = "tool"
)

// Capability ist die signierte, ablaufende Fähigkeitsanzeige.
// Sie enthält keine Prompts, internen IPs oder genauen Hardware-/Energiedaten.
type Capability struct {
	PeerID       string       `json:"peer_id"`
	ExpiresAt    time.Time    `json:"expires_at"`
	Agents       []AgentInfo  `json:"agents"`
	Availability Availability `json:"availability"`
	Policy       Policy       `json:"policy"`
}

type AgentInfo struct {
	AgentID     string   `json:"agent_id"`
	ModelFamily string   `json:"model_family"`
	Skills      []string `json:"skills"`
	Modalities  []string `json:"modalities"`
	ContextMax  int      `json:"context_max"`
	// Strength: Selbsteinschätzung des Anbieters, 1–10, 0 = unbekannt.
	Strength int `json:"strength,omitempty"`
	// Tools: Das Modell beherrscht Werkzeug-Aufrufe (OpenAI tools).
	Tools bool `json:"tools,omitempty"`
}

type Availability struct {
	SlotsNow int      `json:"slots_now"`
	Windows  []Window `json:"windows,omitempty"`
}

type Window struct {
	From  string `json:"from"`
	To    string `json:"to"`
	Slots int    `json:"slots"`
}

type Policy struct {
	Accepts   []string `json:"accepts"`   // "trusted", "public"
	Retention string   `json:"retention"` // z. B. "no-prompt-storage"
}

type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
	// Werkzeuge im OpenAI-Format: Aufrufe des Modells (role "assistant")
	// und Ergebnisse dazu (role "tool").
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	Name       string     `json:"name,omitempty"`
	// Images: eingebettete Bilder (Nachrichten mit role "user"); nur an
	// Agenten mit der Modalität "image", bei reduced nie an Fremde
	// (spec/protocol-v0.1.md § 4.2).
	Images []Image `json:"images,omitempty"`
}

// Image ist ein eingebettetes Bild.
type Image struct {
	MIME string `json:"mime"` // image/png, image/jpeg, image/webp, image/gif
	Data []byte `json:"data"` // im JSON Base64
}

// MaxImageBytes begrenzt alle Bilder eines Teilauftrags zusammen, aus
// demselben Grund wie MaxAudioBytes.
const MaxImageBytes = 2816 << 10 // 2,75 MiB

// ImageMIME: Bildformate, die der Schwarm weiterreicht.
var ImageMIME = []string{"image/png", "image/jpeg", "image/webp", "image/gif"}

// ValidImages: alle Bilder haben Daten und ein unterstütztes Format.
func ValidImages(msgs []ChatMessage) bool {
	for _, m := range msgs {
		for _, img := range m.Images {
			if len(img.Data) == 0 || !slices.Contains(ImageMIME, img.MIME) {
				return false
			}
		}
	}
	return true
}

// ImageBytes zählt die Bilddaten aller Nachrichten.
func ImageBytes(msgs []ChatMessage) int {
	n := 0
	for _, m := range msgs {
		for _, img := range m.Images {
			n += len(img.Data)
		}
	}
	return n
}

// ToolCall ist ein Werkzeug-Aufruf des Modells. Ausgeführt wird er nie im
// Schwarm, sondern von der aufrufenden Anwendung (z. B. einem Coding-Agenten).
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"` // "function"
	Function ToolFunction `json:"function"`
}

type ToolFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // JSON als Text, wie bei OpenAI
}

// Limits gelten pro Auftragsgraph; Delegationen erben gleiche oder strengere Werte.
type Limits struct {
	Deadline     time.Time    `json:"deadline"`
	MaxDepth     int          `json:"max_depth"`
	MaxSubtasks  int          `json:"max_subtasks"`
	MaxTokens    int          `json:"max_tokens"`
	MaxBytes     int          `json:"max_bytes"`
	PrivacyClass PrivacyClass `json:"privacy_class"`
	Depth        int          `json:"depth"`
}

type ProposePayload struct {
	SubtaskID string        `json:"subtask_id"`
	Role      Role          `json:"role"`
	Skill     string        `json:"skill,omitempty"`
	AgentID   string        `json:"agent_id"`
	Messages  []ChatMessage `json:"messages"`
	Limits    Limits        `json:"limits"`
	// JSONSchema optional: erwartete strukturierte Ausgabe.
	JSONSchema string `json:"json_schema,omitempty"`
	// Tools und ToolChoice im OpenAI-Format, unverändert weitergereicht.
	Tools      json.RawMessage `json:"tools,omitempty"`
	ToolChoice json.RawMessage `json:"tool_choice,omitempty"`
	// Audio: Aufnahme für eine Transkription (Fähigkeit audio.transcribe);
	// nie in der Freigabeklasse public (spec/protocol-v0.1.md § 4.2).
	Audio *Audio `json:"audio,omitempty"`
	// Stream: Der Koordinator möchte Zwischenstände als TASK_PROGRESS mit
	// delta/reasoning. Maßgeblich bleibt das Ergebnis (spec/protocol-v0.1.md § 4.2).
	Stream bool `json:"stream,omitempty"`
	// Job: asynchroner Auftrag mit Lease, zu dem der Teilauftrag gehört.
	// Anbieter lehnen Teilaufträge einer älteren Epoche ab, sobald sie eine
	// neuere gesehen haben (spec/cooperation.md § 7.3).
	Job *JobRef `json:"job,omitempty"`
}

// JobRef: Kennung und Lease-Epoche eines asynchronen Auftrags.
type JobRef struct {
	ID    string `json:"id"`
	Epoch int    `json:"epoch"`
}

// MaxAudioBytes begrenzt eine Aufnahme so, dass die Nachricht mit Base64 unter
// der Grenze von 4 MiB bleibt.
const MaxAudioBytes = 2816 << 10 // 2,75 MiB

// Audio ist eine Aufnahme mit den Angaben der OpenAI-Transkriptions-API.
type Audio struct {
	Data     []byte `json:"data"` // im JSON Base64
	MIME     string `json:"mime,omitempty"`
	Filename string `json:"filename,omitempty"`
	Language string `json:"language,omitempty"` // ISO-639-1, optional
	Prompt   string `json:"prompt,omitempty"`   // Hinweise zu Begriffen, optional
}

type BidPayload struct {
	SubtaskID    string `json:"subtask_id"`
	SlotsFree    int    `json:"slots_free"`
	ExpectedSecs int    `json:"expected_secs"`
}

type AcceptPayload struct {
	SubtaskID   string    `json:"subtask_id"`
	LeaseExpiry time.Time `json:"lease_expiry"`
}

type RejectPayload struct {
	SubtaskID string `json:"subtask_id"`
	Reason    string `json:"reason"`
}

// ReasonTokenLimit: Abbruchgrund (TASK_CANCEL), wenn das Modell max_tokens
// erreicht hat, bevor eine Antwort kam, typisch bei Reasoning-Modellen. Der
// Grund nennt keine Inhalte; der Auftraggeber kann ihn dem Nutzer zeigen.
const ReasonTokenLimit = "token-limit erreicht"

// ErrTokenLimit: lokaler wie entfernter Abbruch aus diesem Grund.
var ErrTokenLimit = errors.New(ReasonTokenLimit)

type CancelPayload struct {
	SubtaskID string `json:"subtask_id"`
	Reason    string `json:"reason"`
}

type ProgressPayload struct {
	SubtaskID string `json:"subtask_id"`
	Status    string `json:"status"`
	// Delta: neuer Text der Antwort seit dem letzten Fortschritt; Reasoning:
	// neuer Text der Denkphase des Modells (nur bei ProposePayload.Stream).
	Delta     string `json:"delta,omitempty"`
	Reasoning string `json:"reasoning,omitempty"`
}

// ProgressDelta: Status eines Fortschritts mit gestreamtem Text.
const ProgressDelta = "delta"

// Ergebnisstatus
const (
	StatusOK      = "ok"
	StatusPartial = "partial"
	StatusFailed  = "failed"
)

// Result trennt Antwort, Evidenz, Einschränkungen und Konfidenz.
type Result struct {
	TaskID       string      `json:"task_id"`
	SubtaskID    string      `json:"subtask_id"`
	ProducerPeer string      `json:"producer_peer"`
	AgentID      string      `json:"agent_id"`
	ModelFamily  string      `json:"model_family"`
	Role         Role        `json:"role"`
	Result       ResultBody  `json:"result"`
	Evidence     []Evidence  `json:"evidence,omitempty"`
	Confidence   *Confidence `json:"confidence,omitempty"`
	Limitations  []string    `json:"limitations,omitempty"`
	Usage        Usage       `json:"usage"`
}

type ResultBody struct {
	Status    string     `json:"status"`
	Summary   string     `json:"summary,omitempty"`
	Content   string     `json:"content"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
}

type Evidence struct {
	Type   string `json:"type"`
	Digest string `json:"digest"`
}

type Confidence struct {
	Value float64 `json:"value"`
	Kind  string  `json:"kind"` // "self-reported" | "measured"
}

type Usage struct {
	PromptTokens     int   `json:"prompt_tokens"`
	CompletionTokens int   `json:"completion_tokens"`
	DurationMs       int64 `json:"duration_ms"`
}
