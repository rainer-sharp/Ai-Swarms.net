// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Rainer Sestig. Kommerzielle Lizenz: siehe COMMERCIAL.md.

package p2p

import (
	"context"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/event"
	ma "github.com/multiformats/go-multiaddr"

	"github.com/rainer-sharp/Ai-Swarms.net/internal/identity"
)

func addrs(s ...string) []ma.Multiaddr {
	var out []ma.Multiaddr
	for _, a := range s {
		out = append(out, ma.StringCast(a))
	}
	return out
}

func TestFamilyReach(t *testing.T) {
	for _, c := range []struct {
		e    event.EvtHostReachableAddrsChanged
		want map[string]string
	}{
		{ // DS-Lite: IPv4 scheitert, IPv6 erreichbar
			event.EvtHostReachableAddrsChanged{
				Reachable:   addrs("/ip6/2606:4700:4700::1111/tcp/4711"),
				Unreachable: addrs("/ip4/93.184.215.14/tcp/4711"),
			},
			map[string]string{"ip4": "private", "ip6": "public"},
		},
		{ // eine erreichbare Adresse genügt; Relay-Adressen zählen nicht
			event.EvtHostReachableAddrsChanged{
				Reachable:   addrs("/ip4/93.184.215.14/udp/4711/quic-v1"),
				Unreachable: addrs("/ip4/93.184.215.14/tcp/4711", "/ip4/1.2.3.4/tcp/1/p2p/12D3KooWJGzUm1EzV4v88f1rDPF3TkX1bZYy3U7CahkTZuBrDYTP/p2p-circuit"),
				Unknown:     addrs("/ip6/2606:4700:4700::1111/tcp/4711"),
			},
			map[string]string{"ip4": "public", "ip6": "unknown"},
		},
	} {
		if got := familyReach(c.e); !maps.Equal(got, c.want) {
			t.Errorf("%v, erwartet %v", got, c.want)
		}
	}
}

// TestFamilyHint: Meldet AutoNAT IPv4 unerreichbar und IPv6 erreichbar,
// zeigen Status und Hinweise das (DS-Lite).
func TestFamilyHint(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	id, _ := identity.New()
	n, err := New(ctx, Options{Identity: id, Listen: []string{"/ip4/127.0.0.1/tcp/0"}, NetworkID: "swarmainet"})
	if err != nil {
		t.Fatal(err)
	}
	defer n.Close()
	em, err := n.host.EventBus().Emitter(new(event.EvtHostReachableAddrsChanged))
	if err != nil {
		t.Fatal(err)
	}
	defer em.Close()
	_ = em.Emit(event.EvtHostReachableAddrsChanged{
		Reachable:   addrs("/ip6/2606:4700:4700::1111/tcp/4711"),
		Unreachable: addrs("/ip4/93.184.215.14/tcp/4711"),
	})
	deadline := time.Now().Add(5 * time.Second)
	for n.FamilyReachability()["ip6"] != "public" {
		if time.Now().After(deadline) {
			t.Fatalf("erreichbarkeit je familie: %v", n.FamilyReachability())
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !slices.ContainsFunc(n.Hints(), func(h string) bool { return strings.Contains(h, "DS-Lite") }) {
		t.Fatalf("hinweis fehlt: %v", n.Hints())
	}
}
