// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Rainer Sestig und Mitwirkende.

package orchestrator

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/rainer-sharp/Ai-Swarms.net/internal/protocol"
)

var imageQuestion = []protocol.ChatMessage{{Role: "user", Content: "Was zeigt das Bild?",
	Images: []protocol.Image{{MIME: "image/png", Data: []byte("png")}}}}

// TestImagesOnlyToVision: Bilder gehen nur an Agenten mit Vision, auch an den
// Prüfer, der sie zum Beurteilen braucht; ein Modell ohne Vision bleibt außen vor.
func TestImagesOnlyToVision(t *testing.T) {
	dir := staticDir{
		{ID: "text", Local: true, Family: "mistral", Skills: []string{"general"}, Strength: 9},
		{ID: "sehen", Local: true, Family: "qwen", Skills: []string{"general"}, Vision: true},
		{ID: "pruefen", PeerID: "p1", Trusted: true, Family: "gemma", Skills: []string{"general"}, Vision: true},
	}
	var mu sync.Mutex
	saw := map[string]int{}
	see := func(answer string) func(Task) (string, error) {
		return func(t Task) (string, error) {
			mu.Lock()
			for _, m := range t.Messages {
				saw[string(t.Role)] += len(m.Images)
			}
			mu.Unlock()
			return answer, nil
		}
	}
	ex := &fakeExec{fn: map[string]func(Task) (string, error){
		"text":    func(Task) (string, error) { t.Fatal("modell ohne vision bekam bilder"); return "", nil },
		"sehen":   see("Eine Katze."),
		"pruefen": see(`{"verdict":"ok","issues":[],"confidence":0.9}`),
	}}
	o := &Orchestrator{Dir: dir, Exec: ex}
	ans, err := o.Run(context.Background(), Request{Messages: imageQuestion, Pattern: PatternDraftVerify, Privacy: protocol.PrivacyTrusted})
	if err != nil || ans.Content != "Eine Katze." {
		t.Fatalf("%v %+v", err, ans)
	}
	if saw[string(protocol.RoleSpecialist)] != 1 || saw[string(protocol.RoleVerifier)] != 1 {
		t.Fatalf("bilder je rolle %v, aufrufe %v", saw, ex.calls)
	}
}

// TestImagesReducedStaysTrusted: Ein Bild lässt sich nicht pseudonymisieren;
// bei reduced bekommt kein Fremder die Anfrage, und die Antwort sagt warum.
func TestImagesReducedStaysTrusted(t *testing.T) {
	dir := staticDir{{ID: "fremd", PeerID: "p9", Family: "qwen", Skills: []string{"general"}, Vision: true}}
	called := false
	o := &Orchestrator{Dir: dir, Exec: &fakeExec{fn: map[string]func(Task) (string, error){
		"fremd": func(Task) (string, error) { called = true; return "x", nil },
	}}}
	ans, err := o.Run(context.Background(), Request{Messages: imageQuestion, Pattern: PatternSingle, Privacy: protocol.PrivacyReduced,
		Agents: []string{"fremd"}})
	if called || !errors.Is(err, ErrPrivacy) || !strings.Contains(err.Error(), "bilder lassen sich nicht pseudonymisieren") {
		t.Fatalf("fremd aufgerufen %v, fehler %v", called, err)
	}
	if !strings.Contains(strings.Join(ans.Limitations, " "), "bilder lassen sich nicht pseudonymisieren") {
		t.Fatalf("hinweis fehlt: %v", ans.Limitations)
	}
}

// TestImagesNoVision: Ohne Modell mit Vision nennt der Fehler den Grund.
func TestImagesNoVision(t *testing.T) {
	dir := staticDir{{ID: "text", Local: true, Family: "mistral", Skills: []string{"general"}}}
	o := &Orchestrator{Dir: dir, Exec: &fakeExec{}}
	_, err := o.Run(context.Background(), Request{Messages: imageQuestion, Pattern: PatternSingle, Privacy: protocol.PrivacyLocal})
	if !errors.Is(err, ErrNoAgent) || !strings.Contains(err.Error(), "vision: true") {
		t.Fatalf("fehler %v", err)
	}
	big := []protocol.ChatMessage{{Role: "user", Images: []protocol.Image{{MIME: "image/png", Data: make([]byte, protocol.MaxImageBytes+1)}}}}
	if _, err := o.Run(context.Background(), Request{Messages: big, Privacy: protocol.PrivacyLocal}); err == nil || !strings.Contains(err.Error(), "bilder zusammen") {
		t.Fatalf("zu groß: %v", err)
	}
}
