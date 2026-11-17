// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Rainer Sestig. Kommerzielle Lizenz: siehe COMMERCIAL.md.

package p2p

import (
	"context"
	"testing"
	"time"

	ma "github.com/multiformats/go-multiaddr"

	"github.com/rainer-sharp/Ai-Swarms.net/internal/identity"
)

func TestShareableAddrs(t *testing.T) {
	relay := "/p2p/12D3KooWF1Yvbw9HELe3xoKyc1BuE2Uhv2aLQU9fq3uC8RH1w8BT/p2p-circuit"
	cases := []struct {
		addr          string
		open, private bool
	}{
		{"/ip4/192.168.101.52/tcp/4711", false, true},
		{"/ip4/127.0.0.1/tcp/4711", false, false},
		{"/ip6/::1/tcp/4711", false, false},
		{"/ip6/fd00::1/tcp/4711", false, true},
		{"/ip4/93.184.215.14/tcp/4711", true, true},
		{"/ip6/2a00:1450:4001::1/udp/4711/quic-v1", true, true},
		{"/ip4/93.184.215.14/tcp/4711" + relay, true, true},
		{"/ip4/10.0.0.5/tcp/4711" + relay, false, true},
	}
	for _, c := range cases {
		a := ma.StringCast(c.addr)
		if got := (&Node{}).shareable(a); got != c.open {
			t.Errorf("offen %s: %v", c.addr, got)
		}
		if got := (&Node{private: true}).shareable(a); got != c.private {
			t.Errorf("privat %s: %v", c.addr, got)
		}
	}
}

func TestRateLimiter(t *testing.T) {
	a, b := newPeerID(t), newPeerID(t)
	r := newRateLimiter(2, 3)
	now := time.Now()
	if !r.allow(a, now) || !r.allow(a, now) || r.allow(a, now) {
		t.Fatal("grenze je peer")
	}
	if !r.allow(b, now) || r.allow(b, now) {
		t.Fatal("gesamtgrenze")
	}
	if !r.allow(a, now.Add(limitWindow)) {
		t.Fatal("neues zeitfenster")
	}
}

func TestRelayResources(t *testing.T) {
	open := relayResources(false, false, RelayLimits{})
	auto := relayResources(false, true, RelayLimits{})
	priv := relayResources(true, false, RelayLimits{})
	if auto.MaxReservations >= open.MaxReservations || auto.MaxReservationsPerIP >= open.MaxReservationsPerIP {
		t.Fatalf("auto nicht sparsamer: %+v / %+v", auto, open)
	}
	if priv.MaxCircuits <= open.MaxCircuits || priv.MaxReservationsPerIP != priv.MaxReservations {
		t.Fatalf("privat: %+v", priv)
	}
	l := relayResources(false, true, RelayLimits{Reservations: 100, CircuitsPerPeer: 3, DataMiB: 16, DurationS: 60})
	if l.MaxReservations != 100 || l.MaxCircuits != 3 || l.Limit.Data != 16<<20 || l.Limit.Duration != time.Minute {
		t.Fatalf("grenzen nicht übernommen: %+v %+v", l, l.Limit)
	}
}

func TestRelayModes(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	for _, c := range []struct {
		mode   string
		active bool
	}{{RelayOn, true}, {RelayAuto, false}, {RelayOff, false}} {
		id, _ := identity.New()
		n, err := New(ctx, Options{Identity: id, Listen: []string{"/ip4/127.0.0.1/tcp/0"}, NetworkID: "swarmainet", RelayService: c.mode})
		if err != nil {
			t.Fatal(err)
		}
		// auto: erst bei öffentlicher Erreichbarkeit, die auf Loopback nie kommt
		if got := n.RelayService(); got != c.active {
			t.Errorf("%s: relay aktiv %v", c.mode, got)
		}
		n.Close()
	}
}
