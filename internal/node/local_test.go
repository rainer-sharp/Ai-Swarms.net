// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Rainer Sestig. Kommerzielle Lizenz: siehe COMMERCIAL.md.

package node

import (
	"strings"
	"testing"

	"github.com/rainer-sharp/Ai-Swarms.net/internal/protocol"
)

func TestWithSchemaKeepsSingleSystemMessage(t *testing.T) {
	in := []protocol.ChatMessage{{Role: "system", Content: "Du bist Prüfer."}, {Role: "user", Content: "x"}}
	out := withSchema(in, `{"type":"object"}`)
	if len(out) != 2 || out[0].Role != "system" || out[1].Role != "user" {
		t.Fatalf("nachrichten: %+v", out)
	}
	if !strings.HasPrefix(out[0].Content, "Du bist Prüfer.") || !strings.Contains(out[0].Content, `{"type":"object"}`) {
		t.Fatalf("system-nachricht: %q", out[0].Content)
	}
	if in[0].Content != "Du bist Prüfer." {
		t.Fatal("eingabe verändert")
	}

	out = withSchema([]protocol.ChatMessage{{Role: "user", Content: "x"}}, `{}`)
	if len(out) != 2 || out[0].Role != "system" {
		t.Fatalf("ohne system-nachricht: %+v", out)
	}
	if got := withSchema(in, ""); len(got) != 2 || got[0].Content != in[0].Content {
		t.Fatal("ohne schema verändert")
	}
}
