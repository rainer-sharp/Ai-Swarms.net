// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Rainer Sestig. Kommerzielle Lizenz: siehe COMMERCIAL.md.

// Package p2p ist die Transportschicht: libp2p-Host, mDNS, statische Peers,
// Relay, Peer-Austausch, Austausch signierter Fähigkeitsanzeigen und das
// Auftragsprotokoll.
package p2p

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/event"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/pnet"
	lp2pproto "github.com/libp2p/go-libp2p/core/protocol"
	"github.com/libp2p/go-libp2p/p2p/discovery/mdns"
	bhost "github.com/libp2p/go-libp2p/p2p/host/basic"
	"github.com/libp2p/go-libp2p/p2p/protocol/circuitv2/client"
	"github.com/libp2p/go-libp2p/p2p/protocol/circuitv2/relay"
	"github.com/libp2p/go-libp2p/p2p/transport/tcp"
	ma "github.com/multiformats/go-multiaddr"

	"github.com/rainer-sharp/Ai-Swarms.net/internal/identity"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/membership"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/orchestrator"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/protocol"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/receipt"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/rem"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/tracker"
)

const (
	maxMsgBytes     = 4 << 20
	capabilityTTL   = 5 * time.Minute
	capabilityGrace = 30 * time.Second // nach Verbindungsabriss noch wählbar
	refreshInterval = time.Minute
	maxStreamsPeer  = 4
)

// Provider ist die lokale Seite, die fremde Teilaufträge ausführt.
type Provider interface {
	Offer() []protocol.AgentInfo
	SlotsNow() int
	Admit(public bool) (*rem.Slot, error)
	RunLocal(ctx context.Context, agentID string, t orchestrator.Task) (protocol.Result, error)
}

type Options struct {
	Identity      *identity.Identity
	Listen        []string
	NetworkID     string
	PSKFile       string
	MDNS          bool
	StaticPeers   []string
	Bootstrap     []string // Einstieg, solange needEntry gilt (ohne Peers, DHT oder Relay-Platz)
	Trackers      []string // HTTPS-Adressbriefkästen (spec/p2p-layer.md § 2.3)
	TrustedPeers  []string
	Provide       bool // Fähigkeitsanzeige und Auftragsannahme aktivieren
	AcceptsPublic bool
	// RelayService: auto | true | false (siehe RelayAuto). Leer wie auto.
	RelayService string
	RelayLimits  RelayLimits
	// DNSSeeds: Domains mit _dnsaddr-TXT-Einträgen, angewählt wie Bootstrap.
	DNSSeeds  []string
	PEX       bool   // Peer-Austausch mit verbundenen Knoten
	PeerCache string // Datei des Peer-Caches; leer = keiner
	DHTServer string // auto | true | false
	// AnnouncePrivate: private Adressen auch im offenen Netz veröffentlichen
	// (nur für Testnetze im LAN; spec/p2p-layer.md § 3).
	AnnouncePrivate bool
	// Announce: zusätzlich bekannt gegebene Adressen (ohne /p2p), etwa der
	// DNS-Name einer Portfreigabe.
	Announce []string
	// PortMapping: Portfreigabe per UPnP/NAT-PMP anfordern (spec § 3, Opt-in).
	PortMapping bool
	Provider    Provider
	Receipts    *receipt.Store
	// Membership macht das Netz zu einem Mitgliedsnetz (spec/membership.md).
	Membership *Membership
	// AllowedTools: Werkzeuge, die Modelle dieses Netzes sehen dürfen
	// (spec/privacy-classes.md). Im PSK-Netz heißt nil: alle.
	AllowedTools []string
}

