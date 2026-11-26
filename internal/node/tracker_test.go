// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Rainer Sestig und Mitwirkende.

package node

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rainer-sharp/Ai-Swarms.net/internal/config"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/identity"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/invite"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/tracker"
)

// TestTrackerPrivateNetwork: A und C kennen sich nicht und haben weder mDNS
// noch statische Peers; sie finden sich über den Tracker. Der Tracker sieht
// dabei weder die Netz-Kennung noch Peer-IDs (spec/p2p-layer.md § 2.3).
func TestTrackerPrivateNetwork(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srv := httptest.NewServer(tracker.NewServer())
	defer srv.Close()
	psk := writePSK(t)
	idA, _ := identity.New()
	idC, _ := identity.New()
	viaTracker := func(c *config.Config) {
		c.Node.NetworkID, c.Node.NetworkKeyFile = "p-home", psk
		c.Node.Trackers = []string{srv.URL}
	}

	a := newTestNodeWith(t, ctx, idA, "mistral-small", "mistral", []string{"general"}, 32000, false, viaTracker)
	key, _ := invite.ReadPSK(psk)
	sealer, _ := tracker.NewSealer(key)
	waitFor(t, func() bool {
		recs, _, _ := tracker.Fetch(ctx, srv.URL, sealer.Net())
		return len(recs) == 1
	})
	if recs, _, _ := tracker.Fetch(ctx, srv.URL, "p-home"); len(recs) != 0 {
		t.Fatal("eintrag unter der klartext-kennung")
	}
	recs, _, _ := tracker.Fetch(ctx, srv.URL, sealer.Net())
	if s := string(recs[0]); strings.Contains(s, idA.ID.String()) || strings.Contains(s, "127.0.0.1") {
		t.Fatalf("klartext beim tracker: %s", s)
	}

	c := newTestNodeWith(t, ctx, idC, "qwen2.5-coder:7b", "qwen", []string{"code.python"}, 32000, true, viaTracker)
	waitFor(t, func() bool { return len(a.Agents()) == 2 && len(c.P2P.Peers()) == 1 })
}
