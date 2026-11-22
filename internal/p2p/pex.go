// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Rainer Sestig. Kommerzielle Lizenz: siehe COMMERCIAL.md.

package p2p

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"math/rand/v2"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	lp2pproto "github.com/libp2p/go-libp2p/core/protocol"
	ma "github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"

	"github.com/rainer-sharp/Ai-Swarms.net/internal/protocol"
)

// Peer-Austausch nach spec/p2p-layer.md § 2.4.
const (
	pexMaxPeers   = 32
	pexMaxDials   = 8
	pexMaxBytes   = 64 << 10
	pexInterval   = 10 * time.Minute
	pexAddrTTL    = 30 * time.Minute
	redialMin     = time.Minute
	redialMax     = 15 * time.Minute
	dialTimeout   = 15 * time.Second
	limitedReason = "swarmainet"
)

type pexPeer struct {
	PeerID string   `json:"peer_id"`
	Addrs  []string `json:"addrs"`
}

type pexMsg struct {
	Peers []pexPeer `json:"peers"`
}

// known merkt Peers, die per Peer-Austausch, Tracker oder Peer-Cache
// bekannt wurden, für den Neuaufbau im privaten Schwarm (Vollvermaschung,
// § 7). Im offenen Netz wird nichts gemerkt; dort tragen DHT und Peer-Cache.
type known struct {
	next  time.Time
	fails int
}

// maxKnown begrenzt die Mitgliederliste eines privaten Schwarms.
const maxKnown = 256

