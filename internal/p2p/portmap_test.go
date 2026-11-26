// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Rainer Sestig und Mitwirkende.

package p2p

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	bhost "github.com/libp2p/go-libp2p/p2p/host/basic"
	ma "github.com/multiformats/go-multiaddr"

	"github.com/rainer-sharp/Ai-Swarms.net/internal/identity"
)

// fakeNAT gibt jeden TCP-Port unter einer öffentlichen Adresse frei, ohne
// einen echten Router anzusprechen.
type fakeNAT struct{}

func (fakeNAT) GetMapping(a ma.Multiaddr) ma.Multiaddr {
	if _, err := a.ValueForProtocol(ma.P_TCP); err != nil {
		return nil
	}
	return ma.StringCast("/ip4/93.184.215.14/tcp/4711")
}
func (fakeNAT) HasDiscoveredNAT() bool { return true }
func (fakeNAT) Close() error           { return nil }

func TestPortMapping(t *testing.T) {
	old := newNATManager
	newNATManager = func(network.Network) bhost.NATManager { return fakeNAT{} }
	defer func() { newNATManager = old }()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	id, _ := identity.New()
	n, err := New(ctx, Options{Identity: id, Listen: []string{"/ip4/127.0.0.1/tcp/0"}, NetworkID: "swarmainet", PortMapping: true})
	if err != nil {
		t.Fatal(err)
	}
	defer n.Close()

	m := n.MappingInfo()
	if m == nil || !m.Gateway || !slices.Equal(m.Mapped, []string{"/ip4/93.184.215.14/tcp/4711"}) {
		t.Fatalf("freigabe im status: %+v", m)
	}
	// Die freigegebene öffentliche Adresse wird bekannt gegeben, Loopback nicht.
	deadline := time.Now().Add(10 * time.Second)
	for !slices.ContainsFunc(n.Addrs(), func(a string) bool { return strings.HasPrefix(a, "/ip4/93.184.215.14/tcp/4711") }) {
		if time.Now().After(deadline) {
			t.Fatalf("freigegebene adresse fehlt: %v", n.Addrs())
		}
		time.Sleep(50 * time.Millisecond)
	}
	if slices.ContainsFunc(n.Addrs(), func(a string) bool { return strings.Contains(a, "127.0.0.1") }) {
		t.Fatalf("loopback veröffentlicht: %v", n.Addrs())
	}

	// Ohne port_mapping kein NAT-Manager.
	id2, _ := identity.New()
	n2, err := New(ctx, Options{Identity: id2, Listen: []string{"/ip4/127.0.0.1/tcp/0"}, NetworkID: "swarmainet"})
	if err != nil {
		t.Fatal(err)
	}
	defer n2.Close()
	if n2.MappingInfo() != nil || n2.nat != nil {
		t.Fatal("nat-manager ohne port_mapping")
	}
}

type noGateway struct{ fakeNAT }

func (noGateway) HasDiscoveredNAT() bool               { return false }
func (noGateway) GetMapping(ma.Multiaddr) ma.Multiaddr { return nil }

type noMapping struct{ fakeNAT }

func (noMapping) GetMapping(ma.Multiaddr) ma.Multiaddr { return nil }

func TestPortMappingHints(t *testing.T) {
	old := newNATManager
	defer func() { newNATManager = old }()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	for _, c := range []struct {
		nat  bhost.NATManager
		want string
	}{{noGateway{}, "kein Router"}, {noMapping{}, "Selbstständige Portfreigaben"}} {
		newNATManager = func(network.Network) bhost.NATManager { return c.nat }
		id, _ := identity.New()
		n, err := New(ctx, Options{Identity: id, Listen: []string{"/ip4/127.0.0.1/tcp/0"}, NetworkID: "swarmainet", PortMapping: true})
		if err != nil {
			t.Fatal(err)
		}
		if h := n.mappingHint(); h != "" {
			t.Fatalf("hinweis vor ablauf der suchzeit: %s", h)
		}
		n.started = time.Now().Add(-2 * mappingHintAfter)
		if !slices.ContainsFunc(n.Hints(), func(h string) bool { return strings.Contains(h, c.want) }) {
			t.Fatalf("hinweis %q fehlt: %v", c.want, n.Hints())
		}
		n.Close()
	}
}
