// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Rainer Sestig. Kommerzielle Lizenz: siehe COMMERCIAL.md.

package node

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	lp2pproto "github.com/libp2p/go-libp2p/core/protocol"

	"github.com/rainer-sharp/Ai-Swarms.net/internal/config"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/identity"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/orchestrator"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/p2p"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/protocol"
)

func writePSK(t *testing.T) string {
	t.Helper()
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	path := filepath.Join(t.TempDir(), "swarm.key")
	if err := os.WriteFile(path, []byte("/key/swarm/psk/1.0.0/\n/base16/\n"+hex.EncodeToString(key)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestBridgeNodeConnectsSites bildet spec/p2p-layer.md § 6.1 nach: A und C
// lauschen nicht (hinter NAT), erreichen nur den Brückenknoten B und finden
// sich über Peer-Austausch; der Auftrag läuft über B als Relay. C kommt erst
// hinzu, als A schon verbunden ist, und holt sich A beim Peer-Austausch.
func TestBridgeNodeConnectsSites(t *testing.T) { overLoopback(t, testBridge) }

func testBridge(t *testing.T, listen string) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	psk := writePSK(t)
	idA, _ := identity.New()
	idB, _ := identity.New()
	idC, _ := identity.New()

	b := newTestNodeWith(t, ctx, idB, "-", "-", nil, 0, false, func(c *config.Config) {
		c.Node.NetworkKeyFile = psk
		c.Node.Helper = true
		c.Node.Listen = []string{listen}
	})
	bridge := b.P2P.Addrs()[0]
	behindNAT := func(c *config.Config) {
		c.Node.NetworkKeyFile = psk
		c.Node.Listen = nil
		c.Node.StaticPeers = []string{bridge}
	}
	a := newTestNodeWith(t, ctx, idA, "mistral-small", "mistral", []string{"general"}, 32000, false, behindNAT)
	waitFor(t, func() bool { return len(b.P2P.Peers()) == 1 && len(a.P2P.Reservations()) == 1 })
	c := newTestNodeWith(t, ctx, idC, "qwen2.5-coder:7b", "qwen", []string{"code.python"}, 32000, true, behindNAT)

	waitFor(t, func() bool { return len(a.Agents()) == 2 })
	peers := a.P2P.Peers()
	peerC := slices.IndexFunc(peers, func(p p2p.PeerInfo) bool { return p.PeerID == idC.ID.String() })
	if peerC < 0 || peers[peerC].Via != "relay" {
		t.Fatalf("A–C nicht über relay verbunden: %+v", peers)
	}
	if st := b.Status(); !st.Relay || len(b.Local.Agents()) != 0 {
		t.Fatalf("helfer: %+v", st)
	}

	ans, err := a.Ask(ctx, orchestrator.Request{
		Messages: []protocol.ChatMessage{{Role: "user", Content: "Schreibe eine Python-Funktion für Fakultät"}},
		Pattern:  orchestrator.PatternSingle,
	})
	if err != nil {
		t.Fatal(err)
	}
	if ans.Steps[0].PeerID != idC.ID.String() || ans.Steps[0].Status != protocol.StatusOK || c.calls.Load() != 1 {
		t.Fatalf("auftrag nicht über relay bei C: %+v", ans.Steps)
	}
	if !strings.Contains(ans.Content, "qwen2.5-coder") {
		t.Fatalf("antwort %q", ans.Content)
	}

	// Diagnose: C über Relay erreichbar, ein unbekannter Peer nicht.
	rep, err := a.P2P.Ping(ctx, idC.ID.String())
	if err != nil || !rep.OK || rep.Via != "relay" || len(rep.Agents) != 1 || rep.RTTms <= 0 {
		t.Fatalf("ping C: %+v %v", rep, err)
	}
	idX, _ := identity.New()
	rep, err = a.P2P.Ping(ctx, idX.ID.String())
	if err != nil || rep.OK || len(rep.Hints) == 0 {
		t.Fatalf("ping unbekannt: %+v %v", rep, err)
	}
	if _, err := a.P2P.Ping(ctx, idA.ID.String()); err == nil {
		t.Fatal("ping an sich selbst nicht abgewiesen")
	}
}

// TestPeerCacheAfterRestart: Nach einem Neustart findet ein Knoten seinen
// Peer über den Peer-Cache, ohne mDNS und ohne static_peers.
func TestPeerCacheAfterRestart(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	idA, _ := identity.New()
	idB, _ := identity.New()
	dir := t.TempDir()
	withDir := func(c *config.Config) { c.Node.DataDir = dir }

	b := newTestNode(t, ctx, idB, "qwen2.5-coder:7b", "qwen", []string{"code.python"}, 32000, true, idA)
	a := newTestNodeWith(t, ctx, idA, "mistral-small", "mistral", []string{"general"}, 32000, false, withDir, idB)
	if err := a.P2P.Connect(ctx, b.P2P.Addrs()[0]); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return len(a.Agents()) == 2 })
	_ = a.Close()
	if _, err := os.Stat(filepath.Join(dir, "peers-swarmainet.json")); err != nil {
		t.Fatal(err)
	}

	a2 := newTestNodeWith(t, ctx, idA, "mistral-small", "mistral", []string{"general"}, 32000, false, withDir, idB)
	waitFor(t, func() bool { return len(a2.Agents()) == 2 })
}

