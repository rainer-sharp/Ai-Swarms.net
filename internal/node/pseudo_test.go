// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Rainer Sestig und Mitwirkende.

package node

import (
	"encoding/json"
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

// chatServer ist ein Backend, dessen Antwort answer aus den Nachrichten bildet;
// seen hält fest, was das Modell zu sehen bekam.
func chatServer(t *testing.T, model string, answer func([]protocol.ChatMessage) string) (*httptest.Server, func() string) {
	var mu sync.Mutex
	var seen strings.Builder
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]string{{"id": model}}})
			return
		}
		var req struct {
			Messages []protocol.ChatMessage `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		mu.Lock()
		for _, m := range req.Messages {
			seen.WriteString(m.Content + "\n")
		}
		mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]string{"role": "assistant", "content": answer(req.Messages)}}},
			"usage":   map[string]int{"prompt_tokens": 10, "completion_tokens": 5},
		})
	}))
	t.Cleanup(srv.Close)
	return srv, func() string { mu.Lock(); defer mu.Unlock(); return seen.String() }
}

// TestReducedOverNetwork: Ein fremder Anbieter mit accept_public_tasks nimmt
// eine reduced-Anfrage an, sieht aber nur Platzhalter; die Antwort kommt mit
// den Originalen zurück. Namen erkennt das eigene Modell des Koordinators.
func TestReducedOverNetwork(t *testing.T) {
	ctx := t.Context()
	idA, _ := identity.New()
	idB, _ := identity.New()
	own, _ := chatServer(t, "eigen", func(msgs []protocol.ChatMessage) string {
		if strings.Contains(msgs[0].Content, "persönliche Angaben") {
			return `{"entities":[{"text":"Anna Berg","kind":"PERSON"},{"text":"Hamburg","kind":"LOCATION"}]}`
		}
		return "eigene antwort"
	})
	foreign, seen := chatServer(t, "fremd", func([]protocol.ChatMessage) string {
		return "Liebe [PERSON_1], wir sehen uns in [LOCATION_1]."
	})
	backend := func(srv *httptest.Server, model, family string, offer bool) func(*config.Config) {
		return func(c *config.Config) {
			c.Backends = []config.Backend{{Name: "be", URL: srv.URL + "/v1", Models: []config.Model{
				{Name: model, Family: family, Skills: []string{"general"}, Context: 8000, Offer: offer}}}}
			c.Resources.AcceptPublicTasks = offer
		}
	}
	a := newTestNodeWith(t, ctx, idA, "eigen", "mistral", nil, 8000, false, backend(own, "eigen", "mistral", false))
	b := newTestNodeWith(t, ctx, idB, "fremd", "qwen", nil, 8000, true, backend(foreign, "fremd", "qwen", true))
	if err := a.P2P.Connect(ctx, b.P2P.Addrs()[0]); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return len(a.Agents()) == 2 })

	ans, err := a.Ask(ctx, orchestrator.Request{
		Messages: []protocol.ChatMessage{{Role: "user", Content: "Lade Anna Berg (anna@example.org) nach Hamburg ein."}},
		Pattern:  orchestrator.PatternSingle, Privacy: protocol.PrivacyReduced, Agents: []string{"be/fremd"},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := seen()
	for _, leak := range []string{"Anna Berg", "anna@example.org", "Hamburg"} {
		if strings.Contains(got, leak) {
			t.Fatalf("fremder anbieter sieht %q:\n%s", leak, got)
		}
	}
	if !strings.Contains(got, "Lade [PERSON_1] ([EMAIL_1]) nach [LOCATION_1] ein.") {
		t.Fatalf("fremder anbieter sieht:\n%s", got)
	}
	if ans.Content != "Liebe Anna Berg, wir sehen uns in Hamburg." {
		t.Fatalf("antwort %q", ans.Content)
	}
}

// TestProviderReducedRules: Ein Anbieter nimmt reduced von Fremden nur mit
// accept_public_tasks an, trusted von Fremden nie, Audio als reduced nie.
func TestProviderReducedRules(t *testing.T) {
	ctx := t.Context()
	idA, _ := identity.New()
	idB, _ := identity.New()
	idC, _ := identity.New()
	a := newTestNodeWith(t, ctx, idA, "eigen", "mistral", []string{"general"}, 8000, false, nil)
	open := newTestNodeWith(t, ctx, idB, "fremd", "qwen", []string{"general", orchestrator.SkillTranscribe}, 8000, true,
		func(c *config.Config) { c.Resources.AcceptPublicTasks = true })
	closed := newTestNodeWith(t, ctx, idC, "zu", "llama", []string{"general"}, 8000, true, nil)
	for _, n := range []testNode{open, closed} {
		if err := a.P2P.Connect(ctx, n.P2P.Addrs()[0]); err != nil {
			t.Fatal(err)
		}
	}
	// Ohne accept_public_tasks zeigt closed Fremden keine Agenten an; einen
	// Koordinator, der die Regeln umgeht, hält das nicht auf.
	waitFor(t, func() bool { return len(a.Agents()) == 2 })
	agent := func(family string) orchestrator.Agent {
		if family == "llama" {
			return orchestrator.Agent{ID: "fake/zu", PeerID: closed.P2P.ID().String(), Family: "llama", Skills: []string{"general"}}
		}
		for _, x := range a.Agents() {
			if x.Family == family {
				return x
			}
		}
		t.Fatalf("agent %s fehlt", family)
		return orchestrator.Agent{}
	}
	task := func(class protocol.PrivacyClass, audio bool) orchestrator.Task {
		t := orchestrator.Task{TaskID: "t", SubtaskID: "s", Role: protocol.RoleSpecialist, Skill: "general",
			Messages: []protocol.ChatMessage{{Role: "user", Content: "Hallo [PERSON_1]"}},
			Limits:   protocol.Limits{PrivacyClass: class, MaxTokens: 100}}
		if audio {
			t.Skill, t.Messages, t.Audio = orchestrator.SkillTranscribe, nil, &protocol.Audio{Data: make([]byte, 10)}
		}
		return t
	}
	if _, err := a.P2P.Execute(ctx, agent("qwen"), task(protocol.PrivacyReduced, false)); err != nil {
		t.Fatalf("reduced mit accept_public_tasks abgelehnt: %v", err)
	}
	for _, tc := range []struct {
		name, family string
		task         orchestrator.Task
		want         string
	}{
		{"reduced ohne freigabe", "llama", task(protocol.PrivacyReduced, false), "keine öffentlichen aufträge"},
		{"trusted an fremde", "qwen", task(protocol.PrivacyTrusted, false), "nur für vertrauten kreis"},
		{"audio als reduced", "qwen", task(protocol.PrivacyReduced, true), "audio nur im vertrauten kreis"},
	} {
		if _, err := a.P2P.Execute(ctx, agent(tc.family), tc.task); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v", tc.name, err)
		}
	}
}
