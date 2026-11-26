// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Rainer Sestig und Mitwirkende.

package backend

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/rainer-sharp/Ai-Swarms.net/internal/protocol"
)

// TestChatRequestImages: Nachrichten mit Bildern gehen im OpenAI-Format als
// Liste von Teilen ans Backend, Nachrichten ohne Bilder bleiben Text.
func TestChatRequestImages(t *testing.T) {
	req := ChatRequest{Model: "m", Stream: true, StreamOptions: map[string]bool{"include_usage": true}, Messages: []protocol.ChatMessage{
		{Role: "system", Content: "Sei knapp."},
		{Role: "user", Content: "Was ist das?", Images: []protocol.Image{{MIME: "image/jpeg", Data: []byte("jpg")}}},
	}}
	b, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{
		`{"role":"system","content":"Sei knapp."}`,
		`"content":[{"text":"Was ist das?","type":"text"},{"image_url":{"url":"data:image/jpeg;base64,anBn"},"type":"image_url"}]`,
		`"stream":true`, `"stream_options":{"include_usage":true}`, `"model":"m"`,
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("fehlt %s in\n%s", want, s)
		}
	}
	if strings.Count(s, `"messages"`) != 1 || strings.Contains(s, `"images"`) {
		t.Fatalf("doppelt oder roh: %s", s)
	}
}
