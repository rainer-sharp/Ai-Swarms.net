// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Rainer Sestig und Mitwirkende.

// Package config liest die Knotenkonfiguration (YAML).
package config

import (
	"bytes"
	"cmp"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	ma "github.com/multiformats/go-multiaddr"
	"gopkg.in/yaml.v3"

	"github.com/rainer-sharp/Ai-Swarms.net/internal/membership"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/tracker"
)

type Config struct {
	Node        Node        `yaml:"node"`
	Backends    []Backend   `yaml:"backends"`
	Resources   Resources   `yaml:"resources"`
	Tasks       Tasks       `yaml:"tasks"`
	Cooperation Cooperation `yaml:"cooperation"`
}

// Cooperation steuert die Auswahl des Musters (spec/cooperation.md § 3).
type Cooperation struct {
	// Selection: rules (tasks.pattern, bisheriges Verhalten) oder bandit
	// (Thompson Sampling über Muster nach Kompetenzprofil). Eine Anfrage, die
	// ein Muster nennt (swarmainet/<muster>), bekommt es immer.
	Selection string `yaml:"selection"`
	// Exploration: Anteil zufälliger Wahl unter den besetzbaren Mustern.
	Exploration float64 `yaml:"exploration"`
	// Targets: Sollwerte je Aufgabenkategorie (code, math, general …).
	Targets map[string]Target `yaml:"targets,omitempty"`
	// CoordinateForOthers: asynchrone Aufträge vertrauter Peers annehmen und
	// koordinieren (spec/cooperation.md § 7). Der Koordinator sieht den
	// Auftrag im Klartext.
	CoordinateForOthers bool `yaml:"coordinate_for_others"`
}

// Target: Sollwerte einer Aufgabenkategorie (spec/cooperation.md § 4.1).
type Target struct {
	Quality float64 `yaml:"quality"` // angestrebte Erfolgsquote, z. B. 0.9
	// LatencyP90S: p90-Antwortzeit; erst mit dem Latenz-Regler wirksam.
	LatencyP90S int `yaml:"latency_p90_s,omitempty"`
	// CascadeError: zulässige Fehlerquote der günstigen Stufe der Kaskade
	// (Regler für θ); 0: Standard 0,05.
	CascadeError float64 `yaml:"cascade_error,omitempty"`
}

