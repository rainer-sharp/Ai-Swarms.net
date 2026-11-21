// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Rainer Sestig. Kommerzielle Lizenz: siehe COMMERCIAL.md.

package node

import (
	"testing"

	"github.com/rainer-sharp/Ai-Swarms.net/internal/config"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/identity"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/orchestrator"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/protocol"
)

// TestSelectionRulesAndBandit: Mit rules gilt tasks.pattern, mit bandit
// wählt das Profil; eine Anfrage mit Muster bekommt es in beiden Fällen.
func TestSelectionRulesAndBandit(t *testing.T) {
	ctx := t.Context()
	ask := func(n testNode, p orchestrator.Pattern) *orchestrator.Answer {
		t.Helper()
		ans, err := n.Ask(ctx, orchestrator.Request{
			Messages: []protocol.ChatMessage{{Role: "user", Content: "Was ist ein Schwarm?"}},
			Pattern:  p, Privacy: protocol.PrivacyLocal,
		})
		if err != nil {
			t.Fatal(err)
		}
		return ans
	}
	id, _ := identity.New()
	rules := newTestNodeWith(t, ctx, id, "m", "qwen", []string{"general"}, 8000, false, func(c *config.Config) {
		c.Tasks.Pattern = "single"
	})
	if a := ask(rules, ""); a.SelectedBy != "rules" || a.Pattern != orchestrator.PatternSingle {
		t.Fatalf("regel: %+v", a)
	}
	if a := ask(rules, orchestrator.PatternDraftVerify); a.SelectedBy != "request" {
		t.Fatalf("vorgegeben: %+v", a)
	}

	id2, _ := identity.New()
	bandit := newTestNodeWith(t, ctx, id2, "m", "qwen", []string{"general"}, 8000, false, func(c *config.Config) {
		c.Cooperation.Selection = "bandit"
	})
	// Eine Familie: besetzbar ist nur single, der Bandit wählt es.
	a := ask(bandit, "")
	if a.SelectedBy != "bandit" || a.Pattern != orchestrator.PatternSingle {
		t.Fatalf("bandit: %+v", a)
	}
	if st := bandit.Profile.Recipes()["general"]["single"]; st.Tasks != 1 {
		t.Fatalf("rezeptstatistik: %+v", bandit.Profile.Recipes())
	}
}
