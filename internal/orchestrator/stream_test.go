// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Rainer Sestig. Kommerzielle Lizenz: siehe COMMERCIAL.md.

package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/rainer-sharp/Ai-Swarms.net/internal/protocol"
)

// streamExec streamt die Antwort jedes Agenten Wort für Wort, wenn der
// Schritt es verlangt. Ein Fehler mit Text davor („halb|fehler“) streamt erst
// den Text und scheitert dann.
type streamExec struct{ fakeExec }

func (s *streamExec) Execute(ctx context.Context, a Agent, t Task) (protocol.Result, error) {
	out, err := s.fn[a.ID](t)
	if t.OnDelta != nil {
		text := out
		if err != nil {
			text, _, _ = strings.Cut(err.Error(), "|")
			if text == err.Error() {
				text = ""
			}
		}
		for i, w := range strings.SplitAfter(text, " ") {
			rs := ""
			if i == 0 {
				rs = "(denkt)"
			}
			t.OnDelta(w, rs)
		}
	}
	return s.fakeExec.Execute(ctx, a, t)
}

// events sammelt die Zwischenstände.
type events struct {
	mu                 sync.Mutex
	reasoning, content strings.Builder
	order              []string
}

func (e *events) add(ev Event) {
	e.mu.Lock()
	defer e.mu.Unlock()
	switch ev.Kind {
	case EventReasoning:
		e.reasoning.WriteString(ev.Text)
		e.order = append(e.order, "r")
	case EventContent:
		e.content.WriteString(ev.Text)
		e.order = append(e.order, "c")
	case EventRestart:
		e.content.WriteString("<neu>")
		e.order = append(e.order, "x")
	}
}

