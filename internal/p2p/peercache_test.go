// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Rainer Sestig und Mitwirkende.

package p2p

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
)

func newPeerID(t *testing.T) peer.ID {
	t.Helper()
	_, pub, err := crypto.GenerateEd25519Key(nil)
	if err != nil {
		t.Fatal(err)
	}
	id, err := peer.IDFromPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestPeerCachePrune(t *testing.T) {
	now := time.Now()
	c := loadPeerCache(filepath.Join(t.TempDir(), "peers.json"))
	add := func(addr string, age time.Duration) peer.ID {
		id := newPeerID(t)
		c.entries[id] = cacheEntry{PeerID: id.String(), Addrs: []string{addr}, LastOK: now.Add(-age)}
		return id
	}
	old := add("/ip4/198.51.100.1/tcp/4711", 31*24*time.Hour)
	// zwölf Peers im selben /16, einer davon vertraut und am ältesten
	trusted := add("/ip4/192.168.1.200/tcp/4711", 20*24*time.Hour)
	for i := range 11 {
		add(fmt.Sprintf("/ip4/192.168.%d.%d/tcp/4711", i%3, i+1), time.Duration(i)*time.Hour)
	}
	other := add("/ip4/10.1.2.3/tcp/4711", time.Hour)

	c.prune(now, func(p peer.ID) bool { return p == trusted })

	if _, ok := c.entries[old]; ok {
		t.Error("eintrag älter als 30 tage nicht entfernt")
	}
	if _, ok := c.entries[trusted]; !ok {
		t.Error("vertrauter peer entfernt")
	}
	if _, ok := c.entries[other]; !ok {
		t.Error("peer aus anderem netz entfernt")
	}
	inBucket := 0
	for _, e := range c.entries {
		if bucket(e) == "192.168.0.0" {
			inBucket++
		}
	}
	if inBucket != cacheMaxPerBucket {
		t.Errorf("%d einträge im selben /16, erwartet %d", inBucket, cacheMaxPerBucket)
	}

	if err := c.save(); err != nil {
		t.Fatal(err)
	}
	if got := loadPeerCache(c.path); len(got.entries) != len(c.entries) {
		t.Errorf("nach laden %d statt %d einträge", len(got.entries), len(c.entries))
	}
}

func TestBucketCircuit(t *testing.T) {
	relay := newPeerID(t)
	e := cacheEntry{Addrs: []string{"/ip4/10.0.0.1/tcp/4711/p2p/" + relay.String() + "/p2p-circuit"}}
	if got := bucket(e); got != "relay:"+relay.String() {
		t.Errorf("bucket %q", got)
	}
}
