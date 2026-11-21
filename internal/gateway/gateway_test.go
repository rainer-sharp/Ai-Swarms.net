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

	"github.com/rainer-sharp/Ai-Swarms.net/internal/orchestrator"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/profile"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/protocol"
)

type fakeBackend struct {
	last     orchestrator.Request
	calls    []protocol.ToolCall // Antwort mit Werkzeug-Aufrufen statt Text
	err      error               // Fehler statt Antwort
	feedback []profile.Feedback
}

func (f *fakeBackend) Ask(_ context.Context, r orchestrator.Request) (*orchestrator.Answer, error) {
	f.last = r
	if f.err != nil {
		return nil, f.err
	}
	if f.calls != nil {
		return &orchestrator.Answer{TaskID: "t1", ToolCalls: f.calls, Pattern: r.Pattern}, nil
	}
	return &orchestrator.Answer{TaskID: "t1", Content: "hallo", Pattern: r.Pattern}, nil
}
func (f *fakeBackend) StatusJSON() any  { return map[string]string{"ok": "1"} }
func (f *fakeBackend) ProfileJSON() any { return map[string]string{"profile": "1"} }
func (f *fakeBackend) Feedback(fb profile.Feedback) error {
	f.feedback = append(f.feedback, fb)
	if fb.TaskID != "t1" {
		return profile.ErrUnknownTask
	}
	return nil
}
func (f *fakeBackend) Pause()                                {}
func (f *fakeBackend) Resume()                               {}
func (f *fakeBackend) Stop()                                 {}
func (f *fakeBackend) Connect(context.Context, string) error { return nil }
func (f *fakeBackend) Ping(context.Context, string, string) (any, error) {
	return map[string]bool{"ok": true}, nil
}

func do(h http.Handler, method, path, body, remote, host string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.RemoteAddr, req.Host = remote, host
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestChatCompletion(t *testing.T) {
	fb := &fakeBackend{}
	h := Handler(fb)
	rec := do(h, "POST", "/v1/chat/completions", `{"model":"swarmainet/parallel","messages":[{"role":"user","content":"hi"}]}`, "127.0.0.1:5000", "127.0.0.1:7711")
	if rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	var resp struct {
		Choices []struct {
			Message struct{ Content string } `json:"message"`
		} `json:"choices"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Choices[0].Message.Content != "hallo" || fb.last.Pattern != orchestrator.PatternParallel {
		t.Fatalf("antwort %s, muster %s", rec.Body, fb.last.Pattern)
	}

	rec = do(h, "POST", "/v1/chat/completions", `{"model":"swarmainet","stream":true,"messages":[{"role":"user","content":"hi"}]}`, "127.0.0.1:5000", "localhost:7711")
	if !strings.Contains(rec.Body.String(), "data: [DONE]") || !strings.Contains(rec.Body.String(), "hallo") {
		t.Fatalf("stream: %s", rec.Body)
	}
}

func TestLocalOnly(t *testing.T) {
	h := Handler(&fakeBackend{})
	if rec := do(h, "GET", "/api/status", "", "192.168.1.5:5000", "127.0.0.1:7711"); rec.Code != http.StatusForbidden {
		t.Fatalf("fremde adresse: %d", rec.Code)
	}
	if rec := do(h, "GET", "/api/status", "", "127.0.0.1:5000", "evil.example:7711"); rec.Code != http.StatusForbidden {
		t.Fatalf("dns-rebinding: %d", rec.Code)
	}
	if rec := do(h, "GET", "/api/status", "", "[::1]:5000", "[::1]:7711"); rec.Code != 200 {
		t.Fatalf("ipv6 localhost: %d", rec.Code)
	}
}

func TestAgentsHeader(t *testing.T) {
	fb := &fakeBackend{}
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"swarmainet","messages":[{"role":"user","content":"hi"}]}`))
	req.RemoteAddr, req.Host = "127.0.0.1:5000", "127.0.0.1:7711"
	req.Header.Set("X-SwarmAiNet-Agents", " vllm/qwen3.8-flash-next, ollama/gpt-oss:20b ,")
	req.Header.Set("X-SwarmAiNet-Trace", "1")
	rec := httptest.NewRecorder()
	Handler(fb).ServeHTTP(rec, req)
	if rec.Code != 200 || strings.Join(fb.last.Agents, "|") != "vllm/qwen3.8-flash-next|ollama/gpt-oss:20b" || !fb.last.Trace {
		t.Fatalf("%d agents %q trace %v", rec.Code, fb.last.Agents, fb.last.Trace)
	}
}

