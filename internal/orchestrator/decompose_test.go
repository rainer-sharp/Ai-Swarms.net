// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Rainer Sestig und Mitwirkende.

package orchestrator

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/rainer-sharp/Ai-Swarms.net/internal/protocol"
)

const testPlan = `{"nodes":[
 {"id":"a","skill":"code.python","prompt":"Schreibe die Funktion f.","deps":[]},
 {"id":"b","skill":"code.review","prompt":"Prüfe die Funktion f.","deps":["a"]},
 {"id":"c","skill":"general","prompt":"Erkläre Rekursion.","deps":[]}],
 "synthesis":"Code, Prüfung und Erklärung zusammenführen."}`

// graphExec spielt Planer, Teilaufgaben und Synthese.
func graphExec(plan string, fail func(Task) bool) *fakeExec {
	reply := func(t Task) (string, error) {
		last := t.Messages[len(t.Messages)-1].Content
		switch {
		case fail != nil && fail(t):
			return "", errors.New("modell gescheitert")
		case t.Role == protocol.RolePlanner:
			return plan, nil
		case t.Role == protocol.RoleSynthesizer:
			return "gesamt:\n" + last, nil
		case strings.Contains(last, "Schreibe die Funktion f"):
			return "def f(): pass", nil
		case strings.Contains(last, "Prüfe die Funktion f"):
			if !strings.Contains(last, "<ergebnis>\nTeilaufgabe a:\ndef f(): pass") {
				return "", errors.New("ergebnis von a fehlt")
			}
			return "f ist in Ordnung", nil
		case strings.Contains(last, "Erkläre Rekursion"):
			return "Rekursion ruft sich selbst auf.", nil
		}
		return "", errors.New("unerwarteter auftrag: " + last)
	}
	return &fakeExec{fn: map[string]func(Task) (string, error){"coder": reply, "llama": reply, "local": reply}}
}

func TestDecomposeGraph(t *testing.T) {
	ex := graphExec(testPlan, nil)
	o := &Orchestrator{Dir: agents, Exec: ex, Default: PatternSingle}
	ans, err := o.Run(context.Background(), Request{
		Messages: []protocol.ChatMessage{{Role: "user", Content: "Schreib f in Python, lass sie prüfen und erklär Rekursion."}},
		Pattern:  PatternDecompose, Privacy: protocol.PrivacyTrusted,
	})
	if err != nil {
		t.Fatal(err)
	}
	if ans.Pattern != PatternDecompose || len(ans.Limitations) != 0 {
		t.Fatalf("muster %s, einschränkungen %v", ans.Pattern, ans.Limitations)
	}
	for _, want := range []string{"def f(): pass", "f ist in Ordnung", "Rekursion ruft sich selbst auf.", "<plan>"} {
		if !strings.Contains(ans.Content, want) {
			t.Fatalf("synthese ohne %q:\n%s", want, ans.Content)
		}
	}
	if len(ans.Graph) != 3 {
		t.Fatalf("graph: %+v", ans.Graph)
	}
	for _, g := range ans.Graph {
		if g.Status != protocol.StatusOK || g.Pattern != PatternSingle {
			t.Fatalf("teilaufgabe: %+v", g)
		}
	}
	// Planer und Synthese bevorzugt lokal; die Teilaufgaben mit ihrer Fähigkeit.
	if ex.calls[0] != "planner:local" || ex.calls[len(ex.calls)-1] != "synthesizer:local" {
		t.Fatalf("ablauf: %v", ex.calls)
	}
	nodes := map[string]Step{}
	for _, s := range ans.Steps {
		if s.Node != "" {
			nodes[s.Node] = s
		}
	}
	if nodes["a"].Skill != "code.python" || nodes["a"].AgentID != "coder" || nodes["b"].Skill != "code.review" || nodes["c"].Skill != "general" {
		t.Fatalf("schritte: %+v", ans.Steps)
	}
}

// TestDecomposeFailure: Scheitert eine Teilaufgabe, entfallen ihre
// Nachfolger; die Synthese bekommt die Lücke genannt.
func TestDecomposeFailure(t *testing.T) {
	ex := graphExec(testPlan, func(t Task) bool {
		return t.Role == protocol.RoleSpecialist && strings.Contains(t.Messages[len(t.Messages)-1].Content, "Schreibe die Funktion f")
	})
	o := &Orchestrator{Dir: agents, Exec: ex, Default: PatternSingle}
	ans, err := o.Run(context.Background(), Request{
		Messages: []protocol.ChatMessage{{Role: "user", Content: "drei dinge"}},
		Pattern:  PatternDecompose, Privacy: protocol.PrivacyTrusted,
	})
	if err != nil {
		t.Fatal(err)
	}
	status := map[string]string{}
	for _, g := range ans.Graph {
		status[g.ID] = g.Status
	}
	if status["a"] != protocol.StatusFailed || status["b"] != "skipped" || status["c"] != protocol.StatusOK {
		t.Fatalf("status: %v", status)
	}
	if !strings.Contains(ans.Content, "(kein Ergebnis") || !strings.Contains(ans.Content, "Rekursion ruft sich selbst auf.") {
		t.Fatalf("synthese: %s", ans.Content)
	}
	lim := strings.Join(ans.Limitations, "|")
	if !strings.Contains(lim, "teilaufgabe a gescheitert") || !strings.Contains(lim, "teilaufgabe b entfällt") {
		t.Fatalf("einschränkungen: %v", ans.Limitations)
	}
	if slices.ContainsFunc(ex.calls, func(c string) bool { return strings.Contains(c, "Prüfe") }) {
		t.Fatal("nachfolger lief trotzdem")
	}
}

