// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Rainer Sestig. Kommerzielle Lizenz: siehe COMMERCIAL.md.

package p2p

import (
	"context"
	"log/slog"
	"slices"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/protocol/circuitv2/client"
	"github.com/libp2p/go-libp2p/p2p/protocol/circuitv2/proto"
	"github.com/libp2p/go-libp2p/p2p/protocol/circuitv2/relay"
	ma "github.com/multiformats/go-multiaddr"
)

const (
	maxReservations = 2
	reserveInterval = 30 * time.Second
	reserveRenew    = 5 * time.Minute // vor Ablauf erneuern
)

// Relay-Modi (relay_service): auto bietet Relay im offenen Netz an, sobald
// AutoNAT den Knoten als öffentlich erreichbar erkennt, mit sparsamen
// Grenzen; true immer (Helfer, Brückenknoten); false nie.
const (
	RelayAuto = "auto"
	RelayOn   = "true"
	RelayOff  = "false"
)

// RelayLimits überschreibt einzelne Grenzen; 0 übernimmt die Vorgabe.
type RelayLimits struct {
	Reservations    int
	CircuitsPerPeer int
	DataMiB         int
	DurationS       int
}

// relayResources sind die Grenzen aus spec/p2p-layer.md § 6. Die
// libp2p-Vorgabe von 128 KiB je Verbindung reicht für Aufträge nicht. Im
// privaten Schwarm (Vollvermaschung über einen Brückenknoten) erreicht jedes
// Mitglied der einen Seite jedes der anderen, daher mehr Verbindungen je Peer
// und keine Grenze je IP. Im Modus auto (Freiwillige im offenen Netz) halb so
// viele Reservierungen und je IP weniger.
func relayResources(private, auto bool, l RelayLimits) relay.Resources {
	r := relay.DefaultResources()
	r.Limit = &relay.RelayLimit{Duration: 10 * time.Minute, Data: 8 << 20}
	r.ReservationTTL = time.Hour
	r.MaxReservations = 32
	r.MaxReservationsPerIP = 4
	r.MaxCircuits = 2 // pro Peer
	switch {
	case private:
		r.MaxReservationsPerIP = r.MaxReservations
		r.MaxReservationsPerASN = r.MaxReservations
		r.MaxCircuits = 16
	case auto:
		r.MaxReservations = 16
		r.MaxReservationsPerIP = 2
	}
	if l.Reservations > 0 {
		r.MaxReservations = l.Reservations
		if private {
			r.MaxReservationsPerIP, r.MaxReservationsPerASN = l.Reservations, l.Reservations
		}
	}
	if l.CircuitsPerPeer > 0 {
		r.MaxCircuits = l.CircuitsPerPeer
	}
	if l.DataMiB > 0 {
		r.Limit.Data = int64(l.DataMiB) << 20
	}
	if l.DurationS > 0 {
		r.Limit.Duration = time.Duration(l.DurationS) * time.Second
	}
	return r
}

// upgradeDirect ersetzt eine Relay-Verbindung zu einem vertrauten Peer durch
// eine direkte, sobald eine seiner Adressen erreichbar ist. Relay-Verbindungen
// sind begrenzt, und solange eine besteht, wird sonst nicht neu gewählt. Fremde
// Peers bleiben Sache von DCUtR.
func (n *Node) upgradeDirect(ctx context.Context, p peer.ID) {
	if n.host.Network().Connectedness(p) != network.Limited || !n.wanted(p) {
		return
	}
	ctx, cancel := context.WithTimeout(network.WithForceDirectDial(ctx, limitedReason), dialTimeout)
	defer cancel()
	if err := n.host.Connect(ctx, peer.AddrInfo{ID: p}); err != nil {
		slog.Debug("keine direkte verbindung möglich", "peer", p, "err", err)
		return
	}
	slog.Info("direkte verbindung statt relay", "peer", p)
}

// supportsHop: bietet p Relay an? Bekannt erst nach Identify.
func (n *Node) supportsHop(p peer.ID) bool {
	ps, _ := n.host.Peerstore().SupportsProtocols(p, proto.ProtoIDv2Hop)
	return len(ps) > 0
}

