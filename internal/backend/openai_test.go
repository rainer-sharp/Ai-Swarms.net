// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Rainer Sestig. Kommerzielle Lizenz: siehe COMMERCIAL.md.

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
		{`{"choices":[{"message":{"role":"assistant","content":""},"finish_reason":"length"}]}`, "token-limit (max_tokens 2048)"},
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