type Node struct {
	DataDir   string   `yaml:"data_dir"`
	Listen    []string `yaml:"listen"`
	Gateway   string   `yaml:"gateway"` // nur localhost
	NetworkID string   `yaml:"network_id"`
	// NetworkKeyFile aktiviert einen privaten Schwarm (libp2p PSK).
	// Alle Teilnehmer gelten dann als vertrauter Kreis.
	NetworkKeyFile string `yaml:"network_key_file"`
	// Membership macht das erste Netz zu einem Mitgliedsnetz.
	Membership *Membership `yaml:"membership,omitempty"`
	// AllowedTools: Werkzeuge, die Modelle dieses Netzes sehen dürfen
	// (spec/privacy-classes.md). Fehlt die Liste, gilt im PSK-Netz: alle, im
	// Mitgliedsnetz und im offenen Netz: keine.
	AllowedTools []string `yaml:"allowed_tools,omitempty"`
	MDNS         bool     `yaml:"mdns"`
	StaticPeers  []string `yaml:"static_peers"`
	// Bootstrap: Einstiegsadressen (z. B. aus einer Einladung); angewählt,
	// solange der Knoten keine Peers, keine DHT-Partner oder hinter NAT keinen
	// Relay-Platz hat (spec/p2p-layer.md § 2). static_peers halten dagegen dauerhaft
	// Verbindung.
	Bootstrap []string `yaml:"bootstrap"`
	// Trackers: HTTPS-Adressbriefkästen (swarmainet tracker), bei denen der
	// Knoten seine Adressen hinterlegt und die anderer abholt.
	Trackers     []string `yaml:"trackers"`
	TrustedPeers []string `yaml:"trusted_peers"`
	// Provide: eigene Agenten im Schwarm anbieten (Capability-Anzeige).
	Provide bool `yaml:"provide"`
	// RelayService: Verbindungen anderer Knoten weiterleiten.
	// auto: im offenen Netz, sobald der Knoten öffentlich erreichbar ist (mit
	// sparsamen Grenzen), im privaten Schwarm aus. true: immer, auch ohne
	// öffentliche Erreichbarkeit (Helfer, Brückenknoten). false: nie.
	RelayService string `yaml:"relay_service"`
	// RelayLimits: Grenzen des Relays; 0 = Vorgabe (spec/p2p-layer.md § 6).
	RelayLimits RelayLimits `yaml:"relay_limits,omitempty"`
	// DNSSeeds: Domains mit _dnsaddr-TXT-Einträgen für den Einstieg (wann:
	// wie Bootstrap). Ohne Angabe gilt im offenen Netz DefaultDNSSeeds;
	// [none] schaltet die Seeds ab. Gilt auch für die Netze unter networks,
	// soweit sie keine eigenen angeben; Mitgliedsnetze nutzen keine.
	DNSSeeds []string `yaml:"dns_seeds,omitempty"`
	// PEX: mit verbundenen Knoten bekannte Peers austauschen.
	PEX bool `yaml:"pex"`
	// Helper: nur das Netz tragen (Relay, DHT-Server), ohne eigene Modelle.
	Helper bool `yaml:"helper"`
	// DHTServer: auto | true | false. auto: im offenen Netz bei öffentlicher
	// Erreichbarkeit, im privaten bei eigenen Listen-Adressen oder als Relay.
	DHTServer string `yaml:"dht_server"`
	// AnnouncePrivate: private Adressen (LAN, Loopback) auch im offenen Netz
	// veröffentlichen. Nur für Testnetze im LAN; sonst wären interne
	// Adressen öffentlich sichtbar.
	AnnouncePrivate bool `yaml:"announce_private,omitempty"`
	// Announce: zusätzlich bekannt gegebene Adressen ohne /p2p, etwa
	// /dns4/swarm.example.de/tcp/4711 für einen Knoten hinter einer
	// Portfreigabe, der seine öffentliche Adresse selbst nicht sieht.
	Announce []string `yaml:"announce,omitempty"`
	// PortMapping: beim Router per UPnP oder NAT-PMP eine Portfreigabe für
	// die Listen-Ports anfordern (Opt-in, ändert Router-Einstellungen).
	PortMapping bool `yaml:"port_mapping,omitempty"`
	// Networks: weitere Netze neben dem oben beschriebenen ersten, je mit
	// eigenem Host und Port (spec/p2p-layer.md § 9).
	Networks []Network `yaml:"networks,omitempty"`
}

// Network ist ein weiteres Netz. Felder ohne Angabe übernehmen die Werte des
// ersten Netzes (node bzw. resources.accept_public_tasks); listen ist Pflicht,
// weil jedes Netz einen eigenen Port braucht.
type Network struct {
	NetworkID      string   `yaml:"network_id"`
	NetworkKeyFile string   `yaml:"network_key_file,omitempty"`
	AllowedTools   []string `yaml:"allowed_tools,omitempty"`
	// Membership macht es zu einem Mitgliedsnetz (spec/membership.md).
	Membership   *Membership `yaml:"membership,omitempty"`
	Listen       []string    `yaml:"listen"`
	Bootstrap    []string    `yaml:"bootstrap,omitempty"`
	Trackers     []string    `yaml:"trackers,omitempty"`
	StaticPeers  []string    `yaml:"static_peers,omitempty"`
	TrustedPeers []string    `yaml:"trusted_peers,omitempty"`
	// Identity: separate (Standard, eigener Schlüssel für dieses Netz) oder
	// shared (Peer-ID des ersten Netzes; macht die Teilnahme verknüpfbar).
	Identity          string   `yaml:"identity,omitempty"`
	Provide           *bool    `yaml:"provide,omitempty"`
	AcceptPublicTasks *bool    `yaml:"accept_public_tasks,omitempty"`
	MDNS              *bool    `yaml:"mdns,omitempty"`
	PEX               *bool    `yaml:"pex,omitempty"`
	RelayService      string   `yaml:"relay_service,omitempty"`
	DNSSeeds          []string `yaml:"dns_seeds,omitempty"`
	DHTServer         string   `yaml:"dht_server,omitempty"`
	AnnouncePrivate   *bool    `yaml:"announce_private,omitempty"`
	Announce          []string `yaml:"announce,omitempty"`
	PortMapping       *bool    `yaml:"port_mapping,omitempty"`
}

