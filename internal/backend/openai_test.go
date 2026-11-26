// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Rainer Sestig und Mitwirkende.

package backend

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rainer-sharp/Ai-Swarms.net/internal/protocol"
)

func TestChatEmptyContentIsError(t *testing.T) {
	for _, tc := range []struct{ body, want string }{
		{`{"choices":[{"message":{"role":"assistant","content":""},"finish_reason":"length"}]}`, "token-limit erreicht (max_tokens 2048)"},
		{`{"choices":[{"message":{"role":"assistant","content":"  "},"finish_reason":"stop"}]}`, "leere antwort"},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(tc.body)) }))
		_, err := New(srv.URL, "").Chat(context.Background(), ChatRequest{Model: "m", MaxTokens: 2048})
		srv.Close()
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("fehler %v, erwartet %q", err, tc.want)
		}
	}
}

// TestChatToolCalls: Werkzeug-Aufrufe ohne Text sind eine gültige Antwort,
// und tools/tool_choice werden an das Backend weitergegeben.
func TestChatToolCalls(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got = string(b)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"ls","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`))
	}))
	defer srv.Close()
	resp, err := New(srv.URL, "").Chat(context.Background(), ChatRequest{Model: "m",
		Tools: json.RawMessage(`[{"type":"function","function":{"name":"ls"}}]`), ToolChoice: json.RawMessage(`"auto"`)})
	if err != nil || len(resp.ToolCalls) != 1 || resp.ToolCalls[0].Function.Name != "ls" {
		t.Fatalf("antwort %+v, fehler %v", resp, err)
	}
	if !strings.Contains(got, `"tools":[{`) || !strings.Contains(got, `"tool_choice":"auto"`) {
		t.Fatalf("anfrage: %s", got)
	}
}

// TestTranscribe: Die Aufnahme geht als multipart/form-data mit Modell und
// Sprache an /audio/transcriptions; zurück kommt der Text.
func TestTranscribe(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/audio/transcriptions" {
			http.Error(w, "pfad", 404)
			return
		}
		f, hdr, err := r.FormFile("file")
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		data, _ := io.ReadAll(f)
		if string(data) != "RIFF-daten" || hdr.Filename != "a.wav" || r.FormValue("model") != "asr" || r.FormValue("language") != "de" {
			http.Error(w, "felder", 400)
			return
		}
		_, _ = w.Write([]byte(`{"text":"Hallo Schwarm"}`))
	}))
	defer srv.Close()
	c := New(srv.URL+"/v1", "")
	got, err := c.Transcribe(context.Background(), "asr", protocol.Audio{Data: []byte("RIFF-daten"), Filename: "a.wav", Language: "de"})
	if err != nil || got != "Hallo Schwarm" {
		t.Fatalf("%q %v", got, err)
	}
}

// TestTranscribeChatFallback: Kennt das Backend /audio/transcriptions nicht,
// geht die Aufnahme als audio_url über den Chat, mit abgeschaltetem
// Denkmodus (vLLM mit Nemotron-Transcribe).
func TestTranscribeChatFallback(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":" Rätselhaft, höchst rätselhaft. "},"finish_reason":"stop"}]}`))
	}))
	defer srv.Close()
	c := New(srv.URL+"/v1", "")
	text, err := c.Transcribe(context.Background(), "asr", protocol.Audio{Data: []byte("ID3"), Filename: "a.mp3", Prompt: "Clarimonde"})
	if err != nil || text != "Rätselhaft, höchst rätselhaft." {
		t.Fatalf("%q %v", text, err)
	}
	kw, _ := got["chat_template_kwargs"].(map[string]any)
	content := got["messages"].([]any)[0].(map[string]any)["content"].([]any)
	prompt := content[0].(map[string]any)["text"].(string)
	url := content[1].(map[string]any)["audio_url"].(map[string]any)["url"].(string)
	if kw["enable_thinking"] != false || got["temperature"] != 0.0 || !strings.HasPrefix(url, "data:audio/mpeg;base64,SUQz") ||
		!strings.Contains(prompt, "Transcribe this audio") || !strings.Contains(prompt, "Context terms: Clarimonde.") {
		t.Fatalf("anfrage %v", got)
	}

	// Abbruch an der Token-Grenze ist ein Fehler, keine halbe Abschrift.
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ja ja ja"},"finish_reason":"length"}]}`))
	}))
	defer srv2.Close()
	if _, err := New(srv2.URL+"/v1", "").Transcribe(context.Background(), "asr", protocol.Audio{Data: []byte("x")}); err == nil {
		t.Fatal("abgebrochene abschrift angenommen")
	}
}

