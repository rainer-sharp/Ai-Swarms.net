// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Rainer Sestig. Kommerzielle Lizenz: siehe COMMERCIAL.md.

package p2p

import (
	"context"
	"log/slog"
	"maps"
	"math/rand/v2"
	"slices"
	"strconv"
	"time"

	"github.com/ipfs/go-cid"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/peer"
	lp2pproto "github.com/libp2p/go-libp2p/core/protocol"
	mh "github.com/multiformats/go-multihash"

	"github.com/rainer-sharp/Ai-Swarms.net/internal/protocol"
)

// DHT nach spec/p2p-layer.md § 4 und § 5.
const (
	skillShards     = 4
	anyTag          = "*" // Sammelschlüssel „bietet überhaupt etwas an“
	provideInterval = 12 * time.Hour
	findMaxNew      = 8
	findTimeout     = 5 * time.Second
	ignoreFor       = time.Hour
	trustedLookup   = 5 * time.Minute
	minCandidates   = 2 // darunter sucht der Koordinator über die DHT
)

// dhtPrefix trennt den Schwarm von anderen libp2p-Netzen und private Netze
// untereinander: /swarmainet/kad/1.0.0 bzw. /swarmainet/<network_id>/kad/1.0.0.
func dhtPrefix(private bool, networkID string) lp2pproto.ID {
	if private {
		return lp2pproto.ID("/swarmainet/" + networkID)
	}
	return "/swarmainet"
}

// skillKey ist der DHT-Schlüssel eines Fähigkeitsverweises (§ 5.1).
func skillKey(networkID, tag string, shard int) cid.Cid {
	h, _ := mh.Sum([]byte("swarmainet/skill/v1\n"+networkID+"\n"+tag+"\n"+strconv.Itoa(shard)), mh.SHA2_256, -1)
	return cid.NewCidV1(cid.Raw, h)
}

// dhtMode: auto wird im offenen Netz zur Erreichbarkeitsprüfung von libp2p;
// im privaten Netz ist ein Knoten Server, wenn Mitglieder ihn anwählen können
// (eigene Listen-Adressen) oder er Relay ist.
func (n *Node) dhtMode() dht.ModeOpt {
	switch n.opts.DHTServer {
	case "true":
		return dht.ModeServer
	case "false":
		return dht.ModeClient
	}
	if n.private {
		if len(n.opts.Listen) > 0 || n.opts.RelayService == RelayOn {
			return dht.ModeServer
		}
		return dht.ModeClient
	}
	return dht.ModeAuto
}

func (n *Node) startDHT(ctx context.Context) error {
	opts := []dht.Option{
		dht.Mode(n.dhtMode()),
		dht.ProtocolPrefix(dhtPrefix(n.private, n.opts.NetworkID)),
		dht.DisableValues(), // keine frei beschreibbaren Werte (§ 4)
	}
	if !n.private {
		// Gegen Eclipse: höchstens 3 Peers je IP-Gruppe (IPv4-/16, IPv6-ASN)
		// und Bucket, 20 je Gruppe in der ganzen Tabelle (§ 8).
		opts = append(opts, dht.RoutingTablePeerDiversityFilter(dht.NewRTPeerDiversityFilter(n.host, 3, 20)))
	}
	d, err := dht.New(n.host, opts...)
	if err != nil {
		return err
	}
	n.dht = d
	n.shards = make(map[string]int)
	n.ignore = make(map[string]map[peer.ID]time.Time)
	go func() { _ = d.Bootstrap(ctx) }()
	return nil
}

// supportsDHT: ist p DHT-Server dieses Netzes? Bekannt erst nach Identify.
func (n *Node) supportsDHT(p peer.ID) bool {
	proto := dhtPrefix(n.private, n.opts.NetworkID) + "/kad/1.0.0"
	ps, _ := n.host.Peerstore().SupportsProtocols(p, proto)
	return len(ps) > 0
}

// tags sind die Fähigkeiten und Modalitäten der angebotenen Agenten.
func (n *Node) tags() []string {
	if !n.opts.Provide || n.opts.Provider == nil {
		return nil
	}
	set := map[string]bool{}
	for _, a := range n.opts.Provider.Offer() {
		for _, s := range a.Skills {
			set[s] = true
		}
		for _, m := range a.Modalities {
			set["modality."+m] = true
		}
	}
	if len(set) == 0 {
		return nil
	}
	set[anyTag] = true
	return slices.Sorted(maps.Keys(set))
}

