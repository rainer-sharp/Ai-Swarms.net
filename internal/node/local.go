// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Rainer Sestig. Kommerzielle Lizenz: siehe COMMERCIAL.md.

// Package node verbindet die Module eines Swarm-Knotens: Gateway, Auswahl,
// Orchestrierung, Ressourcenmanager, P2P-Transport und lokale Backends.
package node

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/rainer-sharp/Ai-Swarms.net/internal/backend"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/config"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/orchestrator"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/protocol"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/rem"
)

type localAgent struct {
	info   protocol.AgentInfo
	model  string
	client *backend.Client
	offer  bool
}

// Local führt Teilaufträge auf den eigenen Backends aus.
type Local struct {
	agents map[string]*localAgent
	order  []string
	rem    *rem.Manager
}

func agentID(backendName, model string) string { return backendName + "/" + model }

func NewLocal(backends []config.Backend, m *rem.Manager) *Local {
	l := &Local{agents: make(map[string]*localAgent), rem: m}
	for _, b := range backends {
		c := backend.New(b.URL, b.APIKey)
		for _, md := range b.Models {
			id := agentID(b.Name, md.Name)
			fam := md.Family
			if fam == "" {
				fam = guessFamily(md.Name)
			}
			skills := md.Skills
			if len(skills) == 0 {
				skills = []string{"general"}
			}
			l.agents[id] = &localAgent{
				info:   protocol.AgentInfo{AgentID: id, ModelFamily: fam, Skills: skills, Modalities: []string{"text"}, ContextMax: md.Context, Strength: md.Strength, Tools: md.Tools},
				model:  md.Name,
				client: c,
				offer:  md.Offer,
			}
			l.order = append(l.order, id)
		}
	}
	return l
}

// guessFamily leitet die Modellfamilie aus dem Namen ab (z. B. "qwen2.5-coder:7b" → "qwen").
func guessFamily(model string) string {
	name := strings.ToLower(model)
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	for _, f := range []string{"qwen", "llama", "mistral", "mixtral", "gemma", "phi", "deepseek", "granite", "command", "olmo", "falcon", "yi", "glm"} {
		if strings.HasPrefix(name, f) || strings.HasPrefix(name, "code"+f) {
			return f
		}
	}
	name, _, _ = strings.Cut(name, ":")
	return strings.TrimRight(name, "0123456789.-_")
}

// Agents liefert alle lokalen Agenten (für eigene Aufträge).
func (l *Local) Agents() []orchestrator.Agent {
	out := make([]orchestrator.Agent, 0, len(l.order))
	for _, id := range l.order {
		a := l.agents[id]
		out = append(out, orchestrator.Agent{ID: id, Local: true, Trusted: true, Family: a.info.ModelFamily, Skills: a.info.Skills, ContextMax: a.info.ContextMax, Strength: a.info.Strength, Tools: a.info.Tools})
	}
	return out
}

// Offer liefert nur die zur Bereitstellung freigegebenen Agenten.
func (l *Local) Offer() []protocol.AgentInfo {
	var out []protocol.AgentInfo
	for _, id := range l.order {
		if a := l.agents[id]; a.offer {
			out = append(out, a.info)
		}
	}
	return out
}

func (l *Local) SlotsNow() int { return l.rem.SlotsNow() }

func (l *Local) Admit(public bool) (*rem.Slot, error) {
	return l.rem.Admit(rem.AdmitRequest{Public: public})
}

// withSchema ergänzt den Schema-Hinweis. Steht schon eine System-Nachricht am
// Anfang, wird er dort angehängt: manche Chat-Templates (z. B. Qwen in vLLM)
// lehnen mehr als eine System-Nachricht ab.
func withSchema(msgs []protocol.ChatMessage, schema string) []protocol.ChatMessage {
	if schema == "" {
		return msgs
	}
	hint := "Antworte ausschließlich mit einem JSON-Objekt, das dem folgenden JSON-Schema entspricht. " +
		"Gib nicht das Schema selbst aus, sondern deine ausgefüllte Antwort. Schema: " + schema
	if len(msgs) > 0 && msgs[0].Role == "system" {
		out := slices.Clone(msgs)
		out[0].Content += "\n\n" + hint
		return out
	}
	return append([]protocol.ChatMessage{{Role: "system", Content: hint}}, msgs...)
}

// RunLocal führt einen Teilauftrag auf dem eigenen Backend aus.
func (l *Local) RunLocal(ctx context.Context, id string, t orchestrator.Task) (protocol.Result, error) {
	a, ok := l.agents[id]
	if !ok {
		return protocol.Result{}, fmt.Errorf("agent %q unbekannt", id)
	}
	start := time.Now()
	if t.Audio != nil {
		text, err := a.client.Transcribe(ctx, a.model, *t.Audio)
		if err != nil {
			return protocol.Result{}, err
		}
		return protocol.Result{
			TaskID: t.TaskID, SubtaskID: t.SubtaskID, AgentID: id, ModelFamily: a.info.ModelFamily, Role: t.Role,
			Result: protocol.ResultBody{Status: protocol.StatusOK, Content: text},
			Usage:  protocol.Usage{DurationMs: time.Since(start).Milliseconds()},
		}, nil
	}
	msgs := withSchema(t.Messages, t.JSONSchema)
	creq := backend.ChatRequest{Model: a.model, Messages: msgs, MaxTokens: t.Limits.MaxTokens,
		Tools: t.Tools, ToolChoice: t.ToolChoice}
	var resp backend.ChatResponse
	var err error
	if t.OnDelta != nil && len(t.Tools) == 0 {
		resp, err = a.client.ChatStream(ctx, creq, t.OnDelta)
	} else {
		resp, err = a.client.Chat(ctx, creq)
	}
	if err != nil {
		return protocol.Result{}, err
	}
	return protocol.Result{
		TaskID: t.TaskID, SubtaskID: t.SubtaskID, AgentID: id, ModelFamily: a.info.ModelFamily, Role: t.Role,
		Result:     protocol.ResultBody{Status: protocol.StatusOK, Content: resp.Content, ToolCalls: resp.ToolCalls},
		Confidence: nil,
		Usage: protocol.Usage{PromptTokens: resp.PromptTokens, CompletionTokens: resp.CompletionTokens,
			DurationMs: time.Since(start).Milliseconds()},
	}, nil
}

// Probe prüft beim Start, ob die Backends erreichbar sind und die Modelle anbieten.
func (l *Local) Probe(ctx context.Context) map[string]error {
	out := map[string]error{}
	byClient := map[*backend.Client][]string{}
	for _, id := range l.order {
		a := l.agents[id]
		models, ok := byClient[a.client]
		if !ok {
			var err error
			models, err = a.client.Models(ctx)
			if err != nil {
				out[id] = err
				continue
			}
			byClient[a.client] = models
		}
		found := false
		for _, m := range models {
			found = found || m == a.model
		}
		if !found {
			out[id] = fmt.Errorf("modell %q nicht im backend gefunden", a.model)
		} else {
			out[id] = nil
		}
	}
	return out
}