type Node struct {
	opts    Options
	host    host.Host
	id      *identity.Identity
	replay  *protocol.ReplayCache
	private bool
	member  *memberState       // nur im Mitgliedsnetz
	ctx     context.Context    // endet mit dem Knoten
	cancel  context.CancelFunc // beendet die Hintergrundschleifen
	sealer  *tracker.Sealer    // Tracker-Einträge im privaten Netz
	relay   *relay.Relay
	reach   atomic.Int32     // network.Reachability laut AutoNAT (nur offenes Netz)
	nat     bhost.NATManager // nur mit PortMapping
	started time.Time

	taskLimit, capLimit, pexLimit *rateLimiter

	mu      sync.Mutex
	caps    map[peer.ID]protocol.Capability
	trusted map[peer.ID]bool
	static  map[peer.ID]bool
	streams map[peer.ID]int
	known   map[peer.ID]*known
	cache   *peerCache
	// famReach: Erreichbarkeit je Adressfamilie laut AutoNAT (nur offenes Netz)
	famReach map[string]string

	resvRun sync.Mutex // reiht refreshReservations
	resvMu  sync.Mutex
	resv    map[peer.ID]*client.Reservation
	circuit []ma.Multiaddr

	dht          *dht.IpfsDHT
	dhtMu        sync.Mutex
	shards       map[string]int
	provided     []string
	provideNext  time.Time
	providing    bool
	provideFails int
	ignore       map[string]map[peer.ID]time.Time
}

