// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Rainer Sestig. Kommerzielle Lizenz: siehe COMMERCIAL.md.

package p2p

import (
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/p2p/net/swarm"

	"github.com/rainer-sharp/Ai-Swarms.net/internal/identity"
)

// redialPair startet zwei Knoten auf localhost, verbindet sie und trennt sie
// wieder; lo hat die kleinere Peer-ID.
func redialPair(t *testing.T) (lo, hi *Node) {
	t.Helper()
	ctx := t.Context()
	var nodes [2]*Node
	for i := range nodes {
		id, _ := identity.New()
		n, err := New(ctx, Options{Identity: id, Listen: []string{"/ip4/127.0.0.1/tcp/0"}, NetworkID: "swarmainet",
			AnnouncePrivate: true, DHTServer: "false", RelayService: "false", Provide: true})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { n.Close() })
		nodes[i] = n
	}
	lo, hi = nodes[0], nodes[1]
	if hi.ID() < lo.ID() {
		lo, hi = hi, lo
	}
	if err := lo.host.Connect(ctx, hi.host.Peerstore().PeerInfo(hi.ID())); err != nil {
		t.Fatal(err)
	}
	// Erst trennen, wenn beide über Identify die Adressen des anderen kennen.
	waitUntil(t, 5*time.Second, func() bool {
		return len(hi.host.Peerstore().Addrs(lo.ID())) > 0 && len(lo.host.Peerstore().Addrs(hi.ID())) > 0
	})
	if err := lo.host.Network().ClosePeer(hi.ID()); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, time.Second, func() bool { return !lo.connected(hi.ID()) && !hi.connected(lo.ID()) })
	return lo, hi
}

func waitUntil(t *testing.T, d time.Duration, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatal("bedingung nicht erreicht")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestRedialLostYields: Nach einem Abriss wählt nur der Peer mit der kleineren
// ID sofort neu; der andere wartet, damit kein Simultaneous Open entsteht,
// und wählt selbst, wenn der erste nicht durchkommt.
func TestRedialLostYields(t *testing.T) {
	lo, hi := redialPair(t)
	go hi.redialLost(t.Context(), lo.ID())
	time.Sleep(time.Second)
	if hi.connected(lo.ID()) {
		t.Fatal("größere id hat nicht gewartet")
	}
	// lo wählt nicht: hi muss es nach der Wartezeit selbst tun.
	waitUntil(t, redialYield+2*time.Second, func() bool { return hi.connected(lo.ID()) })

	lo, hi = redialPair(t)
	start := time.Now()
	lo.redialLost(t.Context(), hi.ID())
	if !lo.connected(hi.ID()) || time.Since(start) > time.Second {
		t.Fatalf("kleinere id hat nicht sofort gewählt (%v)", time.Since(start))
	}
}

// TestTaskStreamClearsBackoff: Ein Auftrag, der auf eine Wählsperre trifft,
// hebt sie auf und wählt neu.
func TestTaskStreamClearsBackoff(t *testing.T) {
	lo, hi := redialPair(t)
	sw := lo.host.Network().(*swarm.Swarm)
	for _, a := range lo.host.Peerstore().Addrs(hi.ID()) {
		sw.Backoff().AddBackoff(hi.ID(), a)
	}
	if _, err := lo.host.Network().DialPeer(t.Context(), hi.ID()); err == nil {
		t.Fatal("sperre wirkt nicht; test prüft nichts")
	}
	s, err := lo.newTaskStream(t.Context(), hi.ID())
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Reset()
}