// TestDecomposeUndivided: Ohne sinnvolle Zerlegung läuft der Auftrag mit
// dem Muster nach Regel; ein ungültiger Plan wird als Einschränkung genannt.
func TestDecomposeUndivided(t *testing.T) {
	question := []protocol.ChatMessage{{Role: "user", Content: "Erkläre Rekursion."}}
	for _, c := range []struct {
		name, plan, limit string
		maxSubtasks       int
	}{
		{"eine teilaufgabe", `{"nodes":[{"id":"a","skill":"general","prompt":"Erkläre Rekursion.","deps":[]}],"synthesis":""}`, "", 0},
		{"kreis", `{"nodes":[{"id":"a","skill":"general","prompt":"x","deps":["b"]},{"id":"b","skill":"general","prompt":"y","deps":["a"]}],"synthesis":""}`, "plan ungültig", 0},
		{"kein json", "Ich würde das so machen …", "plan ungültig", 0},
		{"budget", testPlan, "", 4},
	} {
		ex := graphExec(c.plan, nil)
		o := &Orchestrator{Dir: agents, Exec: ex, Default: PatternSingle}
		ans, err := o.Run(context.Background(), Request{Messages: question, Pattern: PatternDecompose,
			Privacy: protocol.PrivacyTrusted, Limits: protocol.Limits{MaxSubtasks: c.maxSubtasks}})
		if err != nil || ans.Pattern != PatternSingle || ans.Content != "Rekursion ruft sich selbst auf." || len(ans.Graph) != 0 {
			t.Errorf("%s: %v %+v", c.name, err, ans)
			continue
		}
		lim := strings.Join(ans.Limitations, "|")
		if c.limit == "" && lim != "" || !strings.Contains(lim, c.limit) {
			t.Errorf("%s: einschränkungen %v", c.name, ans.Limitations)
		}
		if c.maxSubtasks > 0 && slices.Contains(ex.calls, "planner:local") {
			t.Errorf("%s: planer trotz zu kleinem budget", c.name)
		}
	}
}

func TestParsePlan(t *testing.T) {
	known := []string{"code.python", "general"}
	for _, c := range []struct{ name, in, err string }{
		{"doppelte id", `{"nodes":[{"id":"a","prompt":"x"},{"id":"a","prompt":"y"}]}`, "doppelte id"},
		{"unbekannter vorgänger", `{"nodes":[{"id":"a","prompt":"x","deps":["z"]}]}`, "unbekannter vorgänger"},
		{"sich selbst", `{"nodes":[{"id":"a","prompt":"x","deps":["a"]}]}`, "unbekannter vorgänger"},
		{"leer", `{"nodes":[{"id":"a","prompt":"  "}]}`, "text leer"},
		{"zu viele", `{"nodes":[{"id":"a","prompt":"x"},{"id":"b","prompt":"x"},{"id":"c","prompt":"x"}]}`, "erlaubt sind 2"},
		{"id", `{"nodes":[{"id":"a b","prompt":"x"}]}`, "ungültige"},
	} {
		if _, err := parsePlan(c.in, 2, known); err == nil || !strings.Contains(err.Error(), c.err) {
			t.Errorf("%s: %v", c.name, err)
		}
	}
	p, err := parsePlan(`Plan: {"nodes":[{"id":"a","skill":"code.python","prompt":"x"},{"id":"b","skill":"zauberei","prompt":"y","deps":["a","a"]}]}`, 2, known)
	if err != nil || p.Nodes[0].Skill != "code.python" || p.Nodes[1].Skill != "general" || len(p.Nodes[1].Deps) != 1 {
		t.Fatalf("%v %+v", err, p)
	}
}