func New(ctx context.Context, o Options) (*Node, error) {
	ctx, cancel := context.WithCancel(ctx)
	started := false
	defer func() {
		if !started {
			cancel()
		}
	}()
	n := &Node{
		ctx: ctx, cancel: cancel, started: time.Now(),
		opts: o, id: o.Identity,
		replay:  protocol.NewReplayCache(100_000),
		caps:    make(map[peer.ID]protocol.Capability),
		trusted: make(map[peer.ID]bool),
		static:  make(map[peer.ID]bool),
		streams: make(map[peer.ID]int),
		cache:   loadPeerCache(o.PeerCache),
		known:   make(map[peer.ID]*known),
		resv:    make(map[peer.ID]*client.Reservation),

		taskLimit: newRateLimiter(taskLimitPeer, taskLimitTotal),
		capLimit:  newRateLimiter(capLimitPeer, 0),
		pexLimit:  newRateLimiter(pexLimitPeer, 0),
	}
	for _, s := range o.TrustedPeers {
		pid, err := peer.Decode(s)
		if err != nil {
			return nil, fmt.Errorf("trusted_peers %q: %w", s, err)
		}
		n.trusted[pid] = true
	}
	for _, s := range o.StaticPeers {
		if ai, err := peer.AddrInfoFromString(s); err == nil {
			n.static[ai.ID] = true
		}
	}

	var announce []ma.Multiaddr
	for _, s := range o.Announce {
		a, err := ma.NewMultiaddr(s)
		if err != nil {
			return nil, fmt.Errorf("announce %q: %w", s, err)
		}
		announce = append(announce, a)
	}

	if o.Membership != nil {
		if o.PSKFile != "" {
			return nil, errors.New("mitgliedsnetz und netzschlüssel schließen einander aus")
		}
		var err error
		if n.member, err = newMemberState(*o.Membership, o.Identity.ID.String()); err != nil {
			return nil, err
		}
		n.private = true // geschlossen: kein AutoNAT, Vollvermaschung, private Adressen unter Zugelassenen
		if n.member.own.Role == membership.RoleHelper {
			o.Provide = false
			n.opts.Provide = false
		}
	}

	listen := o.Listen
	lopts := []libp2p.Option{
		libp2p.Identity(o.Identity.Priv),
		libp2p.EnableRelay(), // Circuit-Transport, auch ohne eigene Listen-Adressen
		libp2p.EnableHolePunching(),
		libp2p.AddrsFactory(func(addrs []ma.Multiaddr) []ma.Multiaddr {
			// Im offenen Netz keine internen Adressen in Identify und DHT;
			// mDNS nutzt die Schnittstellenadressen und bleibt unberührt.
			addrs = append(append(addrs, announce...), n.circuitAddrs()...)
			return slices.DeleteFunc(addrs, func(a ma.Multiaddr) bool { return !n.shareable(a) })
		}),
	}
	if o.PSKFile != "" {
		f, err := os.Open(o.PSKFile)
		if err != nil {
			return nil, fmt.Errorf("netz-schlüssel: %w", err)
		}
		psk, err := pnet.DecodeV1PSK(f)
		f.Close()
		if err != nil {
			return nil, fmt.Errorf("netz-schlüssel: %w", err)
		}
		if n.sealer, err = tracker.NewSealer(psk); err != nil {
			return nil, err
		}
		// Private Netze funktionieren in libp2p nur über TCP.
		listen = slices.DeleteFunc(slices.Clone(listen), func(a string) bool { return !strings.Contains(a, "/tcp/") })
		lopts = append(lopts, libp2p.PrivateNetwork(psk), libp2p.Transport(tcp.NewTCPTransport))
		n.private = true
	}
	if o.PortMapping {
		lopts = append(lopts, libp2p.NATManager(func(net network.Network) bhost.NATManager {
			n.nat = newNATManager(net)
			return n.nat
		}))
	}
	if len(listen) == 0 {
		// Nur ausgehend, z. B. hinter NAT; erreichbar über Relays.
		lopts = append(lopts, libp2p.NoListenAddrs)
	} else {
		lopts = append(lopts, libp2p.ListenAddrStrings(listen...))
	}
	if !n.private {
		// AutoNAT: Erreichbarkeit prüfen und für andere prüfen (§ 3). Im
		// privaten Netz zählt nur die Erreichbarkeit unter den Mitgliedern.
		lopts = append(lopts, libp2p.EnableNATService(), libp2p.EnableAutoNATv2())
		if o.RelayService == RelayAuto || o.RelayService == "" {
			// libp2p schaltet den Dienst erst bei öffentlicher Erreichbarkeit frei.
			lopts = append(lopts, libp2p.EnableRelayService(relay.WithResources(relayResources(false, true, o.RelayLimits))))
		}
	}
	h, err := libp2p.New(lopts...)
	if err != nil {
		return nil, err
	}
	n.host = h
	if o.RelayService == RelayOn {
		// Ausdrücklich angeboten (Helfer, Brückenknoten): sofort, auch ohne
		// öffentliche Erreichbarkeit; im privaten Schwarm zählt nur, dass die
		// Mitglieder verbunden sind.
		ropts := []relay.Option{relay.WithResources(relayResources(n.private, false, o.RelayLimits))}
		if n.member != nil {
			ropts = append(ropts, relay.WithACL(memberACL{n}))
		}
		if n.relay, err = relay.New(h, ropts...); err != nil {
			h.Close()
			return nil, err
		}
	}

	n.handle(protocol.ProtoCapability, canTask, n.handleCapability)
	if o.Provide {
		n.handle(protocol.ProtoTask, canTask, n.handleTask)
	}
	if o.PEX {
		n.handle(protocol.ProtoPEX, anyRole, n.handlePEX)
	}
	if n.member != nil {
		h.SetStreamHandler(lp2pproto.ID(protocol.ProtoMember), n.handleAdmission)
		h.SetStreamHandler(lp2pproto.ID(protocol.ProtoMemberJoin), n.handleJoin)
		n.handle(protocol.ProtoMemberCRL, anyRole, n.handleCRLs)
		n.handle(protocol.ProtoMemberRenew, anyRole, n.handleRenew)
	}
	if n.member == nil { // Mitgliedsnetze laufen ohne DHT (§ 6)
		if err := n.startDHT(ctx); err != nil {
			h.Close()
			return nil, fmt.Errorf("dht: %w", err)
		}
	}
	h.Network().Notify(&network.NotifyBundle{
		ConnectedF: func(_ network.Network, c network.Conn) {
			if n.member != nil {
				n.onConnected(c)
				return
			}
			go n.fetchCapability(context.Background(), c.RemotePeer())
		},
		DisconnectedF: func(_ network.Network, c network.Conn) { n.peerLost(ctx, c) },
	})

	if o.MDNS {
		svc := mdns.NewMdnsService(h, mdnsName(o.NetworkID, n.member != nil), &mdnsNotifee{n: n})
		if err := svc.Start(); err != nil {
			slog.Warn("mdns nicht verfügbar", "err", err)
		}
	}
	sub, err := h.EventBus().Subscribe(new(event.EvtPeerIdentificationCompleted))
	if err != nil {
		h.Close()
		return nil, err
	}
	go n.onIdentified(ctx, sub)
	rsub, err := h.EventBus().Subscribe(new(event.EvtLocalReachabilityChanged))
	if err != nil {
		h.Close()
		return nil, err
	}
	if !n.private {
		asub, err := h.EventBus().Subscribe(new(event.EvtHostReachableAddrsChanged))
		if err != nil {
			h.Close()
			return nil, err
		}
		go n.watchAddrReach(ctx, asub)
	}
	go func() {
		defer rsub.Close()
		for e := range rsub.Out() {
			r := e.(event.EvtLocalReachabilityChanged).Reachability
			n.reach.Store(int32(r))
			slog.Info("erreichbarkeit", "netz", o.NetworkID, "status", r.String())
		}
	}()
	if n.member != nil {
		go n.watchCRLInbox(ctx)
	}
	go n.dialCache(ctx)
	go n.loop(ctx)
	started = true
	return n, nil
}

