// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Rainer Sestig. Kommerzielle Lizenz: siehe COMMERCIAL.md.

// Package gateway stellt die lokale OpenAI-kompatible API, die Status-API und
// eine einfache Web-Oberfläche bereit. Es lauscht ausschließlich auf localhost.
package gateway

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/rainer-sharp/Ai-Swarms.net/internal/orchestrator"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/protocol"
)

type Backend interface {
	Ask(ctx context.Context, req orchestrator.Request) (*orchestrator.Answer, error)
	StatusJSON() any
	Pause()
	Resume()
	Stop()
	Connect(ctx context.Context, addr string) error
	Ping(ctx context.Context, target, network string) (any, error)
}

//go:embed index.html
var indexHTML []byte

const modelPrefix = "swarmainet"

func Handler(b Backend) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(indexHTML)
	})
	mux.HandleFunc("GET /v1/models", func(w http.ResponseWriter, _ *http.Request) {
		var data []map[string]any
		for _, id := range []string{modelPrefix, modelPrefix + "/single", modelPrefix + "/parallel", modelPrefix + "/draft_verify_synthesize", modelPrefix + "/parallel_verify"} {
			data = append(data, map[string]any{"id": id, "object": "model", "owned_by": "swarmainet"})
		}
		writeJSON(w, http.StatusOK, map[string]any{"object": "list", "data": data})
	})
	mux.HandleFunc("POST /v1/chat/completions", func(w http.ResponseWriter, r *http.Request) { chat(b, w, r) })
	mux.HandleFunc("GET /api/status", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, http.StatusOK, b.StatusJSON()) })
	mux.HandleFunc("POST /api/pause", func(w http.ResponseWriter, _ *http.Request) {
		b.Pause()
		writeJSON(w, 200, map[string]string{"ok": "pausiert"})
	})
	mux.HandleFunc("POST /api/resume", func(w http.ResponseWriter, _ *http.Request) {
		b.Resume()
		writeJSON(w, 200, map[string]string{"ok": "fortgesetzt"})
	})
	mux.HandleFunc("POST /api/stop", func(w http.ResponseWriter, _ *http.Request) {
		b.Stop()
		writeJSON(w, 200, map[string]string{"ok": "sofort-stopp"})
	})
	mux.HandleFunc("POST /api/connect", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Addr string `json:"addr"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		if err := b.Connect(ctx, req.Addr); err != nil {
			writeErr(w, http.StatusBadGateway, err)
			return
		}
		writeJSON(w, 200, map[string]string{"ok": "verbunden"})
	})
	mux.HandleFunc("POST /api/ping", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Peer    string `json:"peer"`
			Network string `json:"network"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
		defer cancel()
		rep, err := b.Ping(ctx, req.Peer, req.Network)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, 200, rep)
	})
	return localOnly(mux)
}