func TestCleanTranscript(t *testing.T) {
	in := "So. <sty:stt:vocal_texture_delivery> Außer <sty:end:vocal_texture_delivery>  auf. <spk:1> Ja, a < b."
	if got := cleanTranscript(in); got != "So. Außer auf. Ja, a < b." {
		t.Fatalf("%q", got)
	}
}

// TestChatStream: Text und Denkphase kommen Stück für Stück, die ganze
// Antwort und die Token-Zahlen am Ende.
func TestChatStream(t *testing.T) {
	var got struct {
		Stream        bool            `json:"stream"`
		StreamOptions map[string]bool `json:"stream_options"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.Header().Set("Content-Type", "text/event-stream")
		for _, ev := range []string{
			`{"choices":[{"delta":{"role":"assistant","reasoning_content":"Hm, "}}]}`,
			`{"choices":[{"delta":{"reasoning":"gut."}}]}`,
			`{"choices":[{"delta":{"content":"Hal"}}]}`,
			`{"choices":[{"delta":{"content":"lo"},"finish_reason":"stop"}]}`,
			`{"choices":[],"usage":{"prompt_tokens":7,"completion_tokens":3}}`,
		} {
			_, _ = io.WriteString(w, ": kommentar\ndata: "+ev+"\n\n")
		}
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()
	var content, reasoning []string
	resp, err := New(srv.URL, "").ChatStream(context.Background(), ChatRequest{Model: "m"}, func(c, r string) {
		if c != "" {
			content = append(content, c)
		}
		if r != "" {
			reasoning = append(reasoning, r)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Stream || !got.StreamOptions["include_usage"] {
		t.Fatalf("anfrage %+v", got)
	}
	if resp.Content != "Hallo" || resp.PromptTokens != 7 || resp.CompletionTokens != 3 ||
		strings.Join(content, "|") != "Hal|lo" || strings.Join(reasoning, "|") != "Hm, |gut." {
		t.Fatalf("%+v %q %q", resp, content, reasoning)
	}
}

// TestChatStreamFallback: Lehnt das Backend den Stream ab oder antwortet ohne
// Stream, gilt die normale Antwort; nur Denkphase ohne Text ist ein Fehler.
func TestChatStreamFallback(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var req struct {
			Stream bool `json:"stream"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Stream {
			http.Error(w, "stream_options unbekannt", http.StatusBadRequest)
			return
		}
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"ganz"}}]}`)
	}))
	defer srv.Close()
	resp, err := New(srv.URL, "").ChatStream(context.Background(), ChatRequest{Model: "m"}, func(string, string) {})
	if err != nil || resp.Content != "ganz" || calls != 2 {
		t.Fatalf("400: %+v %v %d", resp, err, calls)
	}

	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"block"}}]}`)
	}))
	defer plain.Close()
	if resp, err := New(plain.URL, "").ChatStream(context.Background(), ChatRequest{Model: "m"}, func(string, string) {}); err != nil || resp.Content != "block" {
		t.Fatalf("ohne stream: %+v %v", resp, err)
	}

	think := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"reasoning\":\"lang\"},\"finish_reason\":\"length\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer think.Close()
	if _, err := New(think.URL, "").ChatStream(context.Background(), ChatRequest{Model: "m", MaxTokens: 50}, func(string, string) {}); err == nil || !strings.Contains(err.Error(), "token-limit") {
		t.Fatalf("nur denkphase: %v", err)
	}
}

