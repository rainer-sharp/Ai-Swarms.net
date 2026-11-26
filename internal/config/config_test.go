// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Rainer Sestig und Mitwirkende.

package config

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rainer-sharp/Ai-Swarms.net/internal/identity"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/membership"
)

func TestDefaultsAreOptIn(t *testing.T) {
	c := Default()
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	if c.Resources.AcceptPublicTasks || c.Node.Provide {
		t.Fatal("teilnahme muss opt-in sein")
	}
}

func TestGatewayMustBeLocal(t *testing.T) {
	c := Default()
	c.Node.Gateway = "0.0.0.0:7711"
	if c.Validate() == nil {
		t.Fatal("gateway auf allen schnittstellen erlaubt")
	}
}

func TestLoadRoundtripAndUnknownKeys(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "c.yaml")
	if err := Save(p, Default()); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(p); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(p, []byte("ressourcen:\n  tippfehler: 1\n"), 0o600)
	if _, err := Load(p); err == nil {
		t.Fatal("unbekannter schlüssel nicht erkannt")
	}
}

func TestExampleConfigLoads(t *testing.T) {
	c, err := Load("../../config.example.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if home, _ := os.UserHomeDir(); c.Node.DataDir != filepath.Join(home, ".swarmainet") {
		t.Fatalf("~ nicht expandiert: %s", c.Node.DataDir)
	}
	if len(c.Backends[0].Models) != 2 {
		t.Fatal("modelle fehlen")
	}
}

func TestSaveShowsModelTemplate(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.yaml")
	if err := Save(p, Default()); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(p)
	if !strings.Contains(string(data), "# - name: ") || strings.Contains(string(data), "models: []") {
		t.Fatalf("modellvorlage fehlt:\n%s", data)
	}
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Backends[0].Models) != 0 {
		t.Fatal("vorlage darf keine modelle anlegen")
	}
}

func TestModelAsPlainNameIsExplained(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.yaml")
	_ = os.WriteFile(p, []byte("backends:\n  - name: ollama\n    url: http://127.0.0.1:11434/v1\n    models: [gpt-oss:20b]\n"), 0o600)
	_, err := Load(p)
	if err == nil || !strings.Contains(err.Error(), "zeile 4: backends[0].models: \"gpt-oss:20b\"") {
		t.Fatalf("unerwartete meldung: %v", err)
	}
}

func TestModelStrengthRange(t *testing.T) {
	c := Default()
	c.Backends[0].Models = []Model{{Name: "m", Strength: 11}}
	if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "backends[0].models[0].strength") {
		t.Fatalf("strength 11 nicht abgelehnt: %v", err)
	}
	c.Backends[0].Models[0].Strength = 10
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestDHTServer(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.yaml")
	for in, want := range map[string]string{"true": "true", "false": "false", "auto": "auto"} {
		_ = os.WriteFile(p, []byte("node:\n  dht_server: "+in+"\n"), 0o600)
		c, err := Load(p)
		if err != nil || c.Node.DHTServer != want {
			t.Fatalf("dht_server %s: %q, %v", in, c.Node.DHTServer, err)
		}
	}
	_ = os.WriteFile(p, []byte("node:\n  dht_server: immer\n"), 0o600)
	if _, err := Load(p); err == nil {
		t.Fatal("ungültiger wert nicht erkannt")
	}
}

func TestUpdateNodeKeepsComments(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.yaml")
	_ = os.WriteFile(p, []byte("# Kopf\nnode:\n  # Kennung\n  network_id: swarmainet  # alt\n  mdns: true\nbackends: []\n"), 0o600)
	err := UpdateNode(p, map[string]any{"network_id": "p-1234", "bootstrap": []string{"/ip4/1.2.3.4/tcp/4711/p2p/12D3KooWJGzUm1EzV4v88f1rDPF3TkX1bZYy3U7CahkTZuBrDYTP"}})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(p)
	for _, want := range []string{"# Kopf", "# Kennung", "# alt", "network_id: p-1234", "bootstrap:"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("%q fehlt:\n%s", want, data)
		}
	}
	c, err := Load(p)
	if err != nil || c.Node.NetworkID != "p-1234" || len(c.Node.Bootstrap) != 1 || !c.Node.MDNS {
		t.Fatalf("%+v %v", c.Node, err)
	}
	if err := UpdateNode(p, map[string]any{"dht_server": "immer"}); err == nil {
		t.Fatal("ungültige änderung geschrieben")
	}
	if c2, _ := Load(p); c2.Node.DHTServer != "auto" {
		t.Fatal("datei trotz fehler verändert")
	}
}