// localOnly weist Anfragen ab, die nicht von localhost kommen oder einen
// fremden Host-Header tragen (Schutz gegen DNS-Rebinding aus dem Browser).
func localOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if ip := net.ParseIP(host); err != nil || ip == nil || !ip.IsLoopback() {
			http.Error(w, "nur localhost", http.StatusForbidden)
			return
		}
		h := r.Host
		if hh, _, err := net.SplitHostPort(h); err == nil {
			h = hh
		}
		if h != "localhost" && h != "127.0.0.1" && h != "::1" && h != "[::1]" {
			http.Error(w, "ungültiger host", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

type chatRequest struct {
	Model      string          `json:"model"`
	Messages   []chatMessage   `json:"messages"`
	Stream     bool            `json:"stream"`
	MaxTokens  int             `json:"max_tokens"`
	Tools      json.RawMessage `json:"tools,omitempty"`
	ToolChoice json.RawMessage `json:"tool_choice,omitempty"`
}

// chatMessage ist eine Nachricht im OpenAI-Format. content ist Text, eine
// Liste von Teilen ([{"type":"text","text":…}]) oder null (Werkzeug-Aufruf).
type chatMessage struct {
	Role       string              `json:"role"`
	Content    json.RawMessage     `json:"content"`
	ToolCalls  []protocol.ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string              `json:"tool_call_id,omitempty"`
	Name       string              `json:"name,omitempty"`
}

// errImages: Bilder und andere Nicht-Text-Teile verarbeitet der Schwarm noch nicht.
var errImages = errors.New("bilder und andere nicht-text-inhalte werden noch nicht unterstützt")

// text liefert den Inhalt als Text; Teile vom Typ "text" werden verbunden.
func (m chatMessage) text() (string, error) {
	c := strings.TrimSpace(string(m.Content))
	if c == "" || c == "null" {
		return "", nil
	}
	var s string
	if json.Unmarshal(m.Content, &s) == nil {
		return s, nil
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(m.Content, &parts); err != nil {
		return "", fmt.Errorf("content: erwartet text oder eine liste von teilen")
	}
	var texts []string
	for _, p := range parts {
		if p.Type != "text" {
			return "", errImages
		}
		texts = append(texts, p.Text)
	}
	return strings.Join(texts, "\n"), nil
}

func chat(b Backend, w http.ResponseWriter, r *http.Request) {
	var req chatRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2<<20)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if len(req.Messages) == 0 {
		writeErr(w, http.StatusBadRequest, errors.New("messages fehlt"))
		return
	}
	msgs := make([]protocol.ChatMessage, 0, len(req.Messages))
	for _, m := range req.Messages {
		text, err := m.text()
		if err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		msgs = append(msgs, protocol.ChatMessage{Role: m.Role, Content: text, ToolCalls: m.ToolCalls, ToolCallID: m.ToolCallID, Name: m.Name})
	}
	pattern := ""
	if rest, ok := strings.CutPrefix(req.Model, modelPrefix+"/"); ok {
		pattern = rest
	}
	oreq := orchestrator.Request{
		Messages:   msgs,
		Tools:      req.Tools,
		ToolChoice: req.ToolChoice,
		Pattern:    orchestrator.Pattern(pattern),
		Privacy:    protocol.PrivacyClass(r.Header.Get("X-SwarmAiNet-Privacy")),
		Skill:      r.Header.Get("X-SwarmAiNet-Skill"),
		Agents:     splitList(r.Header.Get("X-SwarmAiNet-Agents")),
		Trace:      r.Header.Get("X-SwarmAiNet-Trace") == "1",
	}
	oreq.Limits.MaxTokens = req.MaxTokens
	ans, err := b.Ask(r.Context(), oreq)
	if errors.Is(err, orchestrator.ErrToolsNotAllowed) {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	id := "chatcmpl-" + ans.TaskID
	created := time.Now().Unix()
	finish := "stop"
	if len(ans.ToolCalls) > 0 {
		finish = "tool_calls"
	}
	if req.Stream {
		// Kompatibilität mit Clients, die immer streamen (z. B. Open WebUI):
		// die geprüfte Antwort kommt als ein Block.
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		chunk := func(delta map[string]any, finish any) {
			b, _ := json.Marshal(map[string]any{
				"id": id, "object": "chat.completion.chunk", "created": created, "model": req.Model,
				"choices": []map[string]any{{"index": 0, "delta": delta, "finish_reason": finish}},
			})
			fmt.Fprintf(w, "data: %s\n\n", b)
		}
		delta := map[string]any{"role": "assistant", "content": ans.Content}
		if len(ans.ToolCalls) > 0 {
			calls := make([]map[string]any, len(ans.ToolCalls))
			for i, c := range ans.ToolCalls {
				calls[i] = map[string]any{"index": i, "id": c.ID, "type": c.Type, "function": c.Function}
			}
			delta["tool_calls"] = calls
		}
		chunk(delta, nil)
		chunk(map[string]any{}, finish)
		fmt.Fprint(w, "data: [DONE]\n\n")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id": id, "object": "chat.completion", "created": created, "model": req.Model,
		"choices": []map[string]any{{
			"index": 0, "finish_reason": finish,
			"message": assistantMessage(ans),
		}},
		"swarmainet": ans,
	})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

// splitList zerlegt eine kommagetrennte Header-Liste.
func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func writeErr(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, map[string]any{"error": map[string]string{"message": err.Error()}})
}

// assistantMessage: Antwort im OpenAI-Format; bei Werkzeug-Aufrufen ohne Text
// ist content null.
func assistantMessage(ans *orchestrator.Answer) map[string]any {
	m := map[string]any{"role": "assistant", "content": ans.Content}
	if len(ans.ToolCalls) > 0 {
		m["tool_calls"] = ans.ToolCalls
		if ans.Content == "" {
			m["content"] = nil
		}
	}
	return m
}
