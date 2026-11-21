// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Rainer Sestig. Kommerzielle Lizenz: siehe COMMERCIAL.md.

package node

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/rainer-sharp/Ai-Swarms.net/internal/config"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/identity"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/membership"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/orchestrator"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/p2p"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/profile"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/protocol"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/receipt"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/rem"
)

type Node struct {
	Cfg   config.Config
	ID    *identity.Identity
	REM   *rem.Manager
	Local *Local
	// P2P ist das erste Netz, Nets alle Netze (Nets[0] == P2P), je mit
	// eigenem Host (spec/p2p-layer.md § 9).
	P2P      *p2p.Node
	Nets     []*p2p.Node
	Receipts *receipt.Store
	// Profile: Kompetenzprofil aus eigenen Beobachtungen (spec/cooperation.md § 2).
	Profile *profile.Profile
	Orch    *orchestrator.Orchestrator
	started time.Time
	// NetErrors: Netze, die wegen ihres Zertifikats nicht gestartet sind.
	NetErrors []string
}

// Options erlaubt Tests, Sensoren und Energiequelle zu ersetzen.
type Options struct {
	Identity *identity.Identity // nur für das erste Netz
	HW       rem.HWSensor
	Energy   rem.EnergySource
}

func New(ctx context.Context, cfg config.Config, o Options) (*Node, error) {
	dir := cfg.Node.DataDir
	nets := cfg.AllNetworks()
	id := o.Identity
	if id == nil {
		var err error
		if id, err = identity.LoadOrCreate(nets[0].IdentityFile); err != nil {
			return nil, err
		}
	}
	hw := o.HW
	if hw == nil {
		hw = rem.SystemSensor{}
	}
	energy := o.Energy
	if energy == nil && cfg.Resources.Energy.Source == "home_assistant" {
		energy = &rem.HomeAssistantSource{Cfg: cfg.Resources.Energy.HomeAssistant, Client: &http.Client{Timeout: 10 * time.Second}}
	}
	// accept_public_tasks gilt je Netz und wird in der P2P-Schicht geprüft;
	// der Ressourcenmanager lässt öffentliche Aufträge zu, wenn ein Netz sie annimmt.
	res := cfg.Resources
	res.AcceptPublicTasks = slices.ContainsFunc(nets, func(s config.NetworkSettings) bool { return s.AcceptPublic })
	m, err := rem.New(res, hw, energy, rem.LoadUsage(filepath.Join(dir, "gpu-usage.json")))
	if err != nil {
		return nil, err
	}
	go m.Run(ctx)

	store, err := receipt.OpenStore(filepath.Join(dir, "receipts.jsonl"))
	if err != nil {
		return nil, err
	}
	backends := cfg.Backends
	if cfg.Node.Helper {
		backends = nil // Helferknoten rechnen nicht
	}
	local := NewLocal(backends, m)
	n := &Node{Cfg: cfg, ID: id, REM: m, Local: local, Receipts: store, started: time.Now(),
		Profile: profile.Open(filepath.Join(dir, "profile.json"))}
	// Ressourcenmanager, Backends und Quittungen teilen sich alle Netze;
	// fremde Aufträge aus allen Netzen konkurrieren um dieselben Slots.
	for i, s := range nets {
		nid := id
		if i > 0 && s.IdentityFile != nets[0].IdentityFile {
			if nid, err = identity.LoadOrCreate(s.IdentityFile); err != nil {
				n.Close()
				return nil, err
			}
		}
		mem, err := loadMembership(s, nid, dir)
		if err != nil {
			err = fmt.Errorf("netz %s: %w", s.NetworkID, err)
			if i == 0 {
				n.Close()
				return nil, err
			}
			// Ein Mitgliedsnetz ohne gültiges Zertifikat läuft nicht, die anderen schon.
			slog.Error("netz nicht gestartet", "err", err)
			n.NetErrors = append(n.NetErrors, err.Error())
			continue
		}
		pn, err := p2p.New(ctx, p2p.Options{
			Membership: mem, AllowedTools: s.AllowedTools,
			Identity: nid, Listen: s.Listen, NetworkID: s.NetworkID,
			PSKFile: s.NetworkKeyFile, MDNS: s.MDNS,
			StaticPeers: s.StaticPeers, Bootstrap: s.Bootstrap, Trackers: s.Trackers, TrustedPeers: s.TrustedPeers,
			Provide: s.Provide && !cfg.Node.Helper, AcceptsPublic: s.AcceptPublic,
			RelayService: relayMode(cfg.Node.Helper, s.RelayService), PEX: s.PEX,
			RelayLimits: p2p.RelayLimits(s.RelayLimits), DNSSeeds: s.DNSSeeds,
			PeerCache: filepath.Join(dir, "peers-"+s.NetworkID+".json"),
			DHTServer: dhtServer(cfg.Node.Helper, s.DHTServer), AnnouncePrivate: s.AnnouncePrivate, Announce: s.Announce, PortMapping: s.PortMapping,
			Provider: local, Receipts: store,
		})
		if err != nil {
			n.Close()
			return nil, fmt.Errorf("netz %s: %w", s.NetworkID, err)
		}
		n.Nets = append(n.Nets, pn)
	}
	n.P2P = n.Nets[0]
	n.Orch = &orchestrator.Orchestrator{Dir: n, Exec: n, Parallel: 3,
		Default: orchestrator.Pattern(cfg.Tasks.Pattern), Rate: n.rate}
	if cfg.Cooperation.Selection == "bandit" {
		n.Orch.Choose = n.choose
	}
	return n, nil
}