func TestDockerHelperConfigLoads(t *testing.T) {
	c, err := Load("../../deploy/docker/helper.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if !c.Node.Helper || c.Node.DataDir != "/data" || len(c.Backends) != 0 {
		t.Fatalf("%+v", c.Node)
	}
}

func TestAnnounce(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.yaml")
	_ = os.WriteFile(p, []byte("node:\n  announce: [/dns4/swarm.example.de/tcp/4711]\n"), 0o600)
	if c, err := Load(p); err != nil || c.AllNetworks()[0].Announce[0] != "/dns4/swarm.example.de/tcp/4711" {
		t.Fatalf("%v", err)
	}
	for _, bad := range []string{"swarm.example.de:4711", "/dns4/swarm.example.de/tcp/4711/p2p/12D3KooWJGzUm1EzV4v88f1rDPF3TkX1bZYy3U7CahkTZuBrDYTP"} {
		_ = os.WriteFile(p, []byte("node:\n  announce: [\""+bad+"\"]\n"), 0o600)
		if _, err := Load(p); err == nil {
			t.Errorf("%s angenommen", bad)
		}
	}
}

func TestRaspberryPiConfigLoads(t *testing.T) {
	c, err := Load("../../deploy/raspberry-pi/config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if !c.Node.Helper || len(c.Node.Announce) != 0 || c.Node.MDNS || c.Node.NetworkID != "swarmainet" {
		t.Fatalf("%+v", c.Node)
	}
}

func TestPortMappingPerNetwork(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.yaml")
	_ = os.WriteFile(p, []byte("node:\n  port_mapping: true\n  networks:\n    - network_id: zwei\n      listen: [/ip4/0.0.0.0/tcp/4799]\n      port_mapping: false\n"), 0o600)
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if n := c.AllNetworks(); !n[0].PortMapping || n[1].PortMapping {
		t.Fatalf("%+v", n)
	}
	if Default().Node.PortMapping {
		t.Fatal("port_mapping muss opt-in sein")
	}
}

func TestVPSConfigsLoad(t *testing.T) {
	open, err := Load("../../deploy/vps/open.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if n := open.AllNetworks()[0]; n.NetworkKeyFile != "" || len(n.DNSSeeds) != 0 || !open.Node.Helper || n.RelayLimits.Reservations != 128 {
		t.Fatalf("offen: %+v", n)
	}
	demo, err := Load("../../deploy/vps/demo.yaml")
	if err != nil {
		t.Fatal(err)
	}
	// Gruppennetz: kein Netzschlüssel auf dem VPS, nur ein Zertifikat
	if n := demo.AllNetworks()[0]; n.NetworkKeyFile != "" || n.Membership == nil || len(n.DNSSeeds) != 0 || !demo.Node.Helper {
		t.Fatalf("demo: %+v", n)
	}
	priv, err := Load("../../deploy/vps/private.yaml")
	if err != nil {
		t.Fatal(err)
	}
	// vor dem join wie das offene Netz benannt, damit join es ersetzt
	if n := priv.AllNetworks()[0]; n.NetworkID != "swarmainet" || len(n.DNSSeeds) != 0 || !priv.Node.Helper {
		t.Fatalf("privat: %+v", n)
	}
	gw := map[string]bool{}
	for _, c := range []Config{open, demo, priv} {
		if gw[c.Node.Gateway] || gw[c.Node.Listen[0]] {
			t.Fatalf("gateway oder port doppelt: %s %s", c.Node.Gateway, c.Node.Listen[0])
		}
		gw[c.Node.Gateway], gw[c.Node.Listen[0]] = true, true
	}
}

func TestDuplicateListenIsExplained(t *testing.T) {
	c := Default()
	c.Node.Listen = []string{"/ip6/::/tcp/4711", "/ip6/::/tcp/4711"}
	if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "steht doppelt") {
		t.Fatalf("fehler: %v", err)
	}
}