// TestToolsAndContentParts: Inhalte als Liste von Teilen, Werkzeuge werden
// durchgereicht, Werkzeug-Aufrufe kommen im OpenAI-Format zurück, auch beim
// Streaming; Bilder werden verständlich abgelehnt.
func TestToolsAndContentParts(t *testing.T) {
	fb := &fakeBackend{calls: []protocol.ToolCall{{ID: "call_1", Type: "function",
		Function: protocol.ToolFunction{Name: "read_file", Arguments: `{"path":"main.go"}`}}}}
	h := Handler(fb)
	body := `{"model":"swarmainet","messages":[
		{"role":"system","content":"Du bist ein Agent."},
		{"role":"user","content":[{"type":"text","text":"Lies"},{"type":"text","text":"main.go"}]},
		{"role":"assistant","content":null,"tool_calls":[{"id":"call_0","type":"function","function":{"name":"ls","arguments":"{}"}}]},
		{"role":"tool","tool_call_id":"call_0","content":"main.go"}],
		"tools":[{"type":"function","function":{"name":"read_file","parameters":{"type":"object"}}}]}`
	rec := do(h, "POST", "/v1/chat/completions", body, "127.0.0.1:5000", "127.0.0.1:7711")
	if rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	m := fb.last.Messages
	if m[1].Content != "Lies\nmain.go" || len(m[2].ToolCalls) != 1 || m[3].ToolCallID != "call_0" || !strings.Contains(string(fb.last.Tools), "read_file") {
		t.Fatalf("anfrage falsch übernommen: %+v tools %s", m, fb.last.Tools)
	}
	var resp struct {
		Choices []struct {
			FinishReason string `json:"finish_reason"`
			Message      struct {
				Content   *string             `json:"content"`
				ToolCalls []protocol.ToolCall `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	c := resp.Choices[0]
	if c.FinishReason != "tool_calls" || c.Message.Content != nil || c.Message.ToolCalls[0].Function.Name != "read_file" {
		t.Fatalf("antwort: %s", rec.Body)
	}

	rec = do(h, "POST", "/v1/chat/completions", strings.Replace(body, `"model":"swarmainet"`, `"model":"swarmainet","stream":true`, 1), "127.0.0.1:5000", "127.0.0.1:7711")
	out := rec.Body.String()
	if !strings.Contains(out, `"tool_calls":[{`) || !strings.Contains(out, `"index":0`) || !strings.Contains(out, `"finish_reason":"tool_calls"`) {
		t.Fatalf("stream: %s", out)
	}

	rec = do(h, "POST", "/v1/chat/completions", `{"model":"swarmainet","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:…"}}]}]}`, "127.0.0.1:5000", "127.0.0.1:7711")
	if rec.Code != 400 || !strings.Contains(rec.Body.String(), "bilder") {
		t.Fatalf("bild: %d %s", rec.Code, rec.Body)
	}
}

// TestNoAgentStatus: Scheitert die Auswahl an der Freigabeklasse oder an den
// Werkzeugrechten, liegt es an der Anfrage (400); sonst am Schwarm (502).
func TestNoAgentStatus(t *testing.T) {
	privacy := fmt.Errorf("vorher: %w", orchestrator.ErrPrivacy)
	for _, tc := range []struct {
		err  error
		code int
	}{
		{privacy, 400},
		{orchestrator.ErrToolsNotAllowed, 400},
		{orchestrator.ErrNoAgent, 502},
	} {
		h := Handler(&fakeBackend{err: tc.err})
		rec := do(h, "POST", "/v1/chat/completions", `{"model":"swarmainet","messages":[{"role":"user","content":"x"}]}`, "127.0.0.1:5000", "127.0.0.1:7711")
		if rec.Code != tc.code {
			t.Errorf("%v: %d, erwartet %d", tc.err, rec.Code, tc.code)
		}
	}
}

// TestFeedbackEndpoint: POST /v1/feedback reicht die Rückmeldung durch, kennt
// keine fremden Felder und meldet unbekannte Aufträge mit 404.
func TestFeedbackEndpoint(t *testing.T) {
	fb := &fakeBackend{}
	h := Handler(fb)
	for _, tc := range []struct {
		body string
		code int
	}{
		{`{"task_id":"t1","good":true,"objective":true,"step":0}`, 204},
		{`{"task_id":"t2","good":false}`, 404},
		{`{"task_id":"t1","gut":true}`, 400},
		{`kein json`, 400},
	} {
		if rec := do(h, "POST", "/v1/feedback", tc.body, "127.0.0.1:5000", "127.0.0.1:7711"); rec.Code != tc.code {
			t.Errorf("%s: %d, erwartet %d", tc.body, rec.Code, tc.code)
		}
	}
	if len(fb.feedback) != 2 || !fb.feedback[0].Objective || fb.feedback[0].Step == nil || *fb.feedback[0].Step != 0 {
		t.Fatalf("durchgereicht: %+v", fb.feedback)
	}
	if rec := do(h, "GET", "/api/profile", "", "127.0.0.1:5000", "127.0.0.1:7711"); rec.Code != 200 || !strings.Contains(rec.Body.String(), "profile") {
		t.Fatalf("profil: %d %s", rec.Code, rec.Body)
	}
	if rec := do(h, "POST", "/v1/feedback", `{"task_id":"t1","good":true}`, "192.0.2.7:5000", "127.0.0.1:7711"); rec.Code == 204 {
		t.Fatal("rückmeldung von außen angenommen")
	}
}
