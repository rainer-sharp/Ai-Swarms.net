// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Rainer Sestig und Mitwirkende.

package node

import (
	"context"
	"testing"

	"github.com/rainer-sharp/Ai-Swarms.net/internal/config"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/identity"
)

// TestOpenNetworkOutlivesStartNode: Das offene Netz beginnt mit einem
// Startknoten S und trägt sich danach selbst (spec/p2p-layer.md § 6.1):
// R ist ein erreichbarer Freiwilliger mit Relay und DHT, A und C sind
// hinter NAT (ohne Listen-Adresse) und kennen anfangs nur S. Nach dem Ende
// von S erreicht A den Knoten C weiter über R, und ein neuer Knoten D kommt
// allein mit einer Einladung von A ins Netz und findet C.
func TestOpenNetworkOutlivesStartNode(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ids := make([]*identity.Identity, 5)
	for i := range ids {
		ids[i], _ = identity.New()
	}
	idS, idR, idA, idC, idD := ids[0], ids[1], ids[2], ids[3], ids[4]

	s := newTestNodeWith(t, ctx, idS, "-", "-", nil, 0, false, func(c *config.Config) { c.Node.Helper = true })
	start := s.P2P.Addrs()[0]
	newTestNodeWith(t, ctx, idR, "-", "-", nil, 0, false, func(c *config.Config) {
		c.Node.RelayService, c.Node.DHTServer = "true", "true" // erreichbarer Freiwilliger
		c.Node.Bootstrap = []string{start}
	})
	behindNAT := func(c *config.Config) {
		c.Node.Listen = nil
		c.Node.Bootstrap = []string{start}
	}
	a := newTestNodeWith(t, ctx, idA, "-", "-", nil, 0, false, behindNAT)
	c := newTestNodeWith(t, ctx, idC, "-", "-", nil, 0, false, behindNAT)

	// A und C reservieren bei S und R, die sie über S kennenlernen.
	waitFor(t, func() bool { return len(a.P2P.Reservations()) == 2 && len(c.P2P.Reservations()) == 2 })
	if _, err := a.P2P.Ping(ctx, idC.ID.String()); err != nil {
		t.Fatal(err)
	}

	_ = s.Close()
	waitFor(t, func() bool { return !connectedTo(a, idS) && !connectedTo(c, idS) })

	// A erreicht C weiter, über R.
	rep, err := a.P2P.Ping(ctx, idC.ID.String())
	if err != nil || !rep.OK {
		t.Fatalf("A→C nach dem ende des startknotens: %+v %v", rep, err)
	}

	// D kennt nur eine Einladung von A (dessen Adressen über R).
	invite := a.P2P.Addrs()
	d := newTestNodeWith(t, ctx, idD, "-", "-", nil, 0, false, func(c *config.Config) {
		c.Node.Listen = nil
		c.Node.Bootstrap = invite
	})
	waitFor(t, func() bool { return connectedTo(d, idA) && connectedTo(d, idC) })
}

func connectedTo(n testNode, id *identity.Identity) bool {
	for _, p := range n.P2P.Peers() {
		if p.PeerID == id.ID.String() {
			return true
		}
	}
	return false
}