// TestRelayUpgradedToDirect bildet einen Knoten ohne Listen-Adresse nach: A lauscht nicht und ist für C
// nur über das Relay erreichbar. C wählt A an, die Verbindung ist begrenzt,
// und A ersetzt sie durch eine direkte zu C, das eine eigene Adresse hat.
func TestRelayUpgradedToDirect(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	psk := writePSK(t)
	idA, _ := identity.New()
	idB, _ := identity.New()
	idC, _ := identity.New()
	b := newTestNodeWith(t, ctx, idB, "-", "-", nil, 0, false, func(c *config.Config) {
		c.Node.NetworkKeyFile = psk
		c.Node.Helper = true
	})
	static := []string{b.P2P.Addrs()[0]}
	a := newTestNodeWith(t, ctx, idA, "mistral-small", "mistral", []string{"general"}, 32000, false, func(c *config.Config) {
		c.Node.NetworkKeyFile = psk
		c.Node.Listen = nil
		c.Node.StaticPeers = static
	})
	waitFor(t, func() bool { return len(a.P2P.Reservations()) == 1 })
	newTestNodeWith(t, ctx, idC, "qwen2.5-coder:7b", "qwen", []string{"general"}, 32000, true, func(c *config.Config) {
		c.Node.NetworkKeyFile = psk
		c.Node.StaticPeers = static
	})
	waitFor(t, func() bool {
		peers := a.P2P.Peers()
		i := slices.IndexFunc(peers, func(p p2p.PeerInfo) bool { return p.PeerID == idC.ID.String() })
		return i >= 0 && peers[i].Via == "direct"
	})
}

// TestPEXNamesSelfOnlyInPrivateSwarm: Identify über ein Relay im Internet
// verliert private Adressen, deshalb nennt sich ein Mitglied im privaten
// Schwarm beim Peer-Austausch selbst. Im offenen Netz bleibt es dabei, dass
// nur andere genannt werden.
func TestPEXNamesSelfOnlyInPrivateSwarm(t *testing.T) {
	for _, c := range []struct {
		name string
		psk  string
		want bool
	}{{"privat", writePSK(t), true}, {"offen", "", false}} {
		t.Run(c.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			idA, _ := identity.New()
			idB, _ := identity.New()
			mod := func(cfg *config.Config) { cfg.Node.NetworkKeyFile = c.psk }
			a := newTestNodeWith(t, ctx, idA, "mistral-small", "mistral", []string{"general"}, 32000, false, mod)
			b := newTestNodeWith(t, ctx, idB, "qwen2.5-coder:7b", "qwen", []string{"general"}, 32000, true, mod)
			if err := a.P2P.Connect(ctx, b.P2P.Addrs()[0]); err != nil {
				t.Fatal(err)
			}
			s, err := a.P2P.Host().NewStream(ctx, b.P2P.Host().ID(), lp2pproto.ID(protocol.ProtoPEX))
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			var msg struct {
				Peers []struct {
					PeerID string   `json:"peer_id"`
					Addrs  []string `json:"addrs"`
				} `json:"peers"`
			}
			if err := json.NewDecoder(s).Decode(&msg); err != nil {
				t.Fatal(err)
			}
			got := slices.ContainsFunc(msg.Peers, func(p struct {
				PeerID string   `json:"peer_id"`
				Addrs  []string `json:"addrs"`
			}) bool {
				return p.PeerID == idB.ID.String() && len(p.Addrs) > 0
			})
			if got != c.want {
				t.Fatalf("nennt sich selbst: %v, erwartet %v: %+v", got, c.want, msg.Peers)
			}
		})
	}
}
