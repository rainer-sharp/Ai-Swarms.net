// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Rainer Sestig und Mitwirkende.

package gateway

import (
	"encoding/base64"
	"fmt"
	"strings"
	"testing"

	"github.com/rainer-sharp/Ai-Swarms.net/internal/protocol"
)

// TestChatImages: Eingebettete Bilder kommen als protocol.Image beim
// Orchestrator an; Links, fremde Formate, andere Teile und zu große Bilder
// lehnt das Gateway ab.
func TestChatImages(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\nbild")
	url := "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)
	part := func(url string) string {
		return fmt.Sprintf(`{"model":"swarmainet","messages":[{"role":"user","content":[{"type":"text","text":"Was zeigt das?"},{"type":"image_url","image_url":{"url":%q}}]}]}`, url)
	}
	fb := &fakeBackend{}
	h := Handler(fb)
	rec := do(h, "POST", "/v1/chat/completions", part(url), "127.0.0.1:5000", "127.0.0.1:7711")
	if rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	m := fb.last.Messages[0]
	if m.Content != "Was zeigt das?" || len(m.Images) != 1 || m.Images[0].MIME != "image/png" || string(m.Images[0].Data) != string(png) {
		t.Fatalf("nachricht %+v", m)
	}

	big := "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(make([]byte, protocol.MaxImageBytes+1))
	for _, c := range []struct {
		name, body, want string
		code             int
	}{
		{"link", part("https://example.org/a.png"), "keine links", 400},
		{"format", part("data:image/svg+xml;base64,PHN2Zy8+"), "nicht unterstützt", 400},
		{"base64", part("data:image/png;base64,%%%"), "base64", 400},
		{"audio", `{"model":"swarmainet","messages":[{"role":"user","content":[{"type":"input_audio","input_audio":{}}]}]}`, "nur text und bilder", 400},
		{"zu groß", part(big), "bilder zusammen", 413},
		{"körper", part(big + strings.Repeat("A", maxChatBody)), "anfrage größer", 413},
	} {
		rec := do(h, "POST", "/v1/chat/completions", c.body, "127.0.0.1:5000", "127.0.0.1:7711")
		if rec.Code != c.code || !strings.Contains(rec.Body.String(), c.want) {
			t.Errorf("%s: %d %s", c.name, rec.Code, rec.Body)
		}
	}
}