func TestMembershipValidation(t *testing.T) {
	key, _ := identity.New()
	pub, _ := membership.RootKey(key)
	root, id := membership.EncodeRoot(pub), membership.NetworkID(pub)
	base := func() Config {
		c := Default()
		c.Node.NetworkID = id
		c.Node.DNSSeeds = nil
		c.Node.Membership = &Membership{Root: root}
		return c
	}
	if err := func() error { c := base(); return c.Validate() }(); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		mod  func(*Config)
		want string
	}{
		{"kennung passt nicht", func(c *Config) { c.Node.NetworkID = "m-0000000000000000" }, "passt nicht"},
		{"schlüssel kaputt", func(c *Config) { c.Node.Membership.Root = "kurz" }, "membership.root"},
		{"mit netzschlüssel", func(c *Config) { c.Node.NetworkKeyFile = "/x.key" }, "schließen einander aus"},
		{"mit tracker", func(c *Config) { c.Node.Trackers = []string{"https://t.example.net"} }, "trackers"},
		{"mit trusted_peers", func(c *Config) { c.Node.TrustedPeers = []string{key.ID.String()} }, "trusted_peers"},
		{"mit dns-seed", func(c *Config) { c.Node.DNSSeeds = []string{"seed.example.net"} }, "dns_seeds"},
		{"präfix ohne membership", func(c *Config) { c.Node.Membership = nil }, "vorbehalten"},
		{"geteilte identität", func(c *Config) {
			c.Node.Membership, c.Node.NetworkID = nil, "swarmainet"
			c.Node.Networks = []Network{{NetworkID: id, Listen: []string{"/ip4/0.0.0.0/tcp/4714"}, Identity: "shared", Membership: &Membership{Root: root}}}
		}, "shared"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := base()
			tc.mod(&c)
			if err := c.Validate(); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("fehler: %v", err)
			}
		})
	}
	// Ein Mitgliedsnetz fragt nie den eingebauten DNS-Seed.
	c := base()
	if s := c.AllNetworks()[0].DNSSeeds; len(s) != 0 {
		t.Fatalf("seeds: %v", s)
	}
}

// TestDNSSeedsApplyToAllNetworks: dns_seeds von oben gelten auch für die
// Netze unter networks, eigene Angaben gehen vor, Mitgliedsnetze fragen nie.
func TestDNSSeedsApplyToAllNetworks(t *testing.T) {
	key, _ := identity.New()
	pub, _ := membership.RootKey(key)
	member := Network{NetworkID: membership.NetworkID(pub), Listen: []string{"/ip4/0.0.0.0/tcp/4714"},
		Membership: &Membership{Root: membership.EncodeRoot(pub)}}
	open := Network{NetworkID: "swarmainet", Listen: []string{"/ip4/0.0.0.0/tcp/4712"}}
	own := Network{NetworkID: "eigen", Listen: []string{"/ip4/0.0.0.0/tcp/4713"}, DNSSeeds: []string{"seed.example.net"}}
	for _, tc := range []struct {
		top        []string
		open, eign []string
	}{
		{nil, DefaultDNSSeeds, []string{"seed.example.net"}},
		{[]string{"none"}, nil, []string{"seed.example.net"}},
		{[]string{"seed.example.org"}, []string{"seed.example.org"}, []string{"seed.example.net"}},
	} {
		c := Default()
		c.Node.NetworkID, c.Node.NetworkKeyFile, c.Node.DNSSeeds = "p-12345678", "/x.key", tc.top
		c.Node.Networks = []Network{open, own, member}
		if err := c.Validate(); err != nil {
			t.Fatal(err)
		}
		n := c.AllNetworks()
		if !slices.Equal(n[1].DNSSeeds, tc.open) || !slices.Equal(n[2].DNSSeeds, tc.eign) || len(n[3].DNSSeeds) != 0 {
			t.Errorf("oben %v: offen %v, eigen %v, mitglied %v", tc.top, n[1].DNSSeeds, n[2].DNSSeeds, n[3].DNSSeeds)
		}
	}
}

// TestCooperationValidation: Standard ist die bisherige Regel; ungültige
// Werte für Auswahl, Erkundung und Sollwerte werden abgewiesen.
func TestCooperationValidation(t *testing.T) {
	if d := Default(); d.Cooperation.Selection != "rules" || d.Validate() != nil {
		t.Fatalf("standard: %+v", d.Cooperation)
	}
	for name, mod := range map[string]func(*Config){
		"auswahl":   func(c *Config) { c.Cooperation.Selection = "zufall" },
		"erkundung": func(c *Config) { c.Cooperation.Exploration = 0.6 },
		"sollwert":  func(c *Config) { c.Cooperation.Targets = map[string]Target{"code": {Quality: 1.2}} },
		"muster":    func(c *Config) { c.Tasks.Pattern = "debatte" },
	} {
		c := Default()
		mod(&c)
		if c.Validate() == nil {
			t.Errorf("%s angenommen", name)
		}
	}
	c := Default()
	c.Cooperation = Cooperation{Selection: "bandit", Exploration: 0.1, Targets: map[string]Target{"code": {Quality: 0.8, LatencyP90S: 60}}}
	c.Tasks.Pattern = "cascade"
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
}
