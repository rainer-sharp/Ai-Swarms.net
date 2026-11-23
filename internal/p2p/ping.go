// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Rainer Sestig. Kommerzielle Lizenz: siehe COMMERCIAL.md.

package p2p

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/protocol/ping"
)

// Diagnose eines einzelnen Peers für `swarmainet ping` (spec/p2p-layer.md § 10).
const (
	pingDHTTimeout     = 15 * time.Second
	pingConnectTimeout = 20 * time.Second
	pingDirectWait     = 5 * time.Second
)

type PingReport struct {
	PeerID  string   `json:"peer_id"`
	OK      bool     `json:"ok"`
	Via     string   `json:"via,omitempty"` // direct | relay
	RTTms   float64  `json:"rtt_ms,omitempty"`
	Trusted bool     `json:"trusted"`
	Agents  []string `json:"agents,omitempty"`
	Addrs   []string `json:"addrs,omitempty"`
	Steps   []string `json:"steps"`
	Hints   []string `json:"hints,omitempty"`
}

func (r *PingReport) step(format string, args ...any) {
	r.Steps = append(r.Steps, fmt.Sprintf(format, args...))
}
func (r *PingReport) hint(s string) { r.Hints = append(r.Hints, s) }

// Ping sucht den Peer (Peer-ID oder Multiadresse mit /p2p/…), verbindet sich,
// misst die Laufzeit und holt die Fähigkeitsanzeige. Fehlschläge stehen im
// Bericht, nicht im Fehler; der Fehler meldet nur ungültige Eingaben.
func (n *Node) Ping(ctx context.Context, target string) (*PingReport, error) {
	var id peer.ID
	if strings.HasPrefix(target, "/") {
		ai, err := peer.AddrInfoFromString(target)
		if err != nil {
			return nil, fmt.Errorf("adresse %q: %w", target, err)
		}
		id = ai.ID
		n.host.Peerstore().AddAddrs(id, ai.Addrs, pexAddrTTL)
	} else {
		var err error
		if id, err = peer.Decode(target); err != nil {
			return nil, fmt.Errorf("peer-id %q: %w", target, err)
		}
	}
	if id == n.host.ID() {
		return nil, errors.New("das ist dieser knoten selbst")
	}
	r := &PingReport{PeerID: id.String(), Trusted: n.Trusted(id)}

	if n.connected(id) {
		r.step("Bereits verbunden.")
	} else if !n.pingConnect(ctx, id, r) {
		return r, nil
	}
	r.Via = n.via(id)
	if r.Via == "relay" {
		r.step("Verbunden über Relay; warte bis %s auf eine direkte Verbindung (Hole Punching) …", pingDirectWait)
		deadline := time.Now().Add(pingDirectWait)
		for time.Now().Before(deadline) && n.via(id) == "relay" {
			time.Sleep(100 * time.Millisecond)
		}
		if r.Via = n.via(id); r.Via == "direct" {
			r.step("Hole Punching erfolgreich: jetzt direkt verbunden.")
		} else {
			r.step("Bleibt über Relay (für Text-Aufträge ausreichend).")
		}
	} else {
		r.step("Direkt verbunden.")
	}
	r.Addrs = n.connAddrs(id)

	pctx, cancel := context.WithTimeout(allowLimited(ctx), 10*time.Second)
	res := <-ping.Ping(pctx, n.host, id)
	cancel()
	if res.Error != nil {
		r.step("Ping fehlgeschlagen: %v", res.Error)
		return r, nil
	}
	r.RTTms = float64(res.RTT.Microseconds()) / 1000
	r.step("Ping: %.1f ms.", r.RTTms)
	r.OK = true

	if err := n.fetchCapability(ctx, id); err != nil {
		r.step("Fähigkeitsanzeige nicht abrufbar: %v", err)
		return r, nil
	}
	n.mu.Lock()
	c := n.caps[id]
	n.mu.Unlock()
	for _, a := range c.Agents {
		r.Agents = append(r.Agents, a.AgentID)
	}
	switch {
	case len(r.Agents) == 0:
		r.step("Bietet keine Agenten an (provide: false oder reiner Helfer).")
	case !r.Trusted && !slices.Contains(c.Policy.Accepts, "public"):
		r.step("Bietet %d Agenten an, nimmt von diesem Knoten aber nichts an (nicht vertraut, keine öffentlichen Aufträge).", len(r.Agents))
	default:
		r.step("Bietet %d Agenten an: %s.", len(r.Agents), strings.Join(r.Agents, ", "))
	}
	return r, nil
}

// pingConnect sammelt Adressen (Peerstore, DHT) und verbindet sich.
func (n *Node) pingConnect(ctx context.Context, id peer.ID, r *PingReport) bool {
	addrs := n.host.Peerstore().Addrs(id)
	r.step("%d Adressen bekannt (Peer-Cache, Peer-Austausch, mDNS, Eingabe).", len(addrs))
	if n.dht != nil {
		dctx, cancel := context.WithTimeout(ctx, pingDHTTimeout)
		ai, err := n.dht.FindPeer(dctx, id)
		cancel()
		if err != nil {
			r.step("DHT: nicht gefunden (%v).", err)
		} else {
			r.step("DHT: %d Adressen gefunden.", len(ai.Addrs))
			n.host.Peerstore().AddAddrs(id, ai.Addrs, pexAddrTTL)
		}
	}
	addrs = n.host.Peerstore().Addrs(id)
	for _, a := range addrs {
		r.Addrs = append(r.Addrs, a.String())
	}
	if len(addrs) == 0 {
		r.step("Keine Adresse bekannt.")
		r.hint("Der Peer ist weder im Peer-Cache noch über Peer-Austausch oder DHT bekannt. Adresse direkt angeben (swarmainet ping /ip4/…/tcp/4711/p2p/<peer-id>) oder unter static_peers eintragen.")
		return false
	}
	cctx, cancel := context.WithTimeout(allowLimited(ctx), pingConnectTimeout)
	err := n.host.Connect(cctx, peer.AddrInfo{ID: id})
	cancel()
	if err == nil {
		return true
	}
	r.step("Verbindung fehlgeschlagen: %v", err)
	r.hint("Läuft der Knoten, und lässt seine Firewall " + portPhrase(addrs, "") + " eingehend zu?")
	if n.private {
		r.hint("Privater Schwarm: Beide Seiten brauchen denselben Netzschlüssel (network_key_file); mit falschem Schlüssel scheitert schon der Verbindungsaufbau.")
	}
	if !slices.ContainsFunc(r.Addrs, func(a string) bool { return strings.Contains(a, "/p2p-circuit") }) {
		r.hint("Keine Relay-Adresse bekannt. Liegt der Peer hinter NAT oder an einem anderen Standort, braucht das Netz einen Helfer- oder Brückenknoten (relay_service: true), mit dem beide verbunden sind.")
	}
	return false
}

func (n *Node) via(id peer.ID) string {
	if n.host.Network().Connectedness(id) == network.Connected {
		return "direct"
	}
	return "relay"
}

// connAddrs: Gegenadressen der bestehenden Verbindungen.
func (n *Node) connAddrs(id peer.ID) []string {
	var out []string
	for _, c := range n.host.Network().ConnsToPeer(id) {
		out = append(out, c.RemoteMultiaddr().String())
	}
	return out
}
