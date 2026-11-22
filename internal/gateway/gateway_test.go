// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Rainer Sestig. Kommerzielle Lizenz: siehe COMMERCIAL.md.

package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/rainer-sharp/Ai-Swarms.net/internal/audio"
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

func audioForm(t *testing.T, data []byte, fields map[string]string) (string, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if data != nil {
		fw, _ := mw.CreateFormFile("file", "a.wav")
		_, _ = fw.Write(data)
	}
	for k, v := range fields {
		_ = mw.WriteField(k, v)
	}
	_ = mw.Close()
	return buf.String(), mw.FormDataContentType()
}

// TestTranscriptionEndpoint: /v1/audio/transcriptions reicht die Aufnahme
// als Audio-Auftrag durch und antwortet im OpenAI-Format.
func TestTranscriptionEndpoint(t *testing.T) {
	fb := &fakeBackend{}
	h := Handler(fb)
	post := func(body, ctype, privacy string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/v1/audio/transcriptions", strings.NewReader(body))
		req.RemoteAddr, req.Host = "127.0.0.1:5000", "127.0.0.1:7711"
		req.Header.Set("Content-Type", ctype)
		if privacy != "" {
			req.Header.Set("X-SwarmAiNet-Privacy", privacy)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	body, ct := audioForm(t, []byte("RIFF"), map[string]string{"model": "whisper-1", "language": "de"})
	rec := post(body, ct, "local")
	var out struct {
		Text string `json:"text"`
	}
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &out) != nil || out.Text != "hallo" {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	a := fb.last.Audio
	if a == nil || string(a.Data) != "RIFF" || a.Filename != "a.wav" || a.Language != "de" || fb.last.Privacy != "local" {
		t.Fatalf("auftrag %+v", fb.last)
	}
	body, ct = audioForm(t, []byte("RIFF"), map[string]string{"response_format": "text"})
	if rec := post(body, ct, ""); rec.Code != 200 || rec.Body.String() != "hallo" {
		t.Fatalf("text: %d %q", rec.Code, rec.Body)
	}
	body, ct = audioForm(t, nil, nil)
	if rec := post(body, ct, ""); rec.Code != 400 {
		t.Fatalf("ohne datei: %d", rec.Code)
	}
	// Zu groß für einen Teilauftrag und keine Aufnahme: ohne ffmpeg 413,
	// mit ffmpeg 400 (nicht lesbar).
	want := http.StatusRequestEntityTooLarge
	if audio.Available() {
		want = http.StatusBadRequest
	}
	body, ct = audioForm(t, make([]byte, protocol.MaxAudioBytes+1), nil)
	if rec := post(body, ct, ""); rec.Code != want {
		t.Fatalf("zu groß: %d %s", rec.Code, rec.Body)
	}
	fb.err = orchestrator.ErrPrivacy
	body, ct = audioForm(t, []byte("RIFF"), nil)
	if rec := post(body, ct, "public"); rec.Code != 400 {
		t.Fatalf("public: %d", rec.Code)
	}
}

// chunkBackend schreibt jedes Stück als Wortfolge ab, die sich mit dem
// Nachbarstück in der Überlappung deckt.
// Mit busy meldet ein Stück zuerst „ausgelastet“, mit fail scheitert es; die
// anderen warten dann auf den Abbruch.
type chunkBackend struct {
	fakeBackend
	mu         sync.Mutex
	calls      int
	busy, fail map[int]int
}

func (c *chunkBackend) Ask(ctx context.Context, r orchestrator.Request) (*orchestrator.Answer, error) {
	var i int
	if _, err := fmt.Sscanf(r.Audio.Filename, "stueck-%02d.mp3", &i); err != nil || r.Audio.MIME != "audio/mpeg" || len(r.Audio.Data) > protocol.MaxAudioBytes {
		return nil, fmt.Errorf("stück %q %s %d", r.Audio.Filename, r.Audio.MIME, len(r.Audio.Data))
	}
	c.mu.Lock()
	c.calls++
	busy := c.busy[i] > 0
	if busy {
		c.busy[i]--
	}
	_, fail := c.fail[i]
	failing := len(c.fail) > 0
	c.mu.Unlock()
	switch {
	case busy:
		return nil, fmt.Errorf("alle kandidaten ausgelastet: %w", orchestrator.ErrBusy)
	case fail:
		return nil, errors.New("backend kaputt")
	case failing:
		<-ctx.Done()
		return nil, fmt.Errorf("abgebrochen: %w", ctx.Err())
	}
	// Wort w steht für Minute w; Stück i umfasst Minute 9i bis 9i+10.
	var words []string
	for m := 9 * i; m < min(9*i+10, 25); m++ {
		words = append(words, fmt.Sprintf("w%d", m))
	}
	return &orchestrator.Answer{Content: strings.Join(words, " ")}, nil
}

// TestTranscriptionLong: Eine Aufnahme über dem Limit eines Teilauftrags
// wird geteilt, parallel abgeschrieben und ohne Doppelungen zusammengefügt;
// ein ausgelastetes Stück kommt zurück in die Schlange, ein fehlgeschlagenes
// wird als Ursache gemeldet.
func TestTranscriptionLong(t *testing.T) {
	if !audio.Available() {
		t.Skip("ffmpeg fehlt")
	}
	path := filepath.Join(t.TempDir(), "lang.wav")
	// 25 min WAV mit 8 kHz, 16 bit: etwa 24 MB
	if out, err := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "sine=frequency=300:duration=1500:sample_rate=8000",
		path).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	post := func(cb *chunkBackend) *httptest.ResponseRecorder {
		body, ct := audioForm(t, data, map[string]string{"response_format": "verbose_json"})
		req := httptest.NewRequest("POST", "/v1/audio/transcriptions", strings.NewReader(body))
		req.RemoteAddr, req.Host = "127.0.0.1:5000", "127.0.0.1:7711"
		req.Header.Set("Content-Type", ct)
		rec := httptest.NewRecorder()
		Handler(cb).ServeHTTP(rec, req)
		return rec
	}
	cb := &chunkBackend{busy: map[int]int{1: 2}}
	rec := post(cb)
	var out struct {
		Text       string
		Swarmainet struct{ Chunks int }
	}
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &out) != nil {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	var want []string
	for m := range 25 {
		want = append(want, fmt.Sprintf("w%d", m))
	}
	if out.Text != strings.Join(want, " ") || out.Swarmainet.Chunks != 3 || cb.calls != 5 {
		t.Fatalf("%q, %d stücke, %d aufrufe", out.Text, out.Swarmainet.Chunks, cb.calls)
	}
	rec = post(&chunkBackend{fail: map[int]int{2: 1}})
	if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "stück 2 (ab 18m0s): backend kaputt") {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
}
