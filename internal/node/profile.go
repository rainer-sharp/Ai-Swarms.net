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
	for _, s := range ans.Steps {
		key := profile.AgentKey(s.PeerID, s.AgentID)
		steps = append(steps, profile.Step{
			Role: string(s.Role), Agent: key, Family: s.Family,
			OK: s.Status != protocol.StatusFailed, Verdict: s.Verdict, Strength: strength[key],
		})
	}
	n.Profile.Record(ans.TaskID, ans.Skill, steps)
}

// Feedback nimmt eine Rückmeldung zu einem eigenen Auftrag an (POST /v1/feedback).
func (n *Node) Feedback(fb profile.Feedback) error {
	return n.Profile.Feedback(fb)
}

// ProfileJSON zeigt das Kompetenzprofil (GET /api/profile).
func (n *Node) ProfileJSON() any {
	return map[string]any{"agents": n.Profile.Snapshot()}
}