func (n *Node) Close() error {
	var errs []error
	for _, pn := range n.Nets {
		errs = append(errs, pn.Close())
	}
	return errors.Join(errs...)
}

// relayMode: Helferknoten bieten immer Relay an (spec/p2p-layer.md § 6.1).
func relayMode(helper bool, mode string) string {
	if helper {
		return p2p.RelayOn
	}
	return mode
}

// dhtServer: Helferknoten sind immer DHT-Server (spec/p2p-layer.md § 6.1).
func dhtServer(helper bool, mode string) string {
	if helper && mode == "auto" {
		return "true"
	}
	return mode
}

// net liefert den Knoten eines Netzes; unbekannt: das erste.
func (n *Node) net(id string) *p2p.Node {
	for _, pn := range n.Nets {
		if pn.NetworkID() == id {
			return pn
		}
	}
	return n.P2P
}

// Agents implementiert orchestrator.Directory: eigene und entfernte Agenten
// aller Netze. Welche davon ein Auftrag nutzen darf, entscheidet die
// Freigabeklasse über Trusted (vertraut nur im privaten Netz oder als
// trusted_peer).
func (n *Node) Agents() []orchestrator.Agent {
	out := n.Local.Agents()
	for _, pn := range n.Nets {
		out = append(out, pn.RemoteAgents()...)
	}
	return out
}

// Execute implementiert orchestrator.Executor. Eigene Inferenz zählt als
// eigene Nutzung und hat Vorrang vor fremden Aufträgen.
func (n *Node) Execute(ctx context.Context, a orchestrator.Agent, t orchestrator.Task) (protocol.Result, error) {
	if a.Local {
		done := n.REM.BeginOwnUse()
		defer done()
		return n.Local.RunLocal(ctx, a.ID, t)
	}
	// Teilaufträge gehen nur über das Netz, in dem der Agent angeboten wird.
	return n.net(a.Network).Execute(ctx, a, t)
}

// Ask bearbeitet einen eigenen Auftrag mit den Standardgrenzen aus der Konfiguration.
func (n *Node) Ask(ctx context.Context, req orchestrator.Request) (*orchestrator.Answer, error) {
	a := n.Cfg.Tasks // ohne Muster wählt der Orchestrator (Regel oder Bandit)
	if req.Privacy == "" {
		req.Privacy = protocol.PrivacyClass(a.PrivacyClass)
	}
	if req.Limits.MaxTokens == 0 {
		req.Limits.MaxTokens = a.MaxTokens
	}
	if req.Limits.Deadline.IsZero() && a.DeadlineS > 0 {
		req.Limits.Deadline = time.Now().Add(time.Duration(a.DeadlineS) * time.Second)
	}
	if req.Limits.MaxSubtasks == 0 {
		req.Limits.MaxSubtasks = a.MaxSubtasks
	}
	req.Limits.MaxDepth = 1 // Phase 1: keine Weiterdelegation
	if req.Privacy == protocol.PrivacyPublic {
		// Im offenen Netz fehlende Anbieter über die DHT suchen.
		skill := req.Skill
		if skill == "" {
			skill = orchestrator.Route(req.Messages)
		}
		for _, pn := range n.Nets {
			pn.Discover(ctx, skill) // nur offene Netze suchen
		}
	}
	ans, err := n.Orch.Run(ctx, req)
	if errors.Is(err, orchestrator.ErrNoAgent) || errors.Is(err, orchestrator.ErrToolsNotAllowed) {
		slog.Warn("kein agent wählbar", "bekannt", len(n.Agents()), "freigabe", req.Privacy, "werkzeuge", len(req.Tools) > 0, "beschränkt_auf", req.Agents)
	}
	if err == nil {
		n.recordProfile(ans)
	}
	if ans != nil {
		slog.Info("auftrag", "task", ans.TaskID, "muster", ans.Pattern, "skill", ans.Skill, "urteil", ans.Verdict, "schritte", len(ans.Steps),
			"rollen", roles(req.Messages), "werkzeuge", len(req.Tools) > 0, "antwortete", answeredBy(ans.Steps), "err", err)
	}
	return ans, err
}

