// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Rainer Sestig und Mitwirkende.

package node

import (
	"encoding/json"
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

// TestImagesOverNetwork: Ein Bild geht über das Netz an das Modell mit
// vision: true des anderen Knotens und kommt dort als image_url an; ein
// Modell ohne Vision bekommt nichts. Danach die Regeln beim Anbieter.
func TestImagesOverNetwork(t *testing.T) {
	ctx := t.Context()
	idA, _ := identity.New()
	idB, _ := identity.New()
	var mu sync.Mutex
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]string{{"id": "sehen"}, {"id": "text"}}})
			return
		}
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		body = string(b)
		mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]string{"role": "assistant", "content": "Eine Katze."}}},
			"usage":   map[string]int{"prompt_tokens": 10, "completion_tokens": 3},
		})
	}))
	t.Cleanup(srv.Close)
	a := newTestNodeWith(t, ctx, idA, "eigen", "mistral", nil, 8000, false, nil)
	b := newTestNodeWith(t, ctx, idB, "sehen", "qwen", nil, 8000, true, func(c *config.Config) {
		c.Backends = []config.Backend{{Name: "be", URL: srv.URL + "/v1", Models: []config.Model{
			{Name: "sehen", Family: "qwen", Skills: []string{"general"}, Context: 8000, Offer: true, Vision: true},
			{Name: "text", Family: "llama", Skills: []string{"general"}, Context: 8000, Offer: true}}}}
		c.Resources.AcceptPublicTasks = true
	})
	if err := a.P2P.Connect(ctx, b.P2P.Addrs()[0]); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return len(a.Agents()) == 3 })

	question := []protocol.ChatMessage{{Role: "user", Content: "Was zeigt das?",
		Images: []protocol.Image{{MIME: "image/png", Data: []byte("png")}}}}
	ans, err := a.Ask(ctx, orchestrator.Request{Messages: question, Pattern: orchestrator.PatternSingle,
		Privacy: protocol.PrivacyPublic, Agents: []string{"be/sehen", "be/text"}})
	if err != nil || ans.Content != "Eine Katze." || ans.Steps[0].AgentID != "be/sehen" {
		t.Fatalf("%v %+v", err, ans)
	}
	mu.Lock()
	got := body
	mu.Unlock()
	if !strings.Contains(got, `"image_url":{"url":"data:image/png;base64,cG5n"}`) {
		t.Fatalf("backend sah: %s", got)
	}

	// Anbieter: Ein Koordinator, der die Regeln umgeht, scheitert trotzdem.
	agent := func(id string) orchestrator.Agent {
		for _, x := range a.Agents() {
			if x.ID == id {
				return x
			}
		}
		t.Fatalf("agent %s fehlt", id)
		return orchestrator.Agent{}
	}
	task := func(class protocol.PrivacyClass, mime string) orchestrator.Task {
		return orchestrator.Task{TaskID: "t", SubtaskID: "s", Role: protocol.RoleSpecialist, Skill: "general",
			Messages: []protocol.ChatMessage{{Role: "user", Content: "?", Images: []protocol.Image{{MIME: mime, Data: []byte("x")}}}},
			Limits:   protocol.Limits{PrivacyClass: class, MaxTokens: 100}}
	}
	for _, c := range []struct {
		name, agent string
		task        orchestrator.Task
		want        string
	}{
		{"ohne vision", "be/text", task(protocol.PrivacyPublic, "image/png"), "verarbeitet keine bilder"},
		{"reduced an fremde", "be/sehen", task(protocol.PrivacyReduced, "image/png"), "bilder bei reduced nur im vertrauten kreis"},
		{"format", "be/sehen", task(protocol.PrivacyPublic, "image/svg+xml"), "unbekanntem format"},
	} {
		if _, err := a.P2P.Execute(ctx, agent(c.agent), c.task); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v", c.name, err)
		}
	}
}