// TestChatStreamToolCalls: Werkzeug-Aufrufe in Bruchstücken (mit index) und
// am Stück (ohne index) werden zusammengesetzt und nie als Text gemeldet.
func TestChatStreamToolCalls(t *testing.T) {
	for name, events := range map[string][]string{
		"bruchstücke": {
			`{"choices":[{"delta":{"content":"Ich lese "}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"read_","arguments":""}}]}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"name":"file","arguments":"{\"path\":"}}]}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":1,"id":"call_2","type":"function","function":{"name":"ls","arguments":"{}"}}]}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"main.go\"}"}}]},"finish_reason":"tool_calls"}]}`,
		},
		"am stück": {
			`{"choices":[{"delta":{"content":"Ich lese "}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"id":"call_1","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"main.go\"}"}}]}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"id":"call_2","type":"function","function":{"name":"ls","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`,
		},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			for _, ev := range events {
				_, _ = io.WriteString(w, "data: "+ev+"\n\n")
			}
			_, _ = io.WriteString(w, "data: [DONE]\n\n")
		}))
		var text string
		resp, err := New(srv.URL, "").ChatStream(context.Background(), ChatRequest{Model: "m", Tools: json.RawMessage(`[]`)},
			func(c, _ string) { text += c })
		srv.Close()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		want := []protocol.ToolCall{
			{ID: "call_1", Type: "function", Function: protocol.ToolFunction{Name: "read_file", Arguments: `{"path":"main.go"}`}},
			{ID: "call_2", Type: "function", Function: protocol.ToolFunction{Name: "ls", Arguments: "{}"}},
		}
		if text != "Ich lese " || resp.Content != text || len(resp.ToolCalls) != 2 || resp.ToolCalls[0] != want[0] || resp.ToolCalls[1] != want[1] {
			t.Fatalf("%s: %q %+v", name, text, resp.ToolCalls)
		}
	}

	// Nur ein Aufruf, kein Text: vollständige Antwort.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c","function":{"name":"ls","arguments":"{}"}}]}}]}`+"\n\ndata: [DONE]\n\n")
	}))
	defer srv.Close()
	if resp, err := New(srv.URL, "").ChatStream(context.Background(), ChatRequest{Model: "m"}, func(string, string) {}); err != nil || len(resp.ToolCalls) != 1 {
		t.Fatalf("%+v %v", resp, err)
	}
}

// TestResponseFormat: Das JSON-Schema geht als response_format mit. Lehnt
// ein Backend das ab (400), wiederholt Chat ohne und lässt es danach weg.
func TestResponseFormat(t *testing.T) {
	schema := `{"type":"object","required":["verdict"],"properties":{"verdict":{"enum":["ok","issues"]}}}`
	if SchemaFormat("") != nil || SchemaFormat("{kaputt") != nil {
		t.Fatal("ungültiges schema angenommen")
	}
	for _, strict := range []bool{false, true} {
		var calls, withFormat int
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var req struct {
				ResponseFormat *struct {
					Type       string `json:"type"`
					JSONSchema struct {
						Schema json.RawMessage `json:"schema"`
					} `json:"json_schema"`
				} `json:"response_format"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			calls++
			if req.ResponseFormat != nil {
				withFormat++
				if req.ResponseFormat.Type != "json_schema" || string(req.ResponseFormat.JSONSchema.Schema) != schema {
					t.Errorf("response_format: %+v", req.ResponseFormat)
				}
				if strict { // Backend ohne response_format
					w.WriteHeader(http.StatusBadRequest)
					return
				}
			}
			_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"{\"verdict\":\"ok\"}"}}]}`))
		}))
		c := New(srv.URL, "")
		for range 2 {
			r, err := c.Chat(context.Background(), ChatRequest{Model: "m", ResponseFormat: SchemaFormat(schema)})
			if err != nil || r.Content != `{"verdict":"ok"}` {
				t.Fatalf("strict=%v: %v %q", strict, err, r.Content)
			}
		}
		srv.Close()
		// Unterstützt: beide Anfragen mit Format. Abgelehnt: einmal mit, dann ohne.
		if want := map[bool][2]int{false: {2, 2}, true: {3, 1}}[strict]; [2]int{calls, withFormat} != want {
			t.Errorf("strict=%v: %d aufrufe, %d mit format, erwartet %v", strict, calls, withFormat, want)
		}
	}
}