type Status struct {
	NetErrors []string                   `json:"network_errors,omitempty"`
	PeerID    string                     `json:"peer_id"`
	Addrs     []string                   `json:"addrs"`
	Private   bool                       `json:"private_network"`
	Relay     bool                       `json:"relay_service"`
	Relays    []string                   `json:"relay_reservations"`
	Hints     []string                   `json:"hints,omitempty"`
	DHT       *p2p.DHTInfo               `json:"dht,omitempty"`
	Uptime    string                     `json:"uptime"`
	Provide   bool                       `json:"provide"`
	Local     []orchestrator.Agent       `json:"local_agents"`
	Offered   []protocol.AgentInfo       `json:"offered_agents"`
	Peers     []p2p.PeerInfo             `json:"peers"` // aller Netze
	Networks  []NetStatus                `json:"networks"`
	REM       rem.Status                 `json:"resources"`
	Receipts  int                        `json:"receipts"`
	Ledger    map[string]receipt.Summary `json:"ledger"`
}

// NetStatus beschreibt ein Netz; die Felder oben in Status sind die des
// ersten Netzes.
type NetStatus struct {
	NetworkID string   `json:"network_id"`
	PeerID    string   `json:"peer_id"`
	Addrs     []string `json:"addrs"`
	Private   bool     `json:"private_network"`
	Reach     string   `json:"reachability,omitempty"` // offenes Netz: public | private | unknown
	// ReachFam: je Adressfamilie (ip4, ip6), z. B. hinter DS-Lite ip4 private, ip6 public
	ReachFam map[string]string `json:"reachability_by_family,omitempty"`
	Relay    bool              `json:"relay_service"`
	Relays   []string          `json:"relay_reservations"`
	Hints    []string          `json:"hints,omitempty"`
	DHT      *p2p.DHTInfo      `json:"dht,omitempty"`
	Mapping  *p2p.MappingInfo  `json:"port_mapping,omitempty"`
	Peers    int               `json:"peers"`
	Member   *p2p.MemberInfo   `json:"membership,omitempty"`
}

func netStatus(pn *p2p.Node) NetStatus {
	return NetStatus{
		NetworkID: pn.NetworkID(), PeerID: pn.ID().String(), Addrs: pn.Addrs(),
		Private: pn.Private(), Reach: pn.Reachability(), ReachFam: pn.FamilyReachability(), Relay: pn.RelayService(), Relays: pn.Reservations(),
		Hints: pn.Hints(), DHT: pn.DHTInfo(), Mapping: pn.MappingInfo(), Peers: len(pn.Peers()), Member: pn.MemberInfo(),
	}
}

func (n *Node) Status() Status {
	first := netStatus(n.P2P)
	st := Status{
		NetErrors: n.NetErrors,
		PeerID:    first.PeerID, Addrs: first.Addrs,
		Private: first.Private, Relay: first.Relay, Relays: first.Relays,
		Hints: first.Hints, DHT: first.DHT,
		Uptime:  time.Since(n.started).Truncate(time.Second).String(),
		Provide: n.Cfg.Node.Provide,
		Local:   n.Local.Agents(), Offered: n.Local.Offer(),
		REM:      n.REM.Status(),
		Receipts: n.Receipts.Count(), Ledger: map[string]receipt.Summary{},
	}
	for _, pn := range n.Nets {
		st.Networks = append(st.Networks, netStatus(pn))
		st.Peers = append(st.Peers, pn.Peers()...)
		maps.Copy(st.Ledger, n.Receipts.Summary(pn.ID().String()))
	}
	return st
}