func streamRun(t *testing.T, fn map[string]func(Task) (string, error), req Request) (*Answer, *events) {
	t.Helper()
	ev := &events{}
	req.Stream = ev.add
	req.Privacy = protocol.PrivacyTrusted
	if req.Messages == nil {
		req.Messages = []protocol.ChatMessage{{Role: "user", Content: "Schreibe eine Python-Funktion"}}
	}
	o := &Orchestrator{Dir: agents, Exec: &streamExec{fakeExec{fn: fn}}}
	ans, err := o.Run(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	return ans, ev
}

// TestStreamSingle: Bei single geht der Text direkt in die Antwort, ohne
// Überschriften; das Nachdenken des Modells in die Denkphase.
func TestStreamSingle(t *testing.T) {
	ans, ev := streamRun(t, map[string]func(Task) (string, error){
		"coder": func(Task) (string, error) { return "def f(): return 1", nil },
	}, Request{Pattern: PatternSingle})
	if ev.content.String() != ans.Content || ev.reasoning.String() != "(denkt)" {
		t.Fatalf("antwort %q, denkphase %q", ev.content.String(), ev.reasoning.String())
	}
}

// TestStreamDraftVerify: Entwurf und Urteil stehen in der Denkphase, die
// Überarbeitung ist die Antwort; das JSON des Prüfers erscheint nicht.
func TestStreamDraftVerify(t *testing.T) {
	fn := map[string]func(Task) (string, error){
		"coder": func(Task) (string, error) { return "def f(): return 1", nil },
		"llama": func(Task) (string, error) {
			return `{"verdict":"issues","issues":["fehlender test"],"confidence":0.7}`, nil
		},
		"local": func(Task) (string, error) { return "korrigiert mit test", nil },
	}
	ans, ev := streamRun(t, fn, Request{Pattern: PatternDraftVerify})
	r := ev.reasoning.String()
	for _, want := range []string{"**Entwurf · coder**\n\ndef f(): return 1", "**Prüfung · llama**", "Urteil: Mängel\n- fehlender test",
		"**Überarbeitung · local**", "(denkt)"} {
		if !strings.Contains(r, want) {
			t.Fatalf("denkphase ohne %q:\n%s", want, r)
		}
	}
	if strings.Contains(r, "verdict") || ev.content.String() != ans.Content || ans.Content != "korrigiert mit test" {
		t.Fatalf("denkphase %q, antwort %q", r, ev.content.String())
	}
	if strings.Contains(r, "\n\n\n") {
		t.Fatalf("mehr als eine leerzeile:\n%s", r)
	}
	if !strings.HasPrefix(r, "**Entwurf") {
		t.Fatalf("denkphase beginnt mit leerzeilen: %q", r)
	}

	// Urteil ok: Der Entwurf ist die Antwort, gestreamt wurde er nur als Entwurf.
	fn["llama"] = func(Task) (string, error) { return `{"verdict":"ok","issues":[],"confidence":0.9}`, nil }
	ans, ev = streamRun(t, fn, Request{Pattern: PatternDraftVerify})
	if ev.content.Len() != 0 || !strings.Contains(ev.reasoning.String(), "Urteil: in Ordnung") || ans.Content != "def f(): return 1" {
		t.Fatalf("antwort %q, denkphase %q", ev.content.String(), ev.reasoning.String())
	}
}

// TestStreamRestart: Scheitert ein Schritt, nachdem er schon Antworttext
// gestreamt hat, kommt ein Neustart und die Fassung des nächsten Kandidaten.
func TestStreamRestart(t *testing.T) {
	ans, ev := streamRun(t, map[string]func(Task) (string, error){
		"coder": func(Task) (string, error) { return "", errors.New("halb fertig |verbindung weg") },
		"llama": func(Task) (string, error) { return "ganze antwort", nil },
	}, Request{Pattern: PatternSingle, Skill: "code.python"})
	if ev.content.String() != "halb fertig <neu>ganze antwort" || ans.Content != "ganze antwort" {
		t.Fatalf("%q", ev.content.String())
	}
}

// toolExec streamt Text und liefert Werkzeug-Aufrufe nur im Ergebnis.
type toolExec struct {
	calls map[string][]protocol.ToolCall
}

func (x toolExec) Execute(_ context.Context, a Agent, t Task) (protocol.Result, error) {
	if t.OnDelta != nil {
		t.OnDelta("Ich lese ", "")
		t.OnDelta("die Datei.", "")
	}
	return protocol.Result{Role: t.Role, Result: protocol.ResultBody{Status: protocol.StatusOK,
		Content: "Ich lese die Datei.", ToolCalls: x.calls[a.ID]}}, nil
}

// TestStreamWithTools: Mit Werkzeugen läuft der Text live, die Aufrufe kommen
// nur mit dem Ergebnis. Ruft ein eingeschränkter Agent ein nicht
// freigegebenes Werkzeug auf, wird sein schon gestreamter Text verworfen.
func TestStreamWithTools(t *testing.T) {
	tools := json.RawMessage(`[{"type":"function","function":{"name":"read_file"}},{"type":"function","function":{"name":"run_shell"}}]`)
	dir := staticDir{
		{ID: "fremd", PeerID: "p1", Family: "x", Skills: []string{"general"}, Tools: true, ToolsRestricted: true, ToolsAllow: []string{"read_file"}},
		{ID: "eigen", Local: true, Family: "y", Skills: []string{"general"}, Tools: true},
	}
	call := func(name string) []protocol.ToolCall {
		return []protocol.ToolCall{{ID: "c1", Type: "function", Function: protocol.ToolFunction{Name: name, Arguments: "{}"}}}
	}
	run := func(x toolExec) (*Answer, *events) {
		ev := &events{}
		o := &Orchestrator{Dir: dir, Exec: x}
		ans, err := o.Run(context.Background(), Request{
			Messages: []protocol.ChatMessage{{Role: "user", Content: "lies datei"}}, Privacy: protocol.PrivacyPublic,
			Tools: tools, Stream: ev.add, Agents: []string{"fremd", "eigen"},
		})
		if err != nil {
			t.Fatal(err)
		}
		return ans, ev
	}
	ans, ev := run(toolExec{calls: map[string][]protocol.ToolCall{"fremd": call("read_file")}})
	if ev.content.String() != "Ich lese die Datei." || len(ans.ToolCalls) != 1 || ans.ToolCalls[0].Function.Name != "read_file" {
		t.Fatalf("%q %+v", ev.content.String(), ans.ToolCalls)
	}
	// fremd ruft run_shell auf (nicht freigegeben): verworfen, eigen antwortet.
	ans, ev = run(toolExec{calls: map[string][]protocol.ToolCall{"fremd": call("run_shell"), "eigen": call("run_shell")}})
	if ev.content.String() != "Ich lese die Datei.<neu>Ich lese die Datei." || len(ans.Steps) != 2 || ans.Steps[0].Status != protocol.StatusFailed {
		t.Fatalf("%q %+v", ev.content.String(), ans.Steps)
	}
}