// refreshReservations hält bis zu maxReservations Plätze bei direkt
// verbundenen Relays. Wer selbst Relay ist, braucht keinen. resvRun reiht
// Aufrufe hintereinander; resvMu schützt nur kurz die Daten, weil libp2p
// circuitAddrs bei jeder Adressabfrage aufruft.
func (n *Node) refreshReservations(ctx context.Context) {
	// Im offenen Netz brauchen öffentlich erreichbare Knoten keinen Platz.
	if n.relay != nil || !n.private && n.Reachability() == "public" {
		return
	}
	n.resvRun.Lock()
	defer n.resvRun.Unlock()
	now := time.Now()
	n.resvMu.Lock()
	for p, r := range n.resv {
		if now.After(r.Expiration.Add(-reserveRenew)) || n.host.Network().Connectedness(p) != network.Connected {
			delete(n.resv, p)
			n.host.ConnManager().Unprotect(p, "relay")
		}
	}
	have := len(n.resv)
	n.resvMu.Unlock()
	for _, p := range n.host.Network().Peers() {
		if have >= maxReservations {
			break
		}
		n.resvMu.Lock()
		_, ok := n.resv[p]
		n.resvMu.Unlock()
		if ok || n.host.Network().Connectedness(p) != network.Connected || !n.supportsHop(p) || !n.admitted(p) {
			continue
		}
		cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		r, err := client.Reserve(cctx, n.host, peer.AddrInfo{ID: p})
		cancel()
		if err != nil {
			slog.Info("relay-platz nicht reserviert", "relay", p, "err", err)
			continue
		}
		n.resvMu.Lock()
		n.resv[p] = r
		n.resvMu.Unlock()
		have++
		n.host.ConnManager().Protect(p, "relay")
		slog.Info("relay-platz reserviert", "relay", p, "bis", r.Expiration.Format(time.TimeOnly))
	}
	// Eigene Adressen über die Relays, gebildet aus der Adresse, unter der
	// das Relay tatsächlich erreicht wurde.
	n.resvMu.Lock()
	defer n.resvMu.Unlock()
	n.circuit = n.circuit[:0]
	for p := range n.resv {
		suffix := ma.StringCast("/p2p/" + p.String() + "/p2p-circuit")
		for _, c := range n.host.Network().ConnsToPeer(p) {
			if a := c.RemoteMultiaddr().Encapsulate(suffix); !c.Stat().Limited && !slices.ContainsFunc(n.circuit, a.Equal) {
				n.circuit = append(n.circuit, a)
			}
		}
	}
}

// circuitAddrs liefert die eigenen Adressen über reservierte Relays (für die
// AddrsFactory; libp2p ruft sie schon während libp2p.New auf).
func (n *Node) circuitAddrs() []ma.Multiaddr {
	n.resvMu.Lock()
	defer n.resvMu.Unlock()
	return slices.Clone(n.circuit)
}

// Reservations nennt die Relays, bei denen dieser Knoten einen Platz hat.
func (n *Node) Reservations() []string {
	n.resvMu.Lock()
	defer n.resvMu.Unlock()
	out := make([]string, 0, len(n.resv))
	for p := range n.resv {
		out = append(out, p.String())
	}
	slices.Sort(out)
	return out
}

// RelayService: bietet dieser Knoten gerade Relay an? Im Modus auto erst,
// wenn AutoNAT ihn als öffentlich erreichbar meldet.
func (n *Node) RelayService() bool {
	return n.relay != nil || n.opts.RelayService == RelayAuto && !n.private && n.Reachability() == "public"
}

// Reachability: public | private | unknown laut AutoNAT; im privaten Netz leer.
func (n *Node) Reachability() string {
	if n.private {
		return ""
	}
	switch network.Reachability(n.reach.Load()) {
	case network.ReachabilityPublic:
		return "public"
	case network.ReachabilityPrivate:
		return "private"
	}
	return "unknown"
}

// Private: Teilnahme an einem privaten Schwarm (PSK)?
func (n *Node) Private() bool { return n.private }

// Hints sind Diagnosehinweise für `swarmainet status` (spec/p2p-layer.md § 10).
func (n *Node) Hints() []string {
	peers := n.host.Network().Peers()
	var out []string
	if len(peers) == 0 {
		out = append(out, "Keine Peers verbunden: mDNS, static_peers und Firewall ("+
			portPhrase(n.host.Network().ListenAddresses(), "")+" eingehend) prüfen.")
	}
	if len(n.Reservations()) == 0 && len(n.host.Network().ListenAddresses()) == 0 {
		out = append(out, "Keine Listen-Adresse und kein Relay-Platz: Andere Knoten können diesen Knoten nicht anwählen.")
	}
	if h := n.memberHint(); h != "" {
		out = append(out, h)
	}
	if h := n.familyHint(); h != "" {
		out = append(out, h)
	}
	if h := n.mappingHint(); h != "" {
		out = append(out, h)
	}
	if !n.private && n.Reachability() == "private" && len(n.Reservations()) == 0 {
		out = append(out, "Offenes Netz: nicht öffentlich erreichbar und noch kein Relay-Platz. "+
			"Andere erreichen diesen Knoten erst, wenn ein Relay gefunden ist (oder per IPv6/Portfreigabe).")
	}
	if n.private && !n.RelayService() && len(peers) > 0 && !slices.ContainsFunc(peers, n.supportsHop) {
		out = append(out, "Privater Schwarm ohne Relay: Nur direkt erreichbare Knoten (LAN, IPv6, Portfreigabe) sind verbunden. "+
			"Für mehrere Standorte einen Helfer- oder Brückenknoten einrichten (relay_service: true).")
	}
	return out
}