// provide veröffentlicht die Fähigkeitsverweise neu, wenn sich die Tags
// geändert haben oder provideInterval vergangen ist (§ 5.2).
func (n *Node) provide(ctx context.Context) {
	if n.dht == nil {
		return
	}
	tags := n.tags()
	n.dhtMu.Lock()
	due := !slices.Equal(tags, n.provided) || time.Now().After(n.provideNext)
	if !due || len(tags) == 0 || n.providing {
		n.dhtMu.Unlock()
		return
	}
	n.providing = true
	for _, t := range tags {
		if _, ok := n.shards[t]; !ok {
			n.shards[t] = rand.IntN(skillShards) // je Tag ein fester Shard
		}
	}
	shards := maps.Clone(n.shards)
	n.dhtMu.Unlock()

	ok := true
	for _, t := range tags {
		cctx, cancel := context.WithTimeout(ctx, time.Minute)
		if err := n.dht.Provide(cctx, skillKey(n.opts.NetworkID, t, shards[t]), true); err != nil {
			slog.Debug("fähigkeitsverweis nicht veröffentlicht", "tag", t, "err", err)
			ok = false
		}
		cancel()
	}
	n.dhtMu.Lock()
	defer n.dhtMu.Unlock()
	n.providing = false
	if ok {
		n.provided, n.provideNext, n.provideFails = tags, time.Now().Add(provideInterval), 0
		slog.Info("fähigkeitsverweise veröffentlicht", "tags", tags)
		return
	}
	// Meist ist die Routing-Tabelle kurz nach dem Start noch leer.
	retry := min(5*time.Second<<min(n.provideFails, 4), refreshInterval)
	n.provideFails++
	time.AfterFunc(retry, func() {
		if ctx.Err() == nil {
			n.provide(ctx)
		}
	})
}

// Discover sucht im offenen Netz Anbieter einer Fähigkeit, wenn weniger als
// minCandidates verbunden sind, verbindet sich mit bis zu findMaxNew neuen und
// holt deren Anzeigen. Im privaten Netz sind alle Mitglieder ohnehin
// verbunden (§ 7).
func (n *Node) Discover(ctx context.Context, skill string) {
	if n.dht == nil || n.private || skill == "" {
		return
	}
	have := 0
	for _, a := range n.RemoteAgents() {
		if slices.Contains(a.Skills, skill) {
			have++
		}
	}
	if have >= minCandidates {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, findTimeout)
	defer cancel()
	seen := map[peer.ID]bool{}
	var found []peer.AddrInfo
	for shard := range skillShards {
		for ai := range n.dht.FindProvidersAsync(ctx, skillKey(n.opts.NetworkID, skill, shard), 2*findMaxNew) {
			if ai.ID == n.host.ID() || seen[ai.ID] || n.connected(ai.ID) || n.ignored(skill, ai.ID) {
				continue
			}
			seen[ai.ID] = true
			found = append(found, ai)
		}
	}
	if len(found) > findMaxNew {
		rand.Shuffle(len(found), func(i, j int) { found[i], found[j] = found[j], found[i] })
		found = found[:findMaxNew]
	}
	done := make(chan struct{})
	for _, ai := range found {
		go func() {
			defer func() { done <- struct{}{} }()
			if err := n.host.Connect(allowLimited(ctx), ai); err != nil {
				return
			}
			if err := n.fetchCapability(ctx, ai.ID); err != nil || !n.offers(ai.ID, skill) {
				n.ignoreFor(skill, ai.ID) // Verweis ohne passende Anzeige
			}
		}()
	}
	for range found {
		<-done
	}
}

func (n *Node) offers(p peer.ID, skill string) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	return slices.ContainsFunc(n.caps[p].Agents, func(a protocol.AgentInfo) bool { return slices.Contains(a.Skills, skill) })
}

func (n *Node) ignored(skill string, p peer.ID) bool {
	n.dhtMu.Lock()
	defer n.dhtMu.Unlock()
	return time.Now().Before(n.ignore[skill][p])
}

func (n *Node) ignoreFor(skill string, p peer.ID) {
	n.dhtMu.Lock()
	defer n.dhtMu.Unlock()
	if n.ignore[skill] == nil {
		n.ignore[skill] = make(map[peer.ID]time.Time)
	}
	n.ignore[skill][p] = time.Now().Add(ignoreFor)
}

// findTrusted sucht nicht verbundene trusted_peers über die DHT (Peer-Routing),
// etwa eigene Geräte an anderen Standorten ohne bekannte Adresse.
func (n *Node) findTrusted(ctx context.Context) {
	if n.dht == nil {
		return
	}
	for p := range n.trusted {
		if n.connected(p) {
			continue
		}
		go func() {
			cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			ai, err := n.dht.FindPeer(cctx, p)
			if err != nil {
				return
			}
			if err := n.host.Connect(allowLimited(cctx), ai); err != nil {
				slog.Debug("vertrauter peer gefunden, aber nicht erreichbar", "peer", p, "err", err)
			}
		}()
	}
}

// DHTInfo für `swarmainet status`.
type DHTInfo struct {
	Mode         string   `json:"mode"` // server | client
	RoutingTable int      `json:"routing_table"`
	Provided     []string `json:"provided,omitempty"`
}

func (n *Node) DHTInfo() *DHTInfo {
	if n.dht == nil {
		return nil
	}
	info := &DHTInfo{Mode: "client", RoutingTable: n.dht.RoutingTable().Size()}
	if n.dht.Mode() == dht.ModeServer {
		info.Mode = "server"
	}
	n.dhtMu.Lock()
	info.Provided = slices.Clone(n.provided)
	n.dhtMu.Unlock()
	return info
}
