// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Rainer Sestig und Mitwirkende.

package node

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/rainer-sharp/Ai-Swarms.net/internal/config"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/identity"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/orchestrator"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/protocol"
)

func TestFitTokens(t *testing.T) {
	long := []protocol.ChatMessage{{Role: "user", Content: strings.Repeat("x", 30000)}} // ~10 000 Tokens
	for _, c := range []struct {
		name               string
		maxTokens, context int
		msgs               []protocol.ChatMessage
		tools, want        int
	}{
		{"kontext unbekannt", 8192, 0, long, 0, 8192},
		{"passt", 2048, 32768, long, 0, 2048},
		{"begrenzt", 8192, 16384, long, 0, 16384 - 10004}, // (30000 + 4 + 8) / 3
		{"werkzeuge zählen", 8192, 16384, long, 3000, 16384 - 11004},
		{"bilder zählen", 8192, 8192, []protocol.ChatMessage{{Role: "user", Images: make([]protocol.Image, 2)}}, 0, 8192 - 3000 - 4},
		{"mindestens etwas platz", 8192, 4096, long, 0, minAnswerRoom},
	} {
		if got := fitTokens(c.maxTokens, c.context, c.msgs, c.tools); got != c.want {
			t.Errorf("%s: %d, erwartet %d", c.name, got, c.want)
		}
	}
}

// TestTokenLimitOverNetwork: Verbraucht das Modell des Anbieters max_tokens
// ohne Antwort, erfährt der Auftraggeber den Grund (nicht nur „ausführung
// fehlgeschlagen“). Außerdem kommt max_tokens auf den Kontext begrenzt an.
func TestTokenLimitOverNetwork(t *testing.T) {
	ctx := t.Context()
	idA, _ := identity.New()
	idB, _ := identity.New()
	var mu sync.Mutex
	var maxTokens int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]string{{"id": "denker"}}})
			return
		}
		var req struct {
			MaxTokens int `json:"max_tokens"`
		}
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &req)
		mu.Lock()
		maxTokens = req.MaxTokens
		mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]string{"role": "assistant", "content": ""}, "finish_reason": "length"}},
		})
	}))
	t.Cleanup(srv.Close)
	a := newTestNodeWith(t, ctx, idA, "eigen", "mistral", nil, 8000, false, nil)
	b := newTestNodeWith(t, ctx, idB, "denker", "qwen", nil, 8000, true, func(c *config.Config) {
		c.Backends = []config.Backend{{Name: "be", URL: srv.URL + "/v1", Models: []config.Model{
			{Name: "denker", Family: "qwen", Skills: []string{"general"}, Context: 4096, Offer: true}}}}
		c.Resources.AcceptPublicTasks = true
	})
	if err := a.P2P.Connect(ctx, b.P2P.Addrs()[0]); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return len(a.Agents()) == 2 })

	_, err := a.Ask(ctx, orchestrator.Request{Messages: []protocol.ChatMessage{{Role: "user", Content: "Beweise es."}},
		Pattern: orchestrator.PatternSingle, Privacy: protocol.PrivacyPublic, Agents: []string{"be/denker"},
		Limits: protocol.Limits{MaxTokens: 8192}})
	if !errors.Is(err, protocol.ErrTokenLimit) || !strings.Contains(err.Error(), "anbieter brach ab: token-limit erreicht") {
		t.Fatalf("fehler: %v", err)
	}
	mu.Lock()
	got := maxTokens
	mu.Unlock()
	if got <= minAnswerRoom || got >= 4096 {
		t.Fatalf("max_tokens beim backend: %d, erwartet unter dem kontext 4096", got)
	}
}
