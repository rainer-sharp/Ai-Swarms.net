// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Rainer Sestig und Mitwirkende.

package node

import (
	"errors"
	"strings"
	"testing"

	"github.com/rainer-sharp/Ai-Swarms.net/internal/config"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/identity"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/orchestrator"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/protocol"
)

// TestAudioThroughSwarm: A hat nur ein Textmodell, B ein Transkriptionsmodell.
// Eine Aufnahme von A geht über das Netz an B und kommt als Text zurück; das
// Textmodell bekommt sie nie, und öffentlich geht sie gar nicht hinaus.
func TestAudioThroughSwarm(t *testing.T) {
	ctx := t.Context()
	idA, _ := identity.New()
	idB, _ := identity.New()
	a := newTestNode(t, ctx, idA, "qwen-chat", "qwen", []string{"general"}, 8000, false, idB)
	b := newTestNode(t, ctx, idB, "asr", "nemotron", []string{orchestrator.SkillTranscribe}, 8000, true, idA)
	if err := a.P2P.Connect(ctx, b.P2P.Addrs()[0]); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return len(a.Agents()) == 2 })

	audio := &protocol.Audio{Data: []byte("RIFF-test"), Filename: "a.wav"}
	ans, err := a.Ask(ctx, orchestrator.Request{Audio: audio, Privacy: protocol.PrivacyTrusted})
	if err != nil {
		t.Fatal(err)
	}
	if ans.Content != "ABSCHRIFT von asr: RIFF-test" || ans.Steps[0].PeerID != idB.ID.String() {
		t.Fatalf("%q %+v", ans.Content, ans.Steps)
	}
	if a.calls.Load() != 0 {
		t.Fatal("textmodell hat die aufnahme bekommen")
	}
	if _, err := a.Ask(ctx, orchestrator.Request{Audio: audio, Privacy: protocol.PrivacyPublic}); !errors.Is(err, orchestrator.ErrPrivacy) {
		t.Fatalf("public: %v", err)
	}
	// Eine Textfrage geht nie an das Transkriptionsmodell.
	text, err := a.Ask(ctx, orchestrator.Request{Messages: []protocol.ChatMessage{{Role: "user", Content: "Hallo"}},
		Pattern: orchestrator.PatternSingle, Privacy: protocol.PrivacyTrusted})
	if err != nil || text.Steps[0].Family != "qwen" {
		t.Fatalf("text: %v %+v", err, text)
	}
}

// TestProviderRejectsAudio: Auch wenn ein Koordinator die Regeln umgeht,
// lehnt der Anbieter Audio ab, das öffentlich ist, an einen Agenten ohne
// Audio geht oder zu groß ist.
func TestProviderRejectsAudio(t *testing.T) {
	ctx := t.Context()
	idA, _ := identity.New()
	idB, _ := identity.New()
	a := newTestNodeWith(t, ctx, idA, "qwen-chat", "qwen", []string{"general"}, 8000, false, nil, idB)
	b := newTestNodeWith(t, ctx, idB, "asr", "nemotron", []string{orchestrator.SkillTranscribe}, 8000, true, func(c *config.Config) {
		c.Backends = append(c.Backends, config.Backend{Name: "chat", URL: c.Backends[0].URL, Models: []config.Model{
			{Name: "chat", Family: "llama", Skills: []string{"general"}, Context: 8000, Offer: true}}})
		c.Resources.AcceptPublicTasks = true
	}, idA)
	if err := a.P2P.Connect(ctx, b.P2P.Addrs()[0]); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return len(a.Agents()) == 3 })
	agent := func(family string) orchestrator.Agent {
		for _, x := range a.Agents() {
			if x.Family == family {
				return x
			}
		}
		t.Fatalf("agent %s fehlt", family)
		return orchestrator.Agent{}
	}
	task := func(class protocol.PrivacyClass, size int) orchestrator.Task {
		return orchestrator.Task{TaskID: "t", SubtaskID: "s", Role: protocol.RoleSpecialist, Skill: orchestrator.SkillTranscribe,
			Limits: protocol.Limits{PrivacyClass: class}, Audio: &protocol.Audio{Data: make([]byte, size)}}
	}
	for _, tc := range []struct {
		name  string
		agent string
		task  orchestrator.Task
		want  string
	}{
		{"öffentlich", "nemotron", task(protocol.PrivacyPublic, 10), "audio nur im vertrauten kreis"},
		{"textmodell", "llama", task(protocol.PrivacyTrusted, 10), "agent verarbeitet kein audio"},
		{"zu groß", "nemotron", task(protocol.PrivacyTrusted, protocol.MaxAudioBytes+1), "aufnahme leer oder zu groß"},
	} {
		_, err := a.P2P.Execute(ctx, agent(tc.agent), tc.task)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v", tc.name, err)
		}
	}
	if _, err := a.P2P.Execute(ctx, agent("nemotron"), task(protocol.PrivacyTrusted, 10)); err != nil {
		t.Fatalf("gültige aufnahme abgelehnt: %v", err)
	}
}
