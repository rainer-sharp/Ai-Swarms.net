// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Rainer Sestig und Mitwirkende.

package p2p

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
)

// Peer-Cache nach spec/p2p-layer.md § 2.1: zuletzt erfolgreich verbundene
// Peers, ab dem zweiten Start die Hauptquelle für den Netzeintritt.
const (
	cacheMaxEntries   = 128
	cacheMaxAge       = 30 * 24 * time.Hour
	cacheMaxPerBucket = 8
	cacheStartDials   = 16
	cacheAddrTTL      = 24 * time.Hour
)

type cacheEntry struct {
	PeerID string    `json:"peer_id"`
	Addrs  []string  `json:"addrs"`
	LastOK time.Time `json:"last_ok"`
	Via    string    `json:"via"` // direct | relay
}

type peerCache struct {
	path    string
	entries map[peer.ID]cacheEntry
	dirty   bool
}

func loadPeerCache(path string) *peerCache {
	c := &peerCache{path: path, entries: make(map[peer.ID]cacheEntry)}
	if path == "" {
		return c
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			slog.Warn("peer-cache nicht lesbar", "datei", path, "err", err)
		}
		return c
	}
	var list []cacheEntry
	if err := json.Unmarshal(data, &list); err != nil {
		slog.Warn("peer-cache ungültig, wird neu angelegt", "datei", path, "err", err)
		return c
	}
	for _, e := range list {
		if id, err := peer.Decode(e.PeerID); err == nil {
			c.entries[id] = e
		}
	}
	return c
}

// bucket ordnet einen Eintrag für die Vielfaltsregel zu: IPv4-/16, IPv6-/32
// oder bei Circuit-Adressen das Relay.
func bucket(e cacheEntry) string {
	for _, s := range e.Addrs {
		a, err := ma.NewMultiaddr(s)
		if err != nil {
			continue
		}
		if isCircuit(a) {
			first, _ := ma.SplitFunc(a, func(c ma.Component) bool { return c.Protocol().Code == ma.P_CIRCUIT })
			if id, err := peer.IDFromP2PAddr(first); err == nil {
				return "relay:" + id.String()
			}
		}
		ip, err := manet.ToIP(a)
		if err != nil {
			continue
		}
		if v4 := ip.To4(); v4 != nil {
			return v4.Mask(net.CIDRMask(16, 32)).String()
		}
		return ip.Mask(net.CIDRMask(32, 128)).String()
	}
	return ""
}

func isCircuit(a ma.Multiaddr) bool {
	for _, c := range a {
		if c.Protocol().Code == ma.P_CIRCUIT {
			return true
		}
	}
	return false
}

// prune entfernt alte Einträge und hält Größe und Vielfalt ein. Bevorzugt
// bleiben Peers, für die keep gilt (vertraute und statische), danach die
// zuletzt erfolgreichen.
func (c *peerCache) prune(now time.Time, keep func(peer.ID) bool) {
	type item struct {
		id peer.ID
		e  cacheEntry
	}
	var items []item
	for id, e := range c.entries {
		if now.Sub(e.LastOK) > cacheMaxAge {
			delete(c.entries, id)
			c.dirty = true
			continue
		}
		items = append(items, item{id, e})
	}
	slices.SortFunc(items, func(a, b item) int {
		if ka, kb := keep(a.id), keep(b.id); ka != kb {
			if ka {
				return -1
			}
			return 1
		}
		return b.e.LastOK.Compare(a.e.LastOK)
	})
	perBucket := map[string]int{}
	kept := 0
	for _, it := range items {
		b := bucket(it.e)
		if !keep(it.id) && (kept >= cacheMaxEntries || b != "" && perBucket[b] >= cacheMaxPerBucket) {
			delete(c.entries, it.id)
			c.dirty = true
			continue
		}
		perBucket[b]++
		kept++
	}
}

func (c *peerCache) save() error {
	if c.path == "" || !c.dirty {
		return nil
	}
	list := make([]cacheEntry, 0, len(c.entries))
	for _, e := range c.entries {
		list = append(list, e)
	}
	slices.SortFunc(list, func(a, b cacheEntry) int { return cmp.Compare(a.PeerID, b.PeerID) })
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	tmp := c.path + ".tmp"
	if err := os.MkdirAll(filepath.Dir(c.path), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, c.path); err != nil {
		return err
	}
	c.dirty = false
	return nil
}

// --- Anbindung an den Knoten ---

// rememberPeer trägt einen verbundenen Peer mit seinen aktuellen Adressen ein.
func (n *Node) rememberPeer(p peer.ID) {
	if !n.admitted(p) {
		return // Fremde an einem Mitgliedsnetz merken wir uns nicht
	}
	var addrs []string
	for _, a := range n.host.Peerstore().Addrs(p) {
		addrs = append(addrs, a.String())
	}
	if len(addrs) == 0 {
		return // nur anwählbare Peers lohnen sich
	}
	via := "relay"
	if n.host.Network().Connectedness(p) == network.Connected {
		via = "direct"
	}
	n.mu.Lock()
	n.cache.entries[p] = cacheEntry{PeerID: p.String(), Addrs: addrs, LastOK: time.Now().UTC(), Via: via}
	n.cache.dirty = true
	n.mu.Unlock()
}

// keepInCache: vertraute und statische Peers bleiben bevorzugt im Cache.
func (n *Node) keepInCache(p peer.ID) bool {
	return n.trusted[p] || n.static[p]
}

// saveCache aktualisiert verbundene Peers, räumt auf und schreibt die Datei.
func (n *Node) saveCache() {
	for _, p := range n.host.Network().Peers() {
		n.rememberPeer(p)
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	n.cache.prune(time.Now(), n.keepInCache)
	if err := n.cache.save(); err != nil {
		slog.Warn("peer-cache nicht gespeichert", "err", err)
	}
}

// dialCache wählt beim Start bis zu cacheStartDials Peers aus dem Cache an,
// vertraute zuerst, sonst die zuletzt erfolgreichen.
func (n *Node) dialCache(ctx context.Context) {
	n.mu.Lock()
	list := make([]cacheEntry, 0, len(n.cache.entries))
	for _, e := range n.cache.entries {
		list = append(list, e)
	}
	n.mu.Unlock()
	slices.SortFunc(list, func(a, b cacheEntry) int {
		ia, _ := peer.Decode(a.PeerID)
		ib, _ := peer.Decode(b.PeerID)
		if ta, tb := n.keepInCache(ia), n.keepInCache(ib); ta != tb {
			if ta {
				return -1
			}
			return 1
		}
		return b.LastOK.Compare(a.LastOK)
	})
	for i, e := range list {
		id, err := peer.Decode(e.PeerID)
		if err != nil || id == n.host.ID() {
			continue
		}
		var addrs []ma.Multiaddr
		for _, s := range e.Addrs {
			if a, err := ma.NewMultiaddr(s); err == nil {
				addrs = append(addrs, a)
			}
		}
		n.host.Peerstore().AddAddrs(id, addrs, cacheAddrTTL)
		n.remember(id)
		if i < cacheStartDials {
			go n.dialKnown(ctx, id)
		}
	}
}
