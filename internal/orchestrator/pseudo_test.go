// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Rainer Sestig und Mitwirkende.

package orchestrator

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/rainer-sharp/Ai-Swarms.net/internal/protocol"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/pseudo"
)

var reducedDir = staticDir{
	{ID: "eigen", Local: true, Family: "mistral", Skills: []string{"general"}},
	{ID: "vertraut", PeerID: "p1", Trusted: true, Family: "llama", Skills: []string{"general"}},
	{ID: "fremd", PeerID: "p9", Family: "qwen", Skills: []string{"general"}},
}

const reducedQuestion = "Schreib Anna Berg (anna.berg@example.org) von Projekt Falke eine Absage."

// detectAnna: Erkennung durch das eigene Modell; „Erfunden“ steht nicht im
// Text und darf nicht übernommen werden.
func detectAnna(t Task) (string, error) {
	if t.Role != protocol.RoleRouter {
		return "Antwort von eigen", nil
	}
	return `{"entities":[{"text":"Anna Berg","kind":"PERSON"},{"text":"Erfunden","kind":"PERSON"}]}`, nil
}

func reducedRun(t *testing.T, fn map[string]func(Task) (string, error), agents []string, stream func(Event)) (*Answer, error) {
	t.Helper()
	o := &Orchestrator{Dir: reducedDir, Exec: &streamExec{fakeExec{fn: fn}}}
	return o.Run(context.Background(), Request{
		Messages: []protocol.ChatMessage{{Role: "user", Content: reducedQuestion}},
		Pattern:  PatternSingle, Privacy: protocol.PrivacyReduced, Agents: agents,
		PseudonymizeTerms: []string{"Projekt Falke"}, Stream: stream,
	})
}

// TestReducedPseudonymizesStrangers: Ein fremder Agent sieht nur Platzhalter
// und den Hinweis dazu; seine Antwort kommt mit den Originalen zurück, auch
// im Stream. Erkannt hat das eigene Modell, die Liste kam dazu.
func TestReducedPseudonymizesStrangers(t *testing.T) {
	var seen string
	fn := map[string]func(Task) (string, error){
		"eigen": detectAnna,
		"fremd": func(t Task) (string, error) {
			for _, m := range t.Messages {
				seen += m.Content + "\n"
			}
			return "Liebe [PERSON_1], zu [TERM_1] schreibe ich an [EMAIL_1].", nil
		},
	}
	ev := &events{}
	ans, err := reducedRun(t, fn, []string{"fremd"}, ev.add)
	if err != nil {
		t.Fatal(err)
	}
	for _, leak := range []string{"Anna Berg", "anna.berg@example.org", "Projekt Falke"} {
		if strings.Contains(seen, leak) {
			t.Fatalf("fremder sieht %q:\n%s", leak, seen)
		}
	}
	if !strings.Contains(seen, "Schreib [PERSON_1] ([EMAIL_1]) von [TERM_1] eine Absage.") || !strings.Contains(seen, pseudo.Hint) {
		t.Fatalf("fremder sieht:\n%s", seen)
	}
	want := "Liebe Anna Berg, zu Projekt Falke schreibe ich an anna.berg@example.org."
	if ans.Content != want || ev.content.String() != want {
		t.Fatalf("antwort %q, stream %q", ans.Content, ev.content.String())
	}
	if len(ans.Steps) != 2 || ans.Steps[0].Role != protocol.RoleRouter || ans.Steps[0].AgentID != "eigen" {
		t.Fatalf("schritte %+v", ans.Steps)
	}
	if !strings.Contains(ev.reasoning.String(), "Für fremde Knoten werden 2 Namen, Firmen, Orte und eigene Begriffe ersetzt") {
		t.Fatalf("denkphase %q", ev.reasoning.String())
	}
}

// TestReducedTrustedSeesOriginal: Vertraute Agenten bekommen das Original,
// ohne Erkennungsschritt.
func TestReducedTrustedSeesOriginal(t *testing.T) {
	var seen string
	fn := map[string]func(Task) (string, error){
		"eigen":    func(Task) (string, error) { return "", errors.New("erkennung nicht nötig") },
		"vertraut": func(t Task) (string, error) { seen = t.Messages[len(t.Messages)-1].Content; return "ok", nil },
	}
	ans, err := reducedRun(t, fn, []string{"vertraut"}, nil)
	if err != nil || seen != reducedQuestion || len(ans.Steps) != 1 {
		t.Fatalf("%v %q %+v", err, seen, ans.Steps)
	}
}

// TestReducedWithoutDetectionStaysTrusted: Scheitert die Erkennung, bekommt
// kein fremder Agent etwas; mit nur Fremden ist das ein Fehler der
// Freigabeklasse (400), kein Ausfall.
func TestReducedWithoutDetectionStaysTrusted(t *testing.T) {
	called := false
	fn := map[string]func(Task) (string, error){
		"eigen": func(Task) (string, error) { return "das ist kein json", nil },
		"fremd": func(Task) (string, error) { called = true; return "x", nil },
	}
	_, err := reducedRun(t, fn, []string{"fremd"}, nil)
	if called || !errors.Is(err, ErrPrivacy) || !strings.Contains(err.Error(), "freigabeklasse reduced") {
		t.Fatalf("fremd aufgerufen %v, fehler %v", called, err)
	}
}

// TestReducedAudioStaysTrusted: Eine Aufnahme lässt sich nicht
// pseudonymisieren und geht bei reduced nie an Fremde.
func TestReducedAudioStaysTrusted(t *testing.T) {
	dir := staticDir{{ID: "whisper", PeerID: "p9", Family: "whisper", Skills: []string{SkillTranscribe}}}
	called := false
	o := &Orchestrator{Dir: dir, Exec: &fakeExec{fn: map[string]func(Task) (string, error){
		"whisper": func(Task) (string, error) { called = true; return "text", nil },
	}}}
	_, err := o.Run(context.Background(), Request{Privacy: protocol.PrivacyReduced,
		Audio: &protocol.Audio{Data: []byte("RIFF"), Filename: "a.wav"}})
	if called || !errors.Is(err, ErrPrivacy) {
		t.Fatalf("fremd aufgerufen %v, fehler %v", called, err)
	}
}
