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

// Verlauf mit Angaben, die für die letzte Frage nicht gebraucht werden.
var historyConv = []protocol.ChatMessage{
	{Role: "system", Content: "Nutzerin: Anna Berg, Diagnose Diabetes, wohnt in Kiel."},
	{Role: "user", Content: "Ich brauche einen Brief an meine Vermieterin Frau Holm."},
	{Role: "assistant", Content: "Worum soll es gehen?"},
	{Role: "user", Content: "Die Heizung ist seit Montag kalt. Kurz und höflich bitte."},
}

// minimizeOwn: eigenes Modell kürzt (mit anderer Schreibweise des Namens)
// und erkennt Namen; als Prüfer meldet es keine Mängel.
func minimizeOwn(t Task) (string, error) {
	sys := t.Messages[0].Content
	switch {
	case strings.Contains(sys, "nur das Nötigste"):
		return `{"task":"Schreib einen kurzen, höflichen Brief an die Vermieterin Frau Holm: Die Heizung ist seit Montag kalt."}`, nil
	case strings.Contains(sys, "persönliche Angaben in einem Text"):
		return `{"entities":[{"text":"Anna Berg","kind":"PERSON"},{"text":"Kiel","kind":"LOCATION"},{"text":"Holm","kind":"PERSON"}]}`, nil
	}
	return `{"verdict":"ok","issues":[],"confidence":0.9}`, nil
}

// TestReducedMinimizesHistory: Ein fremder Agent bekommt nur die gekürzte
// Aufgabe, pseudonymisiert; Verlauf und Systemanweisung bleiben beim eigenen
// Knoten. Auch der Prüfer sieht als Aufgabe nur die gekürzte Fassung. Holm
// heißt beim Fremden [PERSON_1], obwohl Anna Berg zuerst erkannt wurde.
func TestReducedMinimizesHistory(t *testing.T) {
	var mu sync.Mutex
	seen := map[string]string{}
	fn := map[string]func(Task) (string, error){
		"eigen": minimizeOwn,
		"fremd": func(t Task) (string, error) {
			mu.Lock()
			for _, m := range t.Messages {
				seen[string(t.Role)] += m.Role + ": " + m.Content + "\n"
			}
			mu.Unlock()
			if t.Role == protocol.RoleVerifier {
				return `{"verdict":"ok","issues":[],"confidence":0.9}`, nil
			}
			return "Sehr geehrte Frau [PERSON_1], die Heizung ist kalt.", nil
		},
	}
	o := &Orchestrator{Dir: reducedDir, Exec: &fakeExec{fn: fn}}
	ev := &events{}
	for _, pattern := range []Pattern{PatternSingle, PatternDraftVerify} {
		seen = map[string]string{}
		ans, err := o.Run(context.Background(), Request{Messages: historyConv, Pattern: pattern, Privacy: protocol.PrivacyReduced,
			Agents: []string{"fremd"}, Stream: ev.add})
		if err != nil {
			t.Fatal(err)
		}
		if ans.Content != "Sehr geehrte Frau Holm, die Heizung ist kalt." {
			t.Fatalf("%s: antwort %q", pattern, ans.Content)
		}
		all := seen[string(protocol.RoleSpecialist)] + seen[string(protocol.RoleVerifier)]
		for _, leak := range []string{"Anna Berg", "Diabetes", "Kiel", "Holm", "Worum soll es gehen"} {
			if strings.Contains(all, leak) {
				t.Fatalf("%s: fremder sieht %q:\n%s", pattern, leak, all)
			}
		}
		if !strings.Contains(seen[string(protocol.RoleSpecialist)], "user: Schreib einen kurzen, höflichen Brief an die Vermieterin Frau [PERSON_1]") {
			t.Fatalf("%s: fremder sieht:\n%s", pattern, all)
		}
	}
	if !strings.Contains(ev.reasoning.String(), "Für fremde Knoten gekürzt: 4 Nachrichten") {
		t.Fatalf("denkphase %q", ev.reasoning.String())
	}
}

// TestReducedVerifierGetsMinimized: Ist der Prüfer fremd, sieht er als
// Aufgabe nur die gekürzte Fassung, den Entwurf pseudonymisiert.
func TestReducedVerifierGetsMinimized(t *testing.T) {
	dir := staticDir{
		{ID: "eigen", Local: true, Family: "mistral", Skills: []string{"general"}},
		{ID: "fremd", PeerID: "p9", Family: "qwen", Skills: []string{"general"}},
	}
	var verifier string
	fn := map[string]func(Task) (string, error){
		"eigen": func(t Task) (string, error) {
			if t.Role == protocol.RoleSpecialist {
				return "Sehr geehrte Frau Holm, die Heizung in Kiel ist kalt.", nil
			}
			return minimizeOwn(t)
		},
		"fremd": func(t Task) (string, error) {
			verifier = t.Messages[len(t.Messages)-1].Content
			return `{"verdict":"ok","issues":[],"confidence":0.9}`, nil
		},
	}
	o := &Orchestrator{Dir: dir, Exec: &fakeExec{fn: fn}}
	ans, err := o.Run(context.Background(), Request{Messages: historyConv, Pattern: PatternDraftVerify, Privacy: protocol.PrivacyReduced})
	if err != nil || ans.Steps[len(ans.Steps)-1].AgentID != "fremd" {
		t.Fatalf("%v %+v", err, ans)
	}
	for _, leak := range []string{"Anna Berg", "Diabetes", "Kiel", "Holm", "Worum soll es gehen"} {
		if strings.Contains(verifier, leak) {
			t.Fatalf("prüfer sieht %q:\n%s", leak, verifier)
		}
	}
	if !strings.Contains(verifier, "<aufgabe>\nuser: Schreib einen kurzen") || !strings.Contains(verifier, "die Heizung in [LOCATION_1] ist kalt") {
		t.Fatalf("prüfer sieht:\n%s", verifier)
	}
}

// TestReducedMinimizeFails: Lässt sich nicht kürzen, bekommt kein Fremder
// etwas.
func TestReducedMinimizeFails(t *testing.T) {
	called := false
	fn := map[string]func(Task) (string, error){
		"eigen": func(t Task) (string, error) {
			if strings.Contains(t.Messages[0].Content, "nur das Nötigste") {
				return `{"task":""}`, nil
			}
			return minimizeOwn(t)
		},
		"fremd": func(Task) (string, error) { called = true; return "x", nil },
	}
	o := &Orchestrator{Dir: reducedDir, Exec: &fakeExec{fn: fn}}
	_, err := o.Run(context.Background(), Request{Messages: historyConv, Pattern: PatternSingle, Privacy: protocol.PrivacyReduced, Agents: []string{"fremd"}})
	if called || !errors.Is(err, ErrPrivacy) || !strings.Contains(err.Error(), "kürzen lieferte keine aufgabe") {
		t.Fatalf("fremd aufgerufen %v, fehler %v", called, err)
	}
}
