// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Rainer Sestig und Mitwirkende.

package p2p

import (
	"context"
	"net"
	"testing"
	"time"

	madns "github.com/multiformats/go-multiaddr-dns"

	"github.com/rainer-sharp/Ai-Swarms.net/internal/identity"
)

// fakeTXT beantwortet TXT-Anfragen aus einer Tabelle.
type fakeTXT map[string][]string

func (f fakeTXT) LookupIPAddr(context.Context, string) ([]net.IPAddr, error) { return nil, nil }
func (f fakeTXT) LookupTXT(_ context.Context, name string) ([]string, error) {
	return f[name], nil
}

// TestDNSSeed: Ein Knoten ohne Peers und ohne Bootstrap findet den
// Startknoten über den _dnsaddr-Eintrag seines DNS-Seeds.
func TestDNSSeed(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	idS, _ := identity.New()
	s, err := New(ctx, Options{Identity: idS, Listen: []string{"/ip4/127.0.0.1/tcp/0"}, NetworkID: "swarmainet", AnnouncePrivate: true})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	r, err := madns.NewResolver(madns.WithDomainResolver("seed.test.", fakeTXT{
		"_dnsaddr.seed.test": {"dnsaddr=" + s.Addrs()[0]},
	}))
	if err != nil {
		t.Fatal(err)
	}
	old := seedResolver
	seedResolver = r
	defer func() { seedResolver = old }()

	idA, _ := identity.New()
	a, err := New(ctx, Options{Identity: idA, NetworkID: "swarmainet", DNSSeeds: []string{"seed.test"}})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	deadline := time.Now().Add(10 * time.Second)
	for !a.connected(s.ID()) {
		if time.Now().After(deadline) {
			t.Fatal("startknoten über dns-seed nicht gefunden")
		}
		time.Sleep(50 * time.Millisecond)
	}
}