// NetworkID: Kennung des Netzes dieses Knotens.
func (n *Node) NetworkID() string { return n.opts.NetworkID }

// Knows: ist der Peer (Peer-ID oder Multiadresse) verbunden oder mit
// Adressen bekannt? Für die Wahl des Netzes bei `swarmainet ping`.
func (n *Node) Knows(target string) bool {
	id, err := peer.Decode(target)
	if ai, aerr := peer.AddrInfoFromString(target); aerr == nil {
		id, err = ai.ID, nil
	}
	return err == nil && (n.connected(id) || len(n.host.Peerstore().Addrs(id)) > 0)
}

func (n *Node) Close() error {
	n.cancel()
	n.saveCache()
	if n.dht != nil {
		n.dht.Close()
	}
	if n.relay != nil {
		n.relay.Close()
	}
	return n.host.Close()
}
func (n *Node) Host() host.Host { return n.host }
func (n *Node) ID() peer.ID     { return n.host.ID() }

type mdnsNotifee struct{ n *Node }

func (m *mdnsNotifee) HandlePeerFound(ai peer.AddrInfo) {
	if ai.ID == m.n.host.ID() || m.n.memberRevoked() {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := m.n.host.Connect(ctx, ai); err != nil {
		slog.Debug("mdns-peer nicht erreichbar", "peer", ai.ID, "err", err)
	}
}

// onIdentified reagiert auf neu identifizierte Peers: erst jetzt ist
// bekannt, ob sie Relay und Peer-Austausch sprechen.
func (n *Node) onIdentified(ctx context.Context, sub event.Subscription) {
	defer sub.Close()
	for {
		select {
		case <-ctx.Done():
			return
		case e, ok := <-sub.Out():
			if !ok {
				return
			}
			p := e.(event.EvtPeerIdentificationCompleted).Peer
			// Im Mitgliedsnetz zählt ein Peer erst mit der Zulassung, die
			// vor oder nach dem Identify abgeschlossen sein kann.
			if n.admitted(p) {
				n.peerReady(ctx, p)
			}
		}
	}
}

func (n *Node) peerReady(ctx context.Context, p peer.ID) {
	n.rememberPeer(p) // auch kurze Verbindungen landen im Peer-Cache
	go n.upgradeDirect(ctx, p)
	if n.supportsHop(p) {
		go n.refreshReservations(ctx)
	}
	if n.opts.PEX && n.supportsPEX(p) {
		go n.fetchPeers(ctx, p)
	}
	if n.supportsDHT(p) {
		go n.provide(ctx) // erst mit gefüllter Routing-Tabelle möglich
	}
}

// mdnsName: Im Mitgliedsnetz verrät der Dienstname die Kennung nicht (§ 6).
func mdnsName(networkID string, member bool) string {
	if !member {
		return "swarmainet-" + networkID
	}
	h := sha256.Sum256([]byte("swarmainet/member-mdns/v1\n" + networkID))
	return "swarmainet-" + hex.EncodeToString(h[:8])
}

// loop verbindet statische und bekannte Peers, hält Relay-Plätze, tauscht
// Peers aus und frischt Fähigkeitsanzeigen auf.
func (n *Node) loop(ctx context.Context) {
	t := time.NewTicker(refreshInterval)
	defer t.Stop()
	lastPEX := time.Now()
	var lastTrusted, lastPublish, lastFetch time.Time
	for {
		n.recheckMembers()
		if !n.memberRevoked() {
			n.connectStatic(ctx)
			n.connectBootstrap(ctx)
			n.connectKnown(ctx)
		}
		n.saveCache()
		go n.refreshReservations(ctx)
		go n.provide(ctx)
		n.syncTrackers(ctx, &lastPublish, &lastFetch)
		if time.Since(lastTrusted) >= trustedLookup {
			lastTrusted = time.Now()
			n.findTrusted(ctx)
		}
		pex := n.opts.PEX && time.Since(lastPEX) >= pexInterval
		if pex {
			lastPEX = time.Now()
		}
		for _, p := range n.host.Network().Peers() {
			if !n.admitted(p) {
				continue // onConnected trennt ihn, wenn es so bleibt
			}
			go n.upgradeDirect(ctx, p)
			if n.member == nil || n.Trusted(p) && n.memberCanTask() {
				go func() {
					if err := n.fetchCapability(ctx, p); err != nil && ctx.Err() == nil && n.wanted(p) {
						slog.Info("fähigkeitsanzeige nicht abgerufen", "peer", p, "err", err)
					}
				}()
			}
			if pex && n.supportsPEX(p) {
				go n.fetchPeers(ctx, p)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (n *Node) connectStatic(ctx context.Context) {
	for _, s := range n.opts.StaticPeers {
		ai, err := peer.AddrInfoFromString(s)
		if err != nil {
			slog.Warn("statischer peer ungültig", "addr", s, "err", err)
			continue
		}
		// Selbst aufgebaute Verbindungen schützen: Ein Brückenknoten hinter NAT
		// ist für die Gegenseite nicht anwählbar.
		n.host.ConnManager().Protect(ai.ID, "static")
		if n.host.Network().Connectedness(ai.ID) == network.Connected {
			continue
		}
		cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		if err := n.host.Connect(cctx, *ai); err != nil {
			slog.Info("statischer peer nicht erreichbar", "peer", ai.ID, "err", err)
		}
		cancel()
	}
}

// needEntry: Einstiegsknoten werden gebraucht, solange kein Peer verbunden
// ist, die DHT-Routing-Tabelle leer ist oder ein Knoten ohne öffentliche
// Erreichbarkeit keinen Relay-Platz hat (spec/p2p-layer.md § 2). Sonst
// bleiben zwei Knoten hinter NAT, die nach dem Ausfall des Startknotens nur
// noch einander kennen, eine Insel, auch wenn er zurückkommt.
func (n *Node) needEntry() bool {
	if len(n.host.Network().Peers()) == 0 {
		return true
	}
	if n.dht != nil && n.dht.RoutingTable().Size() == 0 {
		return true
	}
	if n.relay == nil && n.Reachability() != "public" {
		n.resvMu.Lock()
		defer n.resvMu.Unlock()
		return len(n.resv) == 0
	}
	return false
}

// entryLogLevel: Fehlgeschlagene Einstiegsversuche sind nur auffällig, solange
// gar kein Peer verbunden ist; sonst wiederholen sie sich jede Minute.
func (n *Node) entryLogLevel() slog.Level {
	if len(n.host.Network().Peers()) == 0 {
		return slog.LevelInfo
	}
	return slog.LevelDebug
}

// connectBootstrap wählt die Einstiegsadressen und DNS-Seeds an, solange
// needEntry gilt; sonst tragen Peer-Austausch, DHT und Peer-Cache.
func (n *Node) connectBootstrap(ctx context.Context) {
	if !n.needEntry() {
		return
	}
	for _, d := range n.opts.DNSSeeds {
		go n.connectSeed(ctx, d)
	}
	for _, s := range n.opts.Bootstrap {
		ai, err := peer.AddrInfoFromString(s)
		if err != nil {
			slog.Warn("bootstrap-adresse ungültig", "addr", s, "err", err)
			continue
		}
		if ai.ID == n.host.ID() || n.connected(ai.ID) {
			continue
		}
		go func() {
			cctx, cancel := context.WithTimeout(allowLimited(ctx), 10*time.Second)
			defer cancel()
			if err := n.host.Connect(cctx, *ai); err != nil {
				slog.Log(ctx, n.entryLogLevel(), "einstiegsknoten nicht erreichbar", "peer", ai.ID, "err", err)
			}
		}()
	}
}

// Connect verbindet direkt mit einer Multiadresse inkl. /p2p/<peer-id>.
func (n *Node) Connect(ctx context.Context, addr string) error {
	ai, err := peer.AddrInfoFromString(addr)
	if err != nil {
		return err
	}
	if err := n.host.Connect(ctx, *ai); err != nil {
		return err
	}
	return n.fetchCapability(ctx, ai.ID)
}

// Trusted: im privaten Schwarm sind alle Teilnehmer vertraut, sonst nur
// ausdrücklich freigegebene Schlüssel.
func (n *Node) Trusted(p peer.ID) bool {
	if n.member != nil {
		role, ok := n.member.role(p)
		return ok && canTask(role)
	}
	if n.private {
		return true
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.trusted[p]
}

func (n *Node) Addrs() []string {
	var out []string
	for _, a := range n.host.Addrs() {
		out = append(out, a.Encapsulate(ma.StringCast("/p2p/"+n.host.ID().String())).String())
	}
	return out
}

// --- Fähigkeitsanzeigen ---

func (n *Node) ownCapability() protocol.Capability {
	accepts := []string{"trusted"}
	if n.opts.AcceptsPublic {
		accepts = append(accepts, "public")
	}
	c := protocol.Capability{
		PeerID:    n.host.ID().String(),
		ExpiresAt: time.Now().Add(capabilityTTL).UTC(),
		Policy:    protocol.Policy{Accepts: accepts, Retention: "no-prompt-storage"},
	}
	if n.opts.Provide && n.opts.Provider != nil {
		c.Agents = n.opts.Provider.Offer()
		c.Availability.SlotsNow = n.opts.Provider.SlotsNow()
	}
	return c
}

func (n *Node) handleCapability(s network.Stream) {
	if from := s.Conn().RemotePeer(); n.limited(from) && !n.capLimit.allow(from, time.Now()) {
		_ = s.Reset()
		return
	}
	defer s.Close()
	_ = s.SetDeadline(time.Now().Add(10 * time.Second))
	env, err := protocol.Seal(n.id, protocol.CapabilityAnnounce, "", capabilityTTL, n.ownCapability())
	if err != nil {
		_ = s.Reset()
		return
	}
	_ = writeMsg(s, env)
}

// peerLost: Die letzte Verbindung zu einem Peer ist weg. Das ist oft nur
// kurz, deshalb bleibt seine Anzeige capabilityGrace lang gültig (ein Auftrag
// wählt ihn dann selbst neu an) und vertraute Peers werden neu angewählt
// (redialLost).
func (n *Node) peerLost(ctx context.Context, c network.Conn) {
	p := c.RemotePeer()
	if n.connected(p) {
		return
	}
	if ctx.Err() == nil && n.wanted(p) {
		slog.Info("verbindung zu peer getrennt", "peer", p, "adresse", c.RemoteMultiaddr(), "dauer", time.Since(c.Stat().Opened).Round(time.Second))
		go n.redialLost(ctx, p)
	}
	time.AfterFunc(capabilityGrace, func() {
		if !n.connected(p) {
			n.mu.Lock()
			delete(n.caps, p)
			n.mu.Unlock()
			if n.member != nil {
				n.member.forget(p)
			}
		}
	})
}

func (n *Node) fetchCapability(ctx context.Context, p peer.ID) error {
	if n.member != nil {
		if !n.admitted(p) {
			if err := n.admit(ctx, p); err != nil {
				return err
			}
		}
		if !n.Trusted(p) || !n.memberCanTask() {
			return errNoCapability
		}
	}
	ctx, cancel := context.WithTimeout(allowLimited(ctx), 10*time.Second)
	defer cancel()
	s, err := n.host.NewStream(ctx, p, lp2pproto.ID(protocol.ProtoCapability))
	if err != nil {
		return err
	}
	defer s.Close()
	_ = s.SetDeadline(time.Now().Add(10 * time.Second))
	env, err := readMsg(bufio.NewReader(s))
	if err != nil {
		return err
	}
	// Anzeigen werden periodisch neu geholt; ohne Replay-Cache, aber mit Absenderprüfung.
	if err := env.Verify(time.Now(), nil); err != nil || env.Type != protocol.CapabilityAnnounce || env.Sender != p.String() {
		return fmt.Errorf("ungültige fähigkeitsanzeige von %s: %v", p, err)
	}
	var c protocol.Capability
	if err := env.Decode(&c); err != nil || c.PeerID != p.String() {
		return fmt.Errorf("fähigkeitsanzeige von %s: %v", p, err)
	}
	n.mu.Lock()
	n.caps[p] = c
	n.mu.Unlock()
	return nil
}

// toolsRestricted: Im PSK-Netz gelten alle Werkzeuge, wenn keine Liste gesetzt
// ist, im Mitgliedsnetz immer nur die Liste. Im offenen Netz sind einzig
// trusted_peers ausgenommen.
func (n *Node) toolsRestricted(trusted bool) bool {
	switch {
	case n.member != nil:
		return true
	case n.private:
		return n.opts.AllowedTools != nil
	}
	return !trusted
}

// RemoteAgents liefert die gültigen Agenten anderer Knoten.
func (n *Node) RemoteAgents() []orchestrator.Agent {
	now := time.Now()
	n.mu.Lock()
	defer n.mu.Unlock()
	var out []orchestrator.Agent
	for p, c := range n.caps {
		if now.After(c.ExpiresAt) {
			delete(n.caps, p)
			continue
		}
		trusted := n.private || n.trusted[p]
		if n.member != nil {
			role, ok := n.member.role(p)
			if trusted = ok && canTask(role); !trusted {
				continue // ein helper bekommt nie Aufträge, auch keine öffentlichen
			}
		}
		if !trusted && !slices.Contains(c.Policy.Accepts, "public") {
			continue // nimmt von uns nichts an
		}
		for _, a := range c.Agents {
			out = append(out, orchestrator.Agent{
				ID: a.AgentID, PeerID: p.String(), Network: n.opts.NetworkID, Trusted: trusted,
				Family: a.ModelFamily, Skills: a.Skills, ContextMax: a.ContextMax, Strength: a.Strength, Tools: a.Tools,
				Vision: slices.Contains(a.Modalities, "image"),
				Busy:   c.Availability.SlotsNow == 0, Slots: c.Availability.SlotsNow,
				ToolsRestricted: n.toolsRestricted(trusted), ToolsAllow: n.opts.AllowedTools,
			})
		}
	}
	return out
}

type PeerInfo struct {
	PeerID  string               `json:"peer_id"`
	Network string               `json:"network"`
	Trusted bool                 `json:"trusted"`
	Agents  []protocol.AgentInfo `json:"agents"`
	Slots   int                  `json:"slots_now"`
	Accepts []string             `json:"accepts"`
	Addrs   []string             `json:"addrs"`
	Via     string               `json:"via"` // direct | relay
	Role    string               `json:"role,omitempty"`
}

func (n *Node) Peers() []PeerInfo {
	var out []PeerInfo
	for _, p := range n.host.Network().Peers() {
		info := PeerInfo{PeerID: p.String(), Network: n.opts.NetworkID, Trusted: n.Trusted(p), Via: "relay"}
		if n.host.Network().Connectedness(p) == network.Connected {
			info.Via = "direct"
		}
		if n.member != nil {
			info.Role, _ = n.member.role(p)
		}
		for _, a := range n.host.Peerstore().Addrs(p) {
			info.Addrs = append(info.Addrs, a.String())
		}
		n.mu.Lock()
		if c, ok := n.caps[p]; ok {
			info.Agents, info.Slots, info.Accepts = c.Agents, c.Availability.SlotsNow, c.Policy.Accepts
		}
		n.mu.Unlock()
		out = append(out, info)
	}
	return out
}

// --- Rahmung: eine JSON-Nachricht pro Zeile ---

func writeMsg(w io.Writer, env *protocol.Envelope) error {
	b, err := json.Marshal(env)
	if err != nil {
		return err
	}
	if len(b) > maxMsgBytes {
		return errors.New("nachricht zu groß")
	}
	_, err = w.Write(append(b, '\n'))
	return err
}

func readMsg(r *bufio.Reader) (*protocol.Envelope, error) {
	var buf []byte
	for {
		chunk, isPrefix, err := r.ReadLine()
		if err != nil {
			return nil, err
		}
		buf = append(buf, chunk...)
		if len(buf) > maxMsgBytes {
			return nil, errors.New("nachricht zu groß")
		}
		if !isPrefix {
			break
		}
	}
	var env protocol.Envelope
	if err := json.Unmarshal(buf, &env); err != nil {
		return nil, err
	}
	return &env, nil
}
