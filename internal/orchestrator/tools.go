// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Rainer Sestig und Mitwirkende.

package orchestrator

import (
	"encoding/json"
	"fmt"
	"slices"

	"github.com/rainer-sharp/Ai-Swarms.net/internal/protocol"
)

// Werkzeugrechte nach spec/privacy-classes.md. Der eigene Knoten setzt sie durch,
// weil nur er gefährdet ist: Die Aufrufe eines Modells führt die Anwendung auf
// diesem Rechner aus, und jedes Ergebnis geht zurück an das Modell.

// toolsRestricted: Gilt für diesen Agenten eine Liste? Ein fremder Agent ohne
// ausdrückliche Einordnung gilt immer als eingeschränkt.
func toolsRestricted(a Agent) bool {
	return a.ToolsRestricted || !a.Local && !a.Trusted
}

func toolName(raw json.RawMessage) string {
	var t struct {
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	}
	_ = json.Unmarshal(raw, &t)
	return t.Function.Name
}

// allowedTools nimmt aus den Werkzeugen der Anfrage die freigegebenen. Was sich
// nicht lesen lässt, gilt als nicht freigegeben.
func allowedTools(tools json.RawMessage, allow []string) (json.RawMessage, []string) {
	var list []json.RawMessage
	if err := json.Unmarshal(tools, &list); err != nil {
		return nil, nil
	}
	var kept []json.RawMessage
	var names []string
	for _, t := range list {
		if name := toolName(t); name != "" && slices.Contains(allow, name) {
			kept, names = append(kept, t), append(names, name)
		}
	}
	if len(kept) == 0 {
		return nil, nil
	}
	b, err := json.Marshal(kept)
	if err != nil {
		return nil, nil
	}
	return b, names
}

// historyAllowed: Der Verlauf enthält Aufrufe und Ergebnisse früherer Runden.
// Ein eingeschränkter Agent bekäme so die Ausgabe von Werkzeugen zu sehen, die
// ihm nicht freigegeben sind, und scheidet dann aus.
func historyAllowed(msgs []protocol.ChatMessage, allow []string) bool {
	for _, m := range msgs {
		for _, c := range m.ToolCalls {
			if !slices.Contains(allow, c.Function.Name) {
				return false
			}
		}
		if m.Role == "tool" && m.ToolCallID == "" && !slices.Contains(allow, m.Name) {
			return false // Ergebnis ohne Bezug zu einem Aufruf
		}
	}
	return true
}

// toolsUsable: Kommt der Agent nach den Werkzeugrechten für die Anfrage in Frage?
func toolsUsable(a Agent, req Request) bool {
	if hasTools(req) && !a.Tools {
		return false
	}
	if !toolsRestricted(a) {
		return true
	}
	if !historyAllowed(req.Messages, a.ToolsAllow) {
		return false
	}
	if !hasTools(req) {
		return true
	}
	_, names := allowedTools(req.Tools, a.ToolsAllow)
	return len(names) > 0
}

// toolsFor liefert, was der Agent von den Werkzeugen zu sehen bekommt.
func toolsFor(a Agent, req Request) (tools, choice json.RawMessage) {
	if !hasTools(req) || !toolsRestricted(a) {
		return req.Tools, req.ToolChoice
	}
	kept, names := allowedTools(req.Tools, a.ToolsAllow)
	choice = req.ToolChoice
	if forced := toolName(choice); forced != "" && !slices.Contains(names, forced) {
		choice = json.RawMessage(`"auto"`) // erzwungenes Werkzeug ist nicht freigegeben
	}
	return kept, choice
}

// checkToolCalls verwirft die Antwort eines eingeschränkten Agenten, der ein
// Werkzeug aufruft, das ihm nicht genannt wurde.
func checkToolCalls(a Agent, req Request, calls []protocol.ToolCall) error {
	if !toolsRestricted(a) {
		return nil
	}
	_, names := allowedTools(req.Tools, a.ToolsAllow)
	for _, c := range calls {
		if !slices.Contains(names, c.Function.Name) {
			return fmt.Errorf("modell ruft nicht freigegebenes werkzeug %q auf, verworfen", c.Function.Name)
		}
	}
	return nil
}