// Membership: Zugang zu einem Mitgliedsnetz. Root ist der Wurzelschlüssel aus
// sicherer Quelle; CertFile fehlt, wenn die Datei am Standardort liegt.
type Membership struct {
	Root     string `yaml:"root"`
	CertFile string `yaml:"cert_file,omitempty"`
}

// NetworkSettings ist ein Netz mit allen Werten aufgelöst.
type NetworkSettings struct {
	NetworkID       string
	NetworkKeyFile  string
	Membership      *Membership // CertFile aufgelöst
	AllowedTools    []string
	Listen          []string
	Bootstrap       []string
	Trackers        []string
	StaticPeers     []string
	TrustedPeers    []string
	IdentityFile    string // Schlüsseldatei der Peer-ID
	Provide         bool
	AcceptPublic    bool
	MDNS            bool
	PEX             bool
	RelayService    string // auto | true | false
	RelayLimits     RelayLimits
	DNSSeeds        []string // aufgelöst: Vorgabe im offenen Netz, ohne "none"
	DHTServer       string
	AnnouncePrivate bool
	Announce        []string
	PortMapping     bool
}

// AllNetworks liefert das erste Netz (aus node) und die weiteren, aufgelöst.
func (c Config) AllNetworks() []NetworkSettings {
	n := c.Node
	first := NetworkSettings{
		NetworkID: n.NetworkID, NetworkKeyFile: n.NetworkKeyFile, Listen: n.Listen,
		Bootstrap: n.Bootstrap, Trackers: n.Trackers, StaticPeers: n.StaticPeers, TrustedPeers: n.TrustedPeers,
		IdentityFile: filepath.Join(n.DataDir, "identity.key"),
		Provide:      n.Provide, AcceptPublic: c.Resources.AcceptPublicTasks,
		MDNS: n.MDNS, PEX: n.PEX, RelayService: n.RelayService, RelayLimits: n.RelayLimits, DHTServer: n.DHTServer,
		DNSSeeds:        seeds(n.DNSSeeds, n.NetworkKeyFile == "" && n.Membership == nil),
		AnnouncePrivate: n.AnnouncePrivate, Announce: n.Announce, PortMapping: n.PortMapping,
		Membership:   resolveMembership(n.Membership, n.DataDir, n.NetworkID),
		AllowedTools: n.AllowedTools,
	}
	if n.Membership != nil {
		first.IdentityFile = filepath.Join(n.DataDir, "identity-"+n.NetworkID+".key") // wie member request
	}
	out := []NetworkSettings{first}
	or := func(p *bool, d bool) bool {
		if p != nil {
			return *p
		}
		return d
	}
	for _, x := range n.Networks {
		// dns_seeds gelten ohne eigene Angabe für alle Netze; ein Mitgliedsnetz
		// fragt nie einen Seed (spec/membership.md).
		seedList := x.DNSSeeds
		if seedList == nil && x.Membership == nil {
			seedList = n.DNSSeeds
		}
		s := NetworkSettings{
			NetworkID: x.NetworkID, NetworkKeyFile: expandHome(x.NetworkKeyFile), Listen: x.Listen,
			Bootstrap: x.Bootstrap, Trackers: x.Trackers, StaticPeers: x.StaticPeers, TrustedPeers: x.TrustedPeers,
			IdentityFile: filepath.Join(n.DataDir, "identity-"+x.NetworkID+".key"),
			Provide:      or(x.Provide, first.Provide), AcceptPublic: or(x.AcceptPublicTasks, first.AcceptPublic),
			MDNS: or(x.MDNS, first.MDNS), PEX: or(x.PEX, first.PEX), RelayService: cmp.Or(x.RelayService, first.RelayService),
			RelayLimits: first.RelayLimits, DNSSeeds: seeds(seedList, x.NetworkKeyFile == "" && x.Membership == nil),
			Membership:      resolveMembership(x.Membership, n.DataDir, x.NetworkID),
			AllowedTools:    x.AllowedTools,
			DHTServer:       cmp.Or(x.DHTServer, first.DHTServer),
			AnnouncePrivate: or(x.AnnouncePrivate, first.AnnouncePrivate),
			Announce:        x.Announce, // je Netz eigener Port, daher nicht übernommen
			PortMapping:     or(x.PortMapping, first.PortMapping),
		}
		if x.Identity == "shared" {
			s.IdentityFile = first.IdentityFile
		}
		out = append(out, s)
	}
	return out
}

