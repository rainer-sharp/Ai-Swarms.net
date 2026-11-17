// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Rainer Sestig. Kommerzielle Lizenz: siehe COMMERCIAL.md.

package node

import (
	"context"
	"slices"
	"testing"

	"github.com/rainer-sharp/Ai-Swarms.net/internal/config"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/identity"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/orchestrator"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/protocol"
)

// TestTwoNetworks bildet spec/p2p-schicht.md § 9 nach: H ist im privaten
// Netz p-home und im offenen Netz, dort mit eigener Peer-ID. P (privat) und
// O (offen) sehen nur ihren Teil; trusted-Aufträge verlassen das private
// Netz nicht, public-Aufträge dürfen ins offene.
func TestTwoNetworks(t *testing.T) { overLoopback(t, testTwoNetworks) }

func testTwoNetworks(t *testing.T, listen string) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	psk := writePSK(t)
	yes := true
	idH, _ := identity.New()
	idP, _ := identity.New()
	idO, _ := identity.New()

	h := newTestNodeWith(t, ctx, idH, "qwen2.5-coder:7b", "qwen", []string{"code.python"}, 32000, true, func(c *config.Config) {
		c.Node.NetworkID, c.Node.NetworkKeyFile = "p-home", psk
		c.Node.Listen = []string{listen}
		c.Node.Networks = []config.Network{{NetworkID: "swarmainet", Listen: []string{listen}, AcceptPublicTasks: &yes}}
	})
	if len(h.Nets) != 2 || h.Nets[1].ID() == idH.ID {
		t.Fatalf("offenes netz ohne eigene identität: %d netze", len(h.Nets))
	}
	hPrivate, hOpen := h.Nets[0].Addrs()[0], h.Nets[1].Addrs()[0]

	p := newTestNodeWith(t, ctx, idP, "mistral-small", "mistral", []string{"general"}, 32000, false, func(c *config.Config) {
		c.Node.NetworkID, c.Node.NetworkKeyFile = "p-home", psk
		c.Node.StaticPeers = []string{hPrivate}
		c.Node.Listen = []string{listen}
	})
	o := newTestNodeWith(t, ctx, idO, "llama3.1:8b", "llama", []string{"general", "code.review"}, 128000, true, func(c *config.Config) {
		c.Resources.AcceptPublicTasks = true
		c.Node.StaticPeers = []string{hOpen}
		c.Node.Listen = []string{listen}
	})

	waitFor(t, func() bool { return len(p.Agents()) == 2 && len(o.Agents()) == 2 && len(h.Agents()) == 2 })

	// Trennung: Mit dem jeweils anderen Teil von H kommt keine Verbindung zustande.
	if err := p.P2P.Connect(ctx, hOpen); err == nil {
		t.Fatal("privater knoten erreicht das offene netz")
	}
	if err := o.P2P.Connect(ctx, hPrivate); err == nil {
		t.Fatal("offener knoten erreicht das private netz")
	}

	// O sieht H nur als nicht vertrauten Anbieter öffentlicher Aufträge.
	i := slices.IndexFunc(o.Agents(), func(a orchestrator.Agent) bool { return a.Family == "qwen" })
	if i < 0 || o.Agents()[i].Trusted || o.Agents()[i].PeerID != h.Nets[1].ID().String() {
		t.Fatalf("H aus sicht von O: %+v", o.Agents())
	}

	ask := func(n testNode, privacy protocol.PrivacyClass, skill string) *orchestrator.Answer {
		t.Helper()
		ans, err := n.Ask(ctx, orchestrator.Request{
			Messages: []protocol.ChatMessage{{Role: "user", Content: "Aufgabe"}},
			Pattern:  orchestrator.PatternSingle, Privacy: privacy, Skill: skill,
		})
		if err != nil {
			t.Fatal(err)
		}
		return ans
	}
	// P nutzt H im privaten Netz mit trusted.
	if ans := ask(p, protocol.PrivacyTrusted, "code.python"); ans.Steps[0].PeerID != idH.ID.String() || ans.Steps[0].Status != protocol.StatusOK {
		t.Fatalf("P→H trusted: %+v", ans.Steps)
	}
	// O bekommt H nur mit public.
	if ans := ask(o, protocol.PrivacyTrusted, "code.python"); ans.Steps[0].PeerID == h.Nets[1].ID().String() {
		t.Fatalf("trusted-auftrag von O ging an H: %+v", ans.Steps)
	}
	if ans := ask(o, protocol.PrivacyPublic, "code.python"); ans.Steps[0].PeerID != h.Nets[1].ID().String() || ans.Steps[0].Status != protocol.StatusOK {
		t.Fatalf("O→H public: %+v", ans.Steps)
	}
	// H selbst: trusted bleibt lokal oder im privaten Netz, public darf zu O.
	calls := o.calls.Load()
	for _, s := range ask(h, protocol.PrivacyTrusted, "code.review").Steps {
		if s.PeerID == idO.ID.String() {
			t.Fatalf("trusted-auftrag von H ging ins offene netz: %+v", s)
		}
	}
	if o.calls.Load() != calls {
		t.Fatal("O hat einen trusted-auftrag ausgeführt")
	}
	if ans := ask(h, protocol.PrivacyPublic, "code.review"); ans.Steps[0].PeerID != idO.ID.String() {
		t.Fatalf("H→O public: %+v", ans.Steps)
	}

	st := h.Status()
	if len(st.Networks) != 2 || st.Networks[1].NetworkID != "swarmainet" || st.Networks[1].Private || !st.Networks[0].Private {
		t.Fatalf("status: %+v", st.Networks)
	}
}
