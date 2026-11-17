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
