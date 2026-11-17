// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Rainer Sestig. Kommerzielle Lizenz: siehe COMMERCIAL.md.

package p2p

import (
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
)

// Ratenlimits für nicht vertraute Peers im offenen Netz (spec/p2p-schicht.md § 8).
const (
	limitWindow    = time.Minute
	taskLimitPeer  = 10
	taskLimitTotal = 60
	capLimitPeer   = 6
	pexLimitPeer   = 6
)

// rateLimiter zählt Anfragen in festen Zeitfenstern, je Peer und gesamt
// (total 0 = ohne Gesamtgrenze).
type rateLimiter struct {
	perPeer, total int

	mu    sync.Mutex
	start time.Time
	count int
	peers map[peer.ID]int
}

func newRateLimiter(perPeer, total int) *rateLimiter {
	return &rateLimiter{perPeer: perPeer, total: total, peers: make(map[peer.ID]int)}
}

func (r *rateLimiter) allow(p peer.ID, now time.Time) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if now.Sub(r.start) >= limitWindow {
		r.start, r.count = now, 0
		clear(r.peers)
	}
	if r.peers[p] >= r.perPeer || r.total > 0 && r.count >= r.total {
		return false
	}
	r.peers[p]++
	r.count++
	return true
}

// limited: unterliegt p den Ratenlimits? Nur fremde Peers im offenen Netz.
func (n *Node) limited(p peer.ID) bool { return !n.private && !n.Trusted(p) }