// remember trägt p in known ein (nur im privaten Netz, begrenzt).
func (n *Node) remember(p peer.ID) {
	if !n.private {
		return
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if _, ok := n.known[p]; !ok && len(n.known) < maxKnown {
		n.known[p] = &known{}
	}
}

// allowLimited erlaubt Streams über Relay-Verbindungen.
func allowLimited(ctx context.Context) context.Context {
	return network.WithAllowLimitedConn(ctx, limitedReason)
}

// connected: direkt oder über ein Relay verbunden.
func (n *Node) connected(p peer.ID) bool {
	c := n.host.Network().Connectedness(p)
	return c == network.Connected || c == network.Limited
}

// shareable: Adressen, die an andere weitergegeben werden dürfen. Loopback
// nie (außer für Tests mit announce_private), im offenen Netz auch keine
// privaten Adressen, jeweils auch nicht als Relay-Teil einer Circuit-Adresse.
func (n *Node) shareable(a ma.Multiaddr) bool {
	if n.opts.AnnouncePrivate {
		return true
	}
	first, _ := ma.SplitFunc(a, func(c ma.Component) bool { return c.Protocol().Code == ma.P_CIRCUIT })
	if first == nil || manet.IsIPLoopback(first) {
		return false
	}
	return n.private || manet.IsPublicAddr(first)
}

func (n *Node) handlePEX(s network.Stream) {
	from := s.Conn().RemotePeer()
	if n.limited(from) && !n.pexLimit.allow(from, time.Now()) {
		_ = s.Reset()
		return
	}
	defer s.Close()
	_ = s.SetDeadline(time.Now().Add(10 * time.Second))
	// Als Relay nennen wir für jeden Peer auch den Weg über uns. libp2p
	// speichert Circuit-Adressen über den eigenen Knoten nicht, und Peers
	// hinter NAT haben oft keine andere Adresse. Die Relay-Adresse ist die
	// dieser Verbindung; der Anfragende ist darüber schon verbunden.
	var via ma.Multiaddr
	if n.RelayService() && !s.Conn().Stat().Limited { // keine verschachtelten Circuit-Adressen
		via = s.Conn().LocalMultiaddr().Encapsulate(ma.StringCast("/p2p/" + n.host.ID().String() + "/p2p-circuit"))
	}
	var msg pexMsg
	// Im privaten Schwarm nennen wir auch uns selbst: Identify verwirft private
	// Adressen, wenn die Gegenstelle öffentlich aussieht, also auch über ein
	// Relay im Internet. Sonst fände ein Peer im selben LAN nie den direkten Weg.
	if n.private {
		self := pexPeer{PeerID: n.host.ID().String()}
		for _, a := range n.host.Addrs() {
			if n.shareable(a) {
				self.Addrs = append(self.Addrs, a.String())
			}
		}
		if len(self.Addrs) > 0 {
			msg.Peers = append(msg.Peers, self)
		}
	}
	for _, p := range n.host.Network().Peers() {
		if len(msg.Peers) >= pexMaxPeers {
			break
		}
		// Im offenen Netz bleibt der Vertrauenskreis unsichtbar; im Mitgliedsnetz
		// werden Fremde nie genannt.
		if p == from || !n.private && n.Trusted(p) || !n.admitted(p) {
			continue
		}
		e := pexPeer{PeerID: p.String()}
		addrs := n.host.Peerstore().Addrs(p)
		if via != nil {
			addrs = append(addrs, via)
		}
		for _, a := range addrs {
			if n.shareable(a) {
				e.Addrs = append(e.Addrs, a.String())
			}
		}
		if len(e.Addrs) > 0 {
			msg.Peers = append(msg.Peers, e)
		}
	}
	b, _ := json.Marshal(msg)
	_, _ = s.Write(append(b, '\n'))
}

// fetchPeers fragt p nach seinen Peers und verbindet sich mit neuen.
func (n *Node) fetchPeers(ctx context.Context, p peer.ID) {
	if !n.admitted(p) {
		return
	}
	ctx, cancel := context.WithTimeout(allowLimited(ctx), 10*time.Second)
	defer cancel()
	s, err := n.host.NewStream(ctx, p, lp2pproto.ID(protocol.ProtoPEX))
	if err != nil {
		return
	}
	defer s.Close()
	_ = s.SetDeadline(time.Now().Add(10 * time.Second))
	line, err := bufio.NewReader(io.LimitReader(s, pexMaxBytes)).ReadBytes('\n')
	if err != nil {
		return
	}
	var msg pexMsg
	if err := json.Unmarshal(line, &msg); err != nil {
		slog.Debug("peer-austausch ungültig", "peer", p, "err", err)
		return
	}
	dials := 0
	for i, e := range msg.Peers {
		if i >= pexMaxPeers {
			break
		}
		id, err := peer.Decode(e.PeerID)
		if err != nil || id == n.host.ID() {
			continue
		}
		var addrs []ma.Multiaddr
		for _, as := range e.Addrs {
			if a, err := ma.NewMultiaddr(as); err == nil && n.shareable(a) {
				addrs = append(addrs, a)
			}
		}
		if len(addrs) == 0 {
			continue
		}
		n.host.Peerstore().AddAddrs(id, addrs, pexAddrTTL)
		n.remember(id)
		if !n.connected(id) && dials < pexMaxDials {
			dials++
			go n.dialKnown(ctx, id)
		}
	}
	go n.upgradeDirect(context.WithoutCancel(ctx), p) // p kann eben Adressen genannt haben
}

// dialKnown verbindet mit einem bekannten Peer über die Adressen im
// Peerstore, notfalls über ein Relay, und rechnet den Backoff fort.
func (n *Node) dialKnown(ctx context.Context, p peer.ID) {
	ctx, cancel := context.WithTimeout(allowLimited(context.WithoutCancel(ctx)), dialTimeout)
	defer cancel()
	err := n.host.Connect(ctx, peer.AddrInfo{ID: p})
	n.mu.Lock()
	defer n.mu.Unlock()
	k := n.known[p]
	if k == nil {
		return
	}
	if err != nil {
		k.fails++
		k.next = time.Now().Add(min(redialMin<<min(k.fails-1, 4), redialMax))
		slog.Debug("bekannter peer nicht erreichbar", "peer", p, "err", err)
		return
	}
	k.fails, k.next = 0, time.Time{}
}

// redialYield: So lange lässt der Peer mit der größeren ID dem anderen den
// Vortritt beim Neuwählen, plus bis zu einer Sekunde Zufall.
const redialYield = 2 * time.Second

// redialLost wählt einen Peer nach einem Abriss neu an. Wählen beide Seiten
// zugleich, entsteht über TCP mit Port-Wiederverwendung ein Simultaneous
// Open: eine Verbindung, auf der beide Client sind; der Sicherheits-Handshake
// scheitert auf beiden Seiten und beide sperren einander (dial backoff).
// Deshalb wählt nur der Peer mit der kleineren ID sofort; der andere wartet
// und wählt nur, wenn dann noch keine Verbindung besteht (etwa weil der
// andere hinter NAT nicht durchkommt).
func (n *Node) redialLost(ctx context.Context, p peer.ID) {
	if n.host.ID() > p {
		t := time.NewTimer(redialYield + rand.N(time.Second))
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-t.C:
		}
		if n.connected(p) {
			return
		}
	}
	n.dialKnown(ctx, p)
}

// connectKnown baut im privaten Schwarm Verbindungen zu allen bekannten
// Mitgliedern wieder auf, etwa nach Ablauf einer Relay-Verbindung.
func (n *Node) connectKnown(ctx context.Context) {
	if !n.private {
		return
	}
	now := time.Now()
	n.mu.Lock()
	var due []peer.ID
	for p, k := range n.known {
		if now.After(k.next) && !n.connected(p) && len(n.host.Peerstore().Addrs(p)) > 0 {
			due = append(due, p)
		}
	}
	n.mu.Unlock()
	for _, p := range due {
		go n.dialKnown(ctx, p)
	}
}

// supportsPEX: spricht p den Peer-Austausch? Bekannt erst nach Identify.
func (n *Node) supportsPEX(p peer.ID) bool {
	ps, _ := n.host.Peerstore().SupportsProtocols(p, protocol.ProtoPEX)
	return len(ps) > 0
}
