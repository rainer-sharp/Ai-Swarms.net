// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Rainer Sestig und Mitwirkende.

package orchestrator

import (
	"cmp"
	"context"
	"fmt"
	"slices"

	"github.com/rainer-sharp/Ai-Swarms.net/internal/protocol"
)

// cascadeMinRating: Unter dieser gemessenen Bewertung (etwa 75 % Erfolg gegen
// die Referenz) gilt ein Modell nicht als „ausreichend“ für die erste Stufe.
const cascadeMinRating = 1650

// feasible: Muster, die sich mit den zugelassenen Agenten besetzen lassen.
// Muster mit Prüfer oder Vielfalt brauchen zwei Modellfamilien.
func (r *run) feasible() []Pattern {
	cands := r.candidates(r.req.Skill, false, nil)
	if len(cands) == 0 {
		return nil
	}
	families := map[string]bool{}
	for _, a := range cands {
		families[a.Family] = true
	}
	if len(families) < 2 {
		return []Pattern{PatternSingle}
	}
	return slices.Clone(Patterns)
}

func (r *run) rating(a Agent) (float64, bool) {
	if r.o.Rate == nil {
		return 0, false
	}
	return r.o.Rate(a, r.req.Skill)
}

func strengthOrDefault(a Agent) int {
	if a.Strength == 0 {
		return 5
	}
	return a.Strength
}

// cheapFirst ordnet für die erste Stufe: passende Fähigkeit zuerst, dann
// Modelle ohne zu schwache Bewertung, dann die kleinere Selbstauskunft
// strength als Näherung für geringere Kosten.
func (r *run) cheapFirst(cands []Agent) []Agent {
	out := slices.Clone(cands)
	weak := func(a Agent) bool { v, ok := r.rating(a); return ok && v < cascadeMinRating }
	slices.SortStableFunc(out, func(a, b Agent) int {
		if sa, sb := skillScore(a, r.req.Skill), skillScore(b, r.req.Skill); sa != sb {
			return cmp.Compare(sb, sa)
		}
		if wa, wb := weak(a), weak(b); wa != wb {
			if wa {
				return 1
			}
			return -1
		}
		return strengthOrDefault(a) - strengthOrDefault(b)
	})
	return out
}

// bestFirst ordnet für die Eskalation: gemessene Bewertung vor Selbstauskunft,
// ohne den Ersteller der ersten Stufe.
func (r *run) bestFirst(cands []Agent, author Agent) []Agent {
	out := slices.DeleteFunc(slices.Clone(cands), func(a Agent) bool { return a.key() == author.key() })
	slices.SortStableFunc(out, func(a, b Agent) int {
		ra, oka := r.rating(a)
		rb, okb := r.rating(b)
		switch {
		case oka != okb:
			if oka {
				return -1
			}
			return 1
		case oka && ra != rb:
			if ra > rb {
				return -1
			}
			return 1
		}
		if sa, sb := skillScore(a, r.req.Skill), skillScore(b, r.req.Skill); sa != sb {
			return cmp.Compare(sb, sa)
		}
		return strengthOrDefault(b) - strengthOrDefault(a)
	})
	return out
}

// cascade: erst ein günstiges Modell, dann Prüfung durch eine andere Familie;
// nur wenn das Ergebnis unsicher ist, löst das beste andere Modell die
// Aufgabe neu. Unsicher heißt (spec/cooperation.md § 5): Mängelbefund des
// Prüfers (bei Mathe mit abweichendem Ergebnis, siehe verify), oder der
// Prüfer meldet „ok“ mit einer Konfidenz unter θ (Regler, nur im Verbund mit
// der Prüfung). Eine fehlende Konfidenz (0) zählt nicht als unsicher.
func (r *run) cascade(ctx context.Context) error {
	cands := r.candidates(r.req.Skill, false, nil)
	if len(cands) == 0 {
		return ErrNoAgent
	}
	draft, author, err := r.exec(ctx, protocol.RoleSpecialist, r.cheapFirst(cands), asIs, "", out{"Entwurf", toReasoning})
	if err != nil {
		return err
	}
	r.ans.Content = draft.Result.Content
	v, ok := r.verify(ctx, author)
	if !ok {
		return nil
	}
	if v.Verdict == "ok" {
		theta := 0.0
		if r.o.Theta != nil {
			theta = r.o.Theta(r.req.Skill)
		}
		if theta <= 0 || v.Confidence <= 0 || v.Confidence >= theta {
			return nil
		}
		r.note(fmt.Sprintf("Prüfer unsicher (Konfidenz %.2f unter der Schwelle %.2f): Eskalation.", v.Confidence, theta))
	}
	best := r.bestFirst(cands, author)
	if len(best) == 0 {
		r.limit("keine eskalation möglich: kein weiteres modell")
		return nil
	}
	res, _, err := r.exec(ctx, protocol.RoleSpecialist, best, asIs, "", out{"Eskalation", toContent})
	if err != nil {
		r.limit("eskalation fehlgeschlagen: antwort mit bekannten mängeln")
		return nil
	}
	r.ans.Content, r.ans.Escalated = res.Result.Content, true
	return nil
}