// ProbeBackends protokolliert, welche Modelle erreichbar sind.
func (n *Node) ProbeBackends(ctx context.Context) {
	for id, err := range n.Local.Probe(ctx) {
		if err != nil {
			slog.Warn("lokaler agent nicht verfügbar", "agent", id, "err", err)
		} else {
			slog.Info("lokaler agent bereit", "agent", id)
		}
	}
	if len(n.Local.order) == 0 && !n.Cfg.Node.Helper {
		slog.Warn(fmt.Sprintf("keine modelle konfiguriert; bitte backends[].models in der konfiguration ergänzen"))
	}
}

// Bedienfunktionen für das lokale Gateway.

func (n *Node) StatusJSON() any { return n.Status() }
func (n *Node) Pause()          { n.REM.Pause() }
func (n *Node) Resume()         { n.REM.Resume() }

// Stop ist der Sofort-Stopp: laufende Fremdjobs abbrechen, keine Annahmen mehr.
func (n *Node) Stop() { n.REM.EmergencyStop() }

func (n *Node) Connect(ctx context.Context, addr string) error { return n.P2P.Connect(ctx, addr) }

// Ping prüft den Weg zu einem Peer (Peer-ID oder Multiadresse) im Netz
// network; leer: im Netz, das den Peer kennt, sonst im ersten.
func (n *Node) Ping(ctx context.Context, target, network string) (any, error) {
	pn := n.P2P
	if network != "" {
		if pn = n.net(network); pn.NetworkID() != network {
			return nil, fmt.Errorf("netz %q nicht konfiguriert", network)
		}
	} else {
		for _, x := range n.Nets {
			if x.Knows(target) {
				pn = x
				break
			}
		}
	}
	return pn.Ping(ctx, target)
}

// roles: Reihenfolge der Rollen eines Auftrags fürs Protokoll, nie Inhalte.
func roles(msgs []protocol.ChatMessage) string {
	out := make([]string, len(msgs))
	for i, m := range msgs {
		out[i] = m.Role
	}
	return strings.Join(out, ",")
}

// answeredBy: Familie des Agenten im letzten Schritt.
func answeredBy(steps []orchestrator.Step) string {
	if len(steps) == 0 {
		return ""
	}
	return steps[len(steps)-1].Family
}

// loadMembership liest das Zertifikat eines Mitgliedsnetzes. Vertraut wird dem
// Wurzelschlüssel aus der Konfiguration, nicht dem in der Datei.
func loadMembership(s config.NetworkSettings, id *identity.Identity, dataDir string) (*p2p.Membership, error) {
	if s.Membership == nil {
		return nil, nil
	}
	b, err := membership.ReadBundle(s.Membership.CertFile)
	if err != nil {
		return nil, fmt.Errorf("zertifikat: %w", err)
	}
	if b.Root != s.Membership.Root {
		return nil, fmt.Errorf("zertifikat %s gehört zu einem anderen wurzelschlüssel", s.Membership.CertFile)
	}
	root, err := membership.DecodeRoot(s.Membership.Root)
	if err != nil {
		return nil, err
	}
	net, err := membership.NewNetwork(s.NetworkID, root)
	if err != nil {
		return nil, err
	}
	store := membership.CRLStoreFile(dataDir, s.NetworkID)
	if err := net.LoadCRLs(store, time.Now()); err != nil {
		return nil, fmt.Errorf("sperrlisten: %w", err)
	}
	if _, err := net.Admit(b.Chain, id.ID.String(), time.Now()); err != nil {
		return nil, fmt.Errorf("eigenes zertifikat: %w", err)
	}
	mem := &p2p.Membership{Network: net, Chain: b.Chain, Root: s.Membership.Root, CertFile: s.Membership.CertFile,
		CRLStore: store, CRLInbox: membership.CRLInbox(dataDir, s.NetworkID)}
	var redeem sync.Mutex
	mem.Redeem = func(token, subject string) ([]membership.Cert, error) {
		redeem.Lock()
		defer redeem.Unlock()
		// Das Buch wird je Vorgang neu gelesen, weil `member invite` es schreibt.
		admin, err := membership.OpenAdmin(dataDir, s.NetworkID, "", time.Now())
		if err != nil {
			return nil, err
		}
		return admin.Redeem(token, subject, time.Now())
	}
	mem.Renew = func(subject, role, name string) ([]membership.Cert, error) {
		redeem.Lock()
		defer redeem.Unlock()
		admin, err := membership.OpenAdmin(dataDir, s.NetworkID, "", time.Now())
		if err != nil {
			return nil, err
		}
		return admin.Renew(subject, role, name, time.Now())
	}
	return mem, nil
}
