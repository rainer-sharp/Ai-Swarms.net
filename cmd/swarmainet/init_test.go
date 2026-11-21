// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Rainer Sestig. Kommerzielle Lizenz: siehe COMMERCIAL.md.

package main

import (
	"bytes"
	"crypto/rand"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/rainer-sharp/Ai-Swarms.net/internal/config"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/identity"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/invite"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/membership"
)

func loadCfg(t *testing.T, path string) config.Config {
	t.Helper()
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func initAt(t *testing.T, open bool) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := cmdInit(path, open, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	return path
}

// inviteLink baut eine Einladung in ein privates (psk) oder offenes Netz.
func inviteLink(t *testing.T, network string, psk bool) string {
	t.Helper()
	id, _ := identity.New()
	inv := invite.Invite{Network: network, Peers: []string{"/ip4/192.0.2.1/tcp/4711/p2p/" + id.ID.String()}}
	if psk {
		inv.PSK = make([]byte, 32)
		_, _ = rand.Read(inv.PSK)
	}
	return inv.String()
}

// TestInitPrivateByDefault: init legt ein eigenes privates Netz an, nicht das
// offene; mit -open das offene.
func TestInitPrivateByDefault(t *testing.T) {
	path := initAt(t, false)
	cfg := loadCfg(t, path)
	n := cfg.Node
	if !regexp.MustCompile(`^p-[0-9a-f]{8}$`).MatchString(n.NetworkID) {
		t.Fatalf("netz-kennung %q", n.NetworkID)
	}
	if n.NetworkKeyFile != filepath.Join(n.DataDir, n.NetworkID+".key") {
		t.Fatalf("schlüsseldatei %q", n.NetworkKeyFile)
	}
	st, err := os.Stat(n.NetworkKeyFile)
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("schlüsseldatei: %v %v", st, err)
	}
	if _, err := invite.ReadPSK(n.NetworkKeyFile); err != nil {
		t.Fatal(err)
	}
	if s := cfg.AllNetworks()[0].DNSSeeds; len(s) != 0 {
		t.Fatalf("privates netz fragt dns-seeds: %v", s)
	}
	for _, a := range n.Listen {
		if !strings.Contains(a, "/tcp/") {
			t.Fatalf("privates netz lauscht auf %s", a)
		}
	}
	if !freshPrivate(cfg) {
		t.Fatal("frisches netz nicht erkannt")
	}

	open := loadCfg(t, initAt(t, true)).Node
	if open.NetworkID != "swarmainet" || open.NetworkKeyFile != "" {
		t.Fatalf("init -open: %+v", open)
	}
	if err := cmdInit(path, false, &bytes.Buffer{}); err == nil {
		t.Fatal("vorhandene konfiguration überschrieben")
	}
}

// TestJoinAfterInit: Eine Einladung ersetzt das noch nie gestartete private
// Netz aus init; ein benutztes bleibt, das neue kommt dazu.
func TestJoinAfterInit(t *testing.T) {
	t.Run("frisch, privat", func(t *testing.T) {
		path := initAt(t, false)
		if err := cmdJoin(path, inviteLink(t, "p-12345678", true), ""); err != nil {
			t.Fatal(err)
		}
		n := loadCfg(t, path).Node
		if n.NetworkID != "p-12345678" || n.NetworkKeyFile != filepath.Join(n.DataDir, "p-12345678.key") || len(n.Networks) != 0 {
			t.Fatalf("%+v", n)
		}
	})
	t.Run("frisch, offen", func(t *testing.T) {
		path := initAt(t, false)
		if err := cmdJoin(path, inviteLink(t, "swarmainet", false), ""); err != nil {
			t.Fatal(err)
		}
		cfg := loadCfg(t, path)
		n := cfg.Node
		if n.NetworkID != "swarmainet" || n.NetworkKeyFile != "" || len(n.Networks) != 0 ||
			!slices.ContainsFunc(n.Listen, func(a string) bool { return strings.Contains(a, "quic") }) {
			t.Fatalf("%+v", n)
		}
		if len(cfg.AllNetworks()[0].DNSSeeds) == 0 {
			t.Fatal("offenes netz ohne dns-seed")
		}
	})
	t.Run("benutzt", func(t *testing.T) {
		path := initAt(t, false)
		before := loadCfg(t, path).Node
		if err := os.WriteFile(filepath.Join(before.DataDir, "peers-"+before.NetworkID+".json"), []byte("[]"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := cmdJoin(path, inviteLink(t, "p-12345678", true), ""); err != nil {
			t.Fatal(err)
		}
		n := loadCfg(t, path).Node
		if n.NetworkID != before.NetworkID || n.NetworkKeyFile != before.NetworkKeyFile ||
			len(n.Networks) != 1 || n.Networks[0].NetworkID != "p-12345678" {
			t.Fatalf("eigenes netz verändert: %+v", n)
		}
	})
	t.Run("ohne konfiguration", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.yaml")
		if err := cmdJoin(path, inviteLink(t, "p-12345678", true), ""); err != nil {
			t.Fatal(err)
		}
		n := loadCfg(t, path).Node
		if n.NetworkID != "p-12345678" || len(n.Networks) != 0 {
			t.Fatalf("%+v", n)
		}
		keys, _ := filepath.Glob(filepath.Join(n.DataDir, "p-*.key"))
		if len(keys) != 1 {
			t.Fatalf("schlüsseldateien: %v", keys)
		}
	})
	t.Run("frisch, mitgliedsnetz", func(t *testing.T) {
		path := initAt(t, false)
		key, _ := identity.New()
		pub, _ := membership.RootKey(key)
		network := membership.NetworkID(pub)
		if err := addMemberNetwork(path, network, membership.EncodeRoot(pub), nil, &bytes.Buffer{}); err != nil {
			t.Fatal(err)
		}
		n := loadCfg(t, path).Node
		if n.NetworkID != network || n.NetworkKeyFile != "" || n.Membership == nil || len(n.Networks) != 0 {
			t.Fatalf("%+v", n)
		}
	})
}
