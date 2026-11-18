// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Rainer Sestig. Kommerzielle Lizenz: siehe COMMERCIAL.md.

package node

import (
	"context"
	"testing"

	"github.com/rainer-sharp/Ai-Swarms.net/internal/config"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/identity"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/orchestrator"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/protocol"
)

// TestDHTDiscovery: Im offenen Netz kennt A nur den DHT-Server B. Den
// Anbieter C findet A über dessen Fähigkeitsverweis in der DHT
// (spec/p2p-layer.md § 5); Peer-Austausch ist abgeschaltet.
func TestDHTDiscovery(t *testing.T) { overLoopback(t, testDHTDiscovery) }

func testDHTDiscovery(t *testing.T, listen string) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	idA, _ := identity.New()
	idB, _ := identity.New()
	idC, _ := identity.New()

	b := newTestNodeWith(t, ctx, idB, "-", "-", nil, 0, false, func(c *config.Config) {
		c.Node.Helper = true
		c.Node.Listen = []string{listen}
	})
	if b.P2P.DHTInfo().Mode != "server" {
		t.Fatalf("helfer nicht dht-server: %+v", b.P2P.DHTInfo())
	}
	viaB := func(c *config.Config) {
		c.Node.StaticPeers = []string{b.P2P.Addrs()[0]}
		c.Node.PEX = false
		c.Node.Listen = []string{listen}
	}
	a := newTestNodeWith(t, ctx, idA, "mistral-small", "mistral", []string{"general"}, 32000, false, viaB)
	c := newTestNodeWith(t, ctx, idC, "qwen2.5-coder:7b", "qwen", []string{"code.python"}, 32000, true, func(c *config.Config) {
		viaB(c)
		c.Resources.AcceptPublicTasks = true
	})

	waitFor(t, func() bool { return len(c.P2P.DHTInfo().Provided) > 0 })
	if len(a.Agents()) != 1 {
		t.Fatalf("A kennt C schon vor der suche: %+v", a.Agents())
	}
	ans, err := a.Ask(ctx, orchestrator.Request{
		Messages: []protocol.ChatMessage{{Role: "user", Content: "Schreibe eine Python-Funktion für Fakultät"}},
		Pattern:  orchestrator.PatternSingle,
		Privacy:  protocol.PrivacyPublic,
	})
	if err != nil {
		t.Fatal(err)
	}
	if ans.Steps[0].PeerID != idC.ID.String() || c.calls.Load() != 1 {
		t.Fatalf("anbieter nicht über dht gefunden: %+v", ans.Steps)
	}
}

// TestOpenNetworkHidesPrivateAddrs: Ohne announce_private veröffentlicht ein
// Knoten keine Loopback-Adressen und im offenen Netz keine internen (§ 3);
// LAN-Adressen im privaten Netz prüft TestShareableAddrs.
func TestOpenNetworkHidesPrivateAddrs(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	psk := writePSK(t)
	idO, _ := identity.New()
	idP, _ := identity.New()
	o := newTestNodeWith(t, ctx, idO, "-", "-", nil, 0, false, func(c *config.Config) {
		c.Node.AnnouncePrivate = false
		c.Node.Announce = []string{"/dns4/swarm.example.de/tcp/4711"}
	})
	p := newTestNodeWith(t, ctx, idP, "-", "-", nil, 0, false, func(c *config.Config) {
		c.Node.AnnouncePrivate = false
		c.Node.NetworkKeyFile = psk
	})
	// Nur die angekündigte Adresse bleibt (Portfreigabe mit DNS-Namen).
	if a := o.P2P.Addrs(); len(a) != 1 || a[0] != "/dns4/swarm.example.de/tcp/4711/p2p/"+idO.ID.String() {
		t.Fatalf("offenes netz veröffentlicht %v", a)
	}
	if a := p.P2P.Addrs(); len(a) != 0 {
		t.Fatalf("privates netz veröffentlicht loopback %v", a)
	}
	if r := o.Status().Networks[0].Reach; r == "" {
		t.Fatal("offenes netz ohne erreichbarkeitsangabe im status")
	}
	if r := p.Status().Networks[0].Reach; r != "" {
		t.Fatalf("privates netz mit erreichbarkeitsangabe %q", r)
	}
}