func resolveMembership(m *Membership, dataDir, networkID string) *Membership {
	if m == nil {
		return nil
	}
	r := *m
	r.CertFile = expandHome(r.CertFile)
	if r.CertFile == "" {
		r.CertFile = filepath.Join(dataDir, "member-"+networkID+".cert")
	}
	return &r
}

// RelayLimits begrenzt einen Relay-Dienst; 0 übernimmt die Vorgabe.
type RelayLimits struct {
	Reservations    int `yaml:"reservations,omitempty"`      // gleichzeitige Reservierungen
	CircuitsPerPeer int `yaml:"circuits_per_peer,omitempty"` // Relay-Verbindungen je Peer
	DataMiB         int `yaml:"data_mib,omitempty"`          // Daten je Relay-Verbindung und Richtung
	DurationS       int `yaml:"duration_s,omitempty"`        // Dauer einer Relay-Verbindung
}

// DefaultDNSSeeds: eingebaute Einstiegs-Domains des offenen Netzes
// (spec/p2p-layer.md § 2). Sie dienen nur dem ersten Kontakt; danach
// tragen Peer-Cache, Peer-Austausch und DHT.
var DefaultDNSSeeds = []string{"seed.ai-swarms.net"}

// seeds löst die DNS-Seeds auf: ohne Angabe im offenen Netz die Vorgabe,
// im privaten keine; "none" schaltet ab.
func seeds(list []string, open bool) []string {
	if slices.Contains(list, "none") {
		return nil
	}
	if list == nil && open {
		return slices.Clone(DefaultDNSSeeds)
	}
	return list
}

// validateMembership: Ein Mitgliedsnetz schließt alles aus, was fremde
// Infrastruktur einbezieht oder das Vertrauen an den Zertifikaten vorbei regelt.
func validateMembership(n NetworkSettings, where string) error {
	m := n.Membership
	if m == nil {
		if strings.HasPrefix(n.NetworkID, "m-") {
			return fmt.Errorf("%s.network_id %q: das präfix m- ist mitgliedsnetzen vorbehalten", where, n.NetworkID)
		}
		return nil
	}
	root, err := membership.DecodeRoot(m.Root)
	if err != nil {
		return fmt.Errorf("%s.membership.root: %w", where, err)
	}
	if want := membership.NetworkID(root); want != n.NetworkID {
		return fmt.Errorf("%s.network_id %q passt nicht zum wurzelschlüssel (%s)", where, n.NetworkID, want)
	}
	switch {
	case n.NetworkKeyFile != "":
		return fmt.Errorf("%s: membership und network_key_file schließen einander aus", where)
	case len(n.Trackers) > 0:
		return fmt.Errorf("%s: trackers sind in einem mitgliedsnetz noch nicht vorgesehen", where)
	case len(n.TrustedPeers) > 0:
		return fmt.Errorf("%s: trusted_peers gelten in einem mitgliedsnetz nicht, vertraut wird nach zertifikat", where)
	case len(n.DNSSeeds) > 0:
		return fmt.Errorf("%s: dns_seeds sind in einem mitgliedsnetz nicht vorgesehen", where)
	}
	return nil
}