// TestDecomposeReduced: Bei reduced bekommt ein fremder Agent seine
// Teilaufgabe pseudonymisiert; das Ergebnis kommt mit dem Original zurück.
// Planer und Synthese bleiben auf dem eigenen Knoten.
func TestDecomposeReduced(t *testing.T) {
	plan := `{"nodes":[{"id":"a","skill":"code.python","prompt":"Schreibe Code für Projekt Falke.","deps":[]},
	 {"id":"b","skill":"general","prompt":"Erkläre Rekursion.","deps":[]}],"synthesis":""}`
	var mu sync.Mutex
	var seen []string
	ex := &fakeExec{fn: map[string]func(Task) (string, error){
		"local": func(t Task) (string, error) {
			switch {
			case t.Role == protocol.RolePlanner:
				return plan, nil
			case t.JSONSchema == entitySchema:
				return `{"entities":[]}`, nil
			case t.Role == protocol.RoleSynthesizer:
				return t.Messages[1].Content, nil
			}
			return "Rekursion ruft sich selbst auf.", nil
		},
		"fremd": func(t Task) (string, error) {
			mu.Lock()
			seen = append(seen, t.Messages[len(t.Messages)-1].Content)
			mu.Unlock()
			return "# Code für [TERM_1]", nil
		},
	}}
	dir := staticDir{
		{ID: "local", Local: true, Family: "mistral", Skills: []string{"general"}},
		{ID: "fremd", PeerID: "p9", Family: "qwen", Skills: []string{"code.python"}},
	}
	o := &Orchestrator{Dir: dir, Exec: ex, Default: PatternSingle}
	ans, err := o.Run(context.Background(), Request{
		Messages: []protocol.ChatMessage{{Role: "user", Content: "Code für Projekt Falke und eine Erklärung"}},
		Pattern:  PatternDecompose, Privacy: protocol.PrivacyReduced, PseudonymizeTerms: []string{"Projekt Falke"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(seen) != 1 || strings.Contains(seen[0], "Falke") || !strings.Contains(seen[0], "[TERM_1]") {
		t.Fatalf("fremder sah: %q", seen)
	}
	if !strings.Contains(ans.Content, "# Code für Projekt Falke") {
		t.Fatalf("antwort: %s", ans.Content)
	}
	if ex.calls[0] != "planner:local" || ex.calls[len(ex.calls)-1] != "synthesizer:local" {
		t.Fatalf("ablauf: %v", ex.calls)
	}
}

// TestDecomposeResume: Ein Ersatzkoordinator setzt am Prüfpunkt fort: ohne
// Planer, fertige Teilaufgaben laufen nicht erneut. Prüfpunkte kommen nach
// dem Plan und nach jeder fertigen Teilaufgabe.
func TestDecomposeResume(t *testing.T) {
	ex := graphExec(testPlan, nil)
	o := &Orchestrator{Dir: agents, Exec: ex, Default: PatternSingle}
	var cps []Checkpoint
	var mu sync.Mutex
	ans, err := o.Run(context.Background(), Request{
		Messages: []protocol.ChatMessage{{Role: "user", Content: "drei dinge"}},
		Pattern:  PatternDecompose, Privacy: protocol.PrivacyTrusted,
		OnCheckpoint: func(cp Checkpoint) { mu.Lock(); cps = append(cps, cp); mu.Unlock() },
	})
	if err != nil || len(cps) != 4 || len(cps[0].Results) != 0 || len(cps[3].Results) != 3 {
		t.Fatalf("%v, %d prüfpunkte", err, len(cps))
	}

	// Fortsetzen mit a fertig: Planer und a laufen nicht, b bekommt a's Ergebnis.
	resume := Checkpoint{Plan: cps[0].Plan, Results: map[string]string{"a": "def f(): pass"}}
	ex = graphExec("darf nicht gefragt werden", nil)
	o.Exec = ex
	ans, err = o.Run(context.Background(), Request{
		Messages: []protocol.ChatMessage{{Role: "user", Content: "drei dinge"}},
		Pattern:  PatternDecompose, Privacy: protocol.PrivacyTrusted, Resume: &resume,
	})
	if err != nil || !strings.Contains(ans.Content, "f ist in Ordnung") || len(ans.Graph) != 3 {
		t.Fatalf("%v %+v", err, ans)
	}
	for _, c := range ex.calls {
		if strings.HasPrefix(c, "planner:") {
			t.Fatal("planer trotz prüfpunkt")
		}
	}
	if n := len(ex.calls); n != 3 { // b, c, synthese
		t.Fatalf("aufrufe: %v", ex.calls)
	}
}

// TestDecomposeReservesSynthesis: Auch wenn die Teilaufgaben das Budget
// ausschöpfen, bleibt ein Teilauftrag für die Synthese.
func TestDecomposeReservesSynthesis(t *testing.T) {
	ex := graphExec(testPlan, nil)
	// Nach Regel mit Prüfung: Jede Teilaufgabe kostet Entwurf und Prüfung.
	o := &Orchestrator{Dir: agents, Exec: ex, Default: PatternDraftVerify}
	ans, err := o.Run(context.Background(), Request{
		Messages: []protocol.ChatMessage{{Role: "user", Content: "drei dinge"}},
		Pattern:  PatternDecompose, Privacy: protocol.PrivacyTrusted, Limits: protocol.Limits{MaxSubtasks: 8},
	})
	if err != nil {
		t.Fatal(err)
	}
	if last := ex.calls[len(ex.calls)-1]; last != "synthesizer:local" || len(ex.calls) > 8 {
		t.Fatalf("ablauf: %v", ex.calls)
	}
	if !strings.HasPrefix(ans.Content, "gesamt:") {
		t.Fatalf("keine synthese: %q %v", ans.Content, ans.Limitations)
	}
}
