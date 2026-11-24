// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Rainer Sestig. Kommerzielle Lizenz: siehe COMMERCIAL.md.

package node

import (
	"github.com/rainer-sharp/Ai-Swarms.net/internal/orchestrator"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/profile"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/protocol"
)

// recordProfile übergibt einen abgeschlossenen Auftrag an das Kompetenzprofil
// (spec/cooperation.md § 2). Die Auswahl der Agenten nutzt es noch nicht.
func (n *Node) recordProfile(ans *orchestrator.Answer) {
	if n.Profile == nil || ans == nil {
		return
	}
	strength := map[string]int{}
	for _, a := range n.Agents() {
		strength[profile.AgentKey(a.PeerID, a.ID)] = a.Strength
	}
	steps := make([]profile.Step, 0, len(ans.Steps))
	tokens := 0
	for _, s := range ans.Steps {
		tokens += s.PromptTokens + s.CompletionTokens
		key := profile.AgentKey(s.PeerID, s.AgentID)
		steps = append(steps, profile.Step{
			Role: string(s.Role), Agent: key, Family: s.Family,
			OK: s.Status != protocol.StatusFailed, Verdict: s.Verdict, Strength: strength[key], Node: s.Node, Skill: s.Skill,
		})
	}
	n.Profile.Record(profile.TaskInfo{ID: ans.TaskID, Skill: ans.Skill, Pattern: string(ans.Pattern), Tokens: tokens, Steps: steps,
		Escalated: ans.Escalated})
}

// rate liefert dem Orchestrator die gemessene Bewertung eines Agenten (Kaskade).
func (n *Node) rate(a orchestrator.Agent, skill string) (float64, bool) {
	if n.Profile == nil {
		return 0, false
	}
	return n.Profile.Rating(profile.AgentKey(a.PeerID, a.ID), skill)
}

// selection: Einstellungen für Bandit und Regler aus cooperation.
func (n *Node) selection() profile.Selection {
	sel := profile.Selection{Exploration: n.Cfg.Cooperation.Exploration, Targets: map[string]float64{}, CascadeErrors: map[string]float64{}}
	for cat, tg := range n.Cfg.Cooperation.Targets {
		if tg.Quality > 0 {
			sel.Targets[cat] = tg.Quality
		}
		if tg.CascadeError > 0 {
			sel.CascadeErrors[cat] = tg.CascadeError
		}
	}
	return sel
}

// choose wählt das Muster per Bandit (cooperation.selection: bandit).
func (n *Node) choose(c orchestrator.Choice) orchestrator.Pattern {
	feasible := make([]string, len(c.Feasible))
	for i, p := range c.Feasible {
		feasible[i] = string(p)
	}
	return orchestrator.Pattern(n.Profile.Choose(c.Skill, feasible, string(c.Default), n.selection(), nil))
}

// Feedback nimmt eine Rückmeldung zu einem eigenen Auftrag an (POST /v1/feedback).
func (n *Node) Feedback(fb profile.Feedback) error {
	return n.Profile.Feedback(fb)
}

// ProfileJSON zeigt das Kompetenzprofil (GET /api/profile).
func (n *Node) ProfileJSON() any {
	return map[string]any{"selection": n.Cfg.Cooperation.Selection, "agents": n.Profile.Snapshot(), "recipes": n.Profile.Recipes(),
		"controls": n.Profile.Controls()}
}