// hostnameRe: Domain eines DNS-Seeds.
var hostnameRe = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$`)

// NetworkIDRe: zulässige Netz-Kennungen (spec/p2p-layer.md § 1).
var NetworkIDRe = regexp.MustCompile(`^[a-z0-9-]{1,32}$`)

type Backend struct {
	Name   string  `yaml:"name"`
	URL    string  `yaml:"url"` // OpenAI-kompatibel, z. B. http://127.0.0.1:11434/v1
	APIKey string  `yaml:"api_key"`
	Models []Model `yaml:"models"`
}

type Model struct {
	Name    string   `yaml:"name"`
	Family  string   `yaml:"family"`
	Skills  []string `yaml:"skills"`
	Context int      `yaml:"context"`
	// Strength: eigene Einschätzung der Modellstärke, 1 (schwach) bis 10
	// (stark), 0 = unbekannt. Entscheidet bei gleicher Eignung über den Prüfer.
	Strength int `yaml:"strength"`
	// Offer: darf dieses Modell für fremde Aufträge genutzt werden?
	Offer bool `yaml:"offer"`
	// Tools: Das Modell beherrscht Werkzeug-Aufrufe (OpenAI tools); nur
	// solche Modelle bekommen Aufträge von Coding-Agenten mit Werkzeugen.
	Tools bool `yaml:"tools,omitempty"`
	// Vision: Das Modell versteht Bilder; nur solche Modelle bekommen
	// Anfragen mit Bildern.
	Vision bool `yaml:"vision,omitempty"`
}

type Resources struct {
	PrioritizeOwnUse  bool     `yaml:"prioritize_own_use"`
	AcceptPublicTasks bool     `yaml:"accept_public_tasks"`
	Hardware          Hardware `yaml:"hardware"`
	Schedule          Schedule `yaml:"schedule"`
	Energy            Energy   `yaml:"energy"`
}

type Hardware struct {
	MaxParallelJobs  int     `yaml:"max_parallel_jobs"`
	GPUUtilMeanMax   float64 `yaml:"gpu_util_mean_max"`
	GPUWindowS       int     `yaml:"gpu_window_s"`
	RAMReserveGiB    float64 `yaml:"ram_reserve_gib"`
	VRAMBudgetGiB    float64 `yaml:"vram_budget_gib"`
	ExtraPowerMaxW   float64 `yaml:"extra_power_max_w"`
	TemperatureStopC float64 `yaml:"temperature_stop_c"`
}

type Schedule struct {
	Windows          []TimeWindow `yaml:"windows"`
	GPUMinutesPerDay float64      `yaml:"gpu_minutes_per_day"`
}

type TimeWindow struct {
	Days []string `yaml:"days"`
	From string   `yaml:"from"`
	To   string   `yaml:"to"`
}

type Energy struct {
	Mode           string        `yaml:"mode"`   // pv_preferred | pv_only | price_only | off
	Source         string        `yaml:"source"` // home_assistant | static
	HomeAssistant  HomeAssistant `yaml:"home_assistant"`
	ReserveW       float64       `yaml:"reserve_w"`
	StartAboveW    float64       `yaml:"start_above_w"`
	StartStableS   int           `yaml:"start_stable_s"`
	StopBelowW     float64       `yaml:"stop_below_w"`
	StopStableS    int           `yaml:"stop_stable_s"`
	BatterySocMin  float64       `yaml:"battery_soc_min"`
	PriceMaxEurKWh float64       `yaml:"price_max_eur_kwh"`
	StaleReadings  string        `yaml:"stale_readings"` // no_new_jobs | ignore
	ReadingMaxAgeS int           `yaml:"reading_max_age_s"`
}

type HomeAssistant struct {
	URL        string `yaml:"url"`
	TokenEnv   string `yaml:"token_env"`
	GridExport string `yaml:"grid_export"` // Entity, Watt, positiv = Einspeisung
	BatterySoc string `yaml:"battery_soc"`
	Price      string `yaml:"price"`
}

type Tasks struct {
	// Standardmuster: single | parallel | draft_verify_synthesize | parallel_verify | cascade
	Pattern      string `yaml:"pattern"`
	PrivacyClass string `yaml:"privacy_class"`
	MaxTokens    int    `yaml:"max_tokens"`
	DeadlineS    int    `yaml:"deadline_s"`
	// JobDeadlineS: Frist asynchroner Aufträge (POST /v1/jobs); 0: eine Stunde.
	JobDeadlineS int `yaml:"job_deadline_s"`
	MaxSubtasks  int `yaml:"max_subtasks"`
	// PseudonymizeTerms: eigene Begriffe (Namen, Projekte, Kunden), die bei
	// privacy_class reduced für fremde Knoten immer ersetzt werden, zusätzlich
	// zu dem, was die Erkennung findet (spec/privacy-classes.md).
	PseudonymizeTerms []string `yaml:"pseudonymize_terms"`
}

// Default liefert die Werte aus dem Gesamtkonzept; fremde Aufträge sind aus (Opt-in).
func Default() Config {
	home, _ := os.UserHomeDir()
	return Config{
		Node: Node{
			DataDir:      filepath.Join(home, ".swarmainet"),
			Listen:       []string{"/ip4/0.0.0.0/udp/4711/quic-v1", "/ip4/0.0.0.0/tcp/4711", "/ip6/::/udp/4711/quic-v1", "/ip6/::/tcp/4711"},
			Gateway:      "127.0.0.1:7711",
			NetworkID:    "swarmainet",
			MDNS:         true,
			Provide:      false,
			PEX:          true,
			DHTServer:    "auto",
			RelayService: "auto",
		},
		Backends: []Backend{{
			Name: "ollama",
			URL:  "http://127.0.0.1:11434/v1",
		}},
		Resources: Resources{
			PrioritizeOwnUse:  true,
			AcceptPublicTasks: false,
			Hardware: Hardware{
				MaxParallelJobs:  1,
				GPUUtilMeanMax:   60,
				GPUWindowS:       120,
				RAMReserveGiB:    16,
				VRAMBudgetGiB:    24,
				ExtraPowerMaxW:   180,
				TemperatureStopC: 85,
			},
			Schedule: Schedule{
				Windows: []TimeWindow{
					{Days: []string{"mon", "tue", "wed", "thu", "fri"}, From: "22:00", To: "07:00"},
					{Days: []string{"sat", "sun"}, From: "00:00", To: "24:00"},
				},
				GPUMinutesPerDay: 180,
			},
			Energy: Energy{
				Mode:           "off",
				Source:         "home_assistant",
				ReserveW:       150,
				StartAboveW:    500,
				StartStableS:   300,
				StopBelowW:     250,
				StopStableS:    120,
				BatterySocMin:  50,
				PriceMaxEurKWh: 0.18,
				StaleReadings:  "no_new_jobs",
				ReadingMaxAgeS: 120,
				HomeAssistant:  HomeAssistant{URL: "http://homeassistant.local:8123", TokenEnv: "HA_TOKEN"},
			},
		},
		Tasks: Tasks{
			Pattern:      "draft_verify_synthesize",
			PrivacyClass: "trusted",
			MaxTokens:    8192,
			DeadlineS:    300,
			JobDeadlineS: 3600,
			MaxSubtasks:  8,
		},
		Cooperation: Cooperation{Selection: "rules", Exploration: 0.05},
	}
}

// Load liest path; fehlende Felder behalten die Standardwerte.
func Load(path string) (Config, error) {
	c := Default()
	data, err := os.ReadFile(path)
	if err != nil {
		return c, err
	}
	if err := checkModelEntries(data); err != nil {
		return c, fmt.Errorf("%s: %w", path, err)
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&c); err != nil {
		return c, fmt.Errorf("%s: %w", path, err)
	}
	c.Node.DataDir = expandHome(c.Node.DataDir)
	c.Node.NetworkKeyFile = expandHome(c.Node.NetworkKeyFile)
	return c, c.Validate()
}

// checkModelEntries erkennt Modelle, die nur als Name statt als Eintrag mit
// name:, family: … angegeben sind (z. B. `models: [gpt-oss:20b]`), und
// liefert dafür eine verständlichere Meldung als der YAML-Decoder.
func checkModelEntries(data []byte) error {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil || len(doc.Content) == 0 {
		return nil // Syntaxfehler meldet der eigentliche Decoder
	}
	backends := mappingValue(doc.Content[0], "backends")
	if backends == nil || backends.Kind != yaml.SequenceNode {
		return nil
	}
	for i, b := range backends.Content {
		models := mappingValue(b, "models")
		if models == nil || models.Kind != yaml.SequenceNode {
			continue
		}
		for _, m := range models.Content {
			if m.Kind != yaml.ScalarNode {
				continue
			}
			return fmt.Errorf("zeile %d: backends[%d].models: %q muss als eintrag mit name: angegeben werden, z. B.\n"+
				"      models:\n"+
				"        - name: %s\n"+
				"          family: …\n"+
				"          skills: [general]\n"+
				"          context: 32768\n"+
				"          offer: false\n"+
				"siehe config.example.yaml", m.Line, i, m.Value, m.Value)
		}
	}
	return nil
}

func mappingValue(n *yaml.Node, key string) *yaml.Node {
	if n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i+1]
		}
	}
	return nil
}

func expandHome(p string) string {
	if rest, ok := strings.CutPrefix(p, "~/"); ok {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, rest)
		}
	}
	return p
}

func (c Config) Validate() error {
	host, _, err := net.SplitHostPort(c.Node.Gateway)
	if err != nil {
		return fmt.Errorf("gateway: %w", err)
	}
	if ip := net.ParseIP(host); host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return fmt.Errorf("gateway %q: verwaltungsport darf nur auf localhost lauschen", c.Node.Gateway)
	}
	for i, b := range c.Backends {
		for j, m := range b.Models {
			if m.Strength < 0 || m.Strength > 10 {
				return fmt.Errorf("backends[%d].models[%d].strength %d: erlaubt sind 0 (unbekannt) bis 10", i, j, m.Strength)
			}
		}
	}
	if err := c.validateNetworks(); err != nil {
		return err
	}
	if c.Resources.Hardware.MaxParallelJobs < 0 {
		return fmt.Errorf("max_parallel_jobs < 0")
	}
	switch c.Resources.Energy.Mode {
	case "off", "pv_preferred", "pv_only", "price_only":
	default:
		return fmt.Errorf("energy.mode %q unbekannt", c.Resources.Energy.Mode)
	}
	e := c.Resources.Energy
	if e.Mode != "off" && e.StopBelowW > e.StartAboveW {
		return fmt.Errorf("energy: stop_below_w muss ≤ start_above_w sein (hysterese)")
	}
	switch c.Tasks.Pattern {
	case "single", "parallel", "draft_verify_synthesize", "parallel_verify", "cascade", "decompose":
	default:
		return fmt.Errorf("tasks.pattern %q unbekannt", c.Tasks.Pattern)
	}
	switch c.Cooperation.Selection {
	case "rules", "bandit":
	default:
		return fmt.Errorf("cooperation.selection %q: erlaubt sind rules, bandit", c.Cooperation.Selection)
	}
	if e := c.Cooperation.Exploration; e < 0 || e > 0.5 {
		return fmt.Errorf("cooperation.exploration %v: erlaubt ist 0 bis 0.5", e)
	}
	for cat, tg := range c.Cooperation.Targets {
		if tg.Quality <= 0 || tg.Quality > 1 {
			return fmt.Errorf("cooperation.targets.%s.quality %v: erlaubt ist ein Wert über 0 bis 1", cat, tg.Quality)
		}
		if tg.LatencyP90S < 0 {
			return fmt.Errorf("cooperation.targets.%s.latency_p90_s: keine negativen Werte", cat)
		}
		if tg.CascadeError < 0 || tg.CascadeError >= 1 {
			return fmt.Errorf("cooperation.targets.%s.cascade_error %v: erlaubt ist 0 bis unter 1", cat, tg.CascadeError)
		}
	}
	return nil
}

func (c Config) validateNetworks() error {
	ids := map[string]bool{}
	keys := map[string]string{}
	listen := map[string]string{}
	for i, n := range c.AllNetworks() {
		where := "node"
		if i > 0 {
			where = fmt.Sprintf("node.networks[%d]", i-1)
		}
		if !NetworkIDRe.MatchString(n.NetworkID) {
			return fmt.Errorf("%s.network_id %q: erlaubt sind a-z, 0-9 und -, höchstens 32 zeichen", where, n.NetworkID)
		}
		if ids[n.NetworkID] {
			return fmt.Errorf("%s.network_id %q kommt mehrfach vor", where, n.NetworkID)
		}
		ids[n.NetworkID] = true
		if err := validateMembership(n, where); err != nil {
			return err
		}
		if n.NetworkKeyFile != "" {
			if other, ok := keys[n.NetworkKeyFile]; ok {
				return fmt.Errorf("%s: netzschlüssel %s gehört schon zum netz %q", where, n.NetworkKeyFile, other)
			}
			keys[n.NetworkKeyFile] = n.NetworkID
		}
		switch n.DHTServer {
		case "auto", "true", "false":
		default:
			return fmt.Errorf("%s.dht_server %q: erlaubt sind auto, true, false", where, n.DHTServer)
		}
		switch n.RelayService {
		case "auto", "true", "false":
		default:
			return fmt.Errorf("%s.relay_service %q: erlaubt sind auto, true, false", where, n.RelayService)
		}
		for _, d := range n.DNSSeeds {
			if !hostnameRe.MatchString(d) {
				return fmt.Errorf("%s.dns_seeds %q: erwartet ein Domainname wie seed.example.net (oder none)", where, d)
			}
		}
		if l := n.RelayLimits; l.Reservations < 0 || l.CircuitsPerPeer < 0 || l.DataMiB < 0 || l.DurationS < 0 {
			return fmt.Errorf("%s.relay_limits: keine negativen Werte", where)
		}
		for _, a := range n.Announce {
			m, err := ma.NewMultiaddr(a)
			if err != nil {
				return fmt.Errorf("%s.announce %q: %w", where, a, err)
			}
			if _, err := m.ValueForProtocol(ma.P_P2P); err == nil {
				return fmt.Errorf("%s.announce %q: ohne /p2p/<peer-id> angeben", where, a)
			}
		}
		for _, u := range n.Trackers {
			if err := tracker.CheckURL(u); err != nil {
				return fmt.Errorf("%s.trackers: %w", where, err)
			}
		}
		if i > 0 && len(n.Listen) == 0 {
			return fmt.Errorf("%s.listen fehlt: jedes weitere netz braucht einen eigenen port", where)
		}
		for _, a := range n.Listen {
			if strings.HasSuffix(a, "/0") {
				continue // zufälliger port
			}
			if other, ok := listen[a]; ok {
				if other == n.NetworkID {
					return fmt.Errorf("%s.listen %s steht doppelt in der liste", where, a)
				}
				return fmt.Errorf("%s.listen %s wird schon vom netz %q benutzt", where, a, other)
			}
			listen[a] = n.NetworkID
		}
	}
	for i, x := range c.Node.Networks {
		if x.Membership != nil && x.Identity == "shared" {
			return fmt.Errorf("node.networks[%d]: identity shared ist in einem mitgliedsnetz nicht erlaubt", i)
		}
		if x.Identity != "" && x.Identity != "separate" && x.Identity != "shared" {
			return fmt.Errorf("node.networks[%d].identity %q: erlaubt sind separate, shared", i, x.Identity)
		}
	}
	return nil
}

// emptyModelsRe trifft eine leere Modellliste; Save ersetzt sie durch eine
// auskommentierte Vorlage, damit das erwartete Format in der Datei sichtbar ist.
var emptyModelsRe = regexp.MustCompile(`(?m)^( *)models: \[\]$`)

const modelsTemplate = "${1}models:\n" +
	"${1}    # Je Modell ein Eintrag; name wie vom Server gemeldet (GET <url>/models).\n" +
	"${1}    # - name: qwen2.5-coder:7b\n" +
	"${1}    #   family: qwen\n" +
	"${1}    #   skills: [code.python, code.go, code.review]\n" +
	"${1}    #   context: 32768\n" +
	"${1}    #   strength: 6           # 1 (schwach) bis 10 (stark), wählt den Prüfer\n" +
	"${1}    #   offer: false"

// UpdateNode setzt Schlüssel unter node: in einer bestehenden Datei und
// erhält dabei Kommentare (für `swarmainet join`). Die geänderte Datei muss
// gültig bleiben.
func UpdateNode(path string, set map[string]any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if len(doc.Content) == 0 {
		doc = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode}}}
	}
	root := doc.Content[0]
	node := mappingValue(root, "node")
	if node == nil {
		node = &yaml.Node{Kind: yaml.MappingNode}
		root.Content = append(root.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: "node"}, node)
	}
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		var v yaml.Node
		if err := v.Encode(set[k]); err != nil {
			return err
		}
		if old := mappingValue(node, k); old != nil {
			v.HeadComment, v.LineComment, v.FootComment = old.HeadComment, old.LineComment, old.FootComment
			*old = v
		} else {
			node.Content = append(node.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: k}, &v)
		}
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o600); err != nil {
		return err
	}
	if _, err := Load(tmp); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

// Save schreibt die Konfiguration (für `swarmainet init`).
func Save(path string, c Config) error {
	data, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	data = emptyModelsRe.ReplaceAll(data, []byte(modelsTemplate))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}
