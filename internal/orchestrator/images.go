// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Rainer Sestig. Kommerzielle Lizenz: siehe COMMERCIAL.md.

package orchestrator

import (
	"slices"

	"github.com/rainer-sharp/Ai-Swarms.net/internal/protocol"
)

// Bilder in Anfragen (spec/privacy-classes.md Regel 7): Sie gehen nur an
// Agenten, die Bilder verstehen (Modalität "image", in der Konfiguration
// vision: true). Prüfer und Synthese sehen sie auch, sonst könnten sie einen
// Entwurf zu einem Bild nicht beurteilen. Ein Bild lässt sich nicht
// pseudonymisieren; reduced bleibt deshalb im vertrauten Kreis.

func hasImages(msgs []protocol.ChatMessage) bool {
	return slices.ContainsFunc(msgs, func(m protocol.ChatMessage) bool { return len(m.Images) > 0 })
}

// withImages hängt die Bilder der Anfrage an die letzte Nutzer-Nachricht
// eines selbst gebauten Prompts (Prüfung, Synthese).
func withImages(prompt, req []protocol.ChatMessage) []protocol.ChatMessage {
	var imgs []protocol.Image
	for _, m := range req {
		imgs = append(imgs, m.Images...)
	}
	if len(imgs) == 0 {
		return prompt
	}
	out := slices.Clone(prompt)
	for i := len(out) - 1; i >= 0; i-- {
		if out[i].Role == "user" {
			out[i].Images = append(slices.Clone(out[i].Images), imgs...)
			return out
		}
	}
	return append(out, protocol.ChatMessage{Role: "user", Images: imgs})
}
