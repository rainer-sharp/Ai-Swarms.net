// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Rainer Sestig. Kommerzielle Lizenz: siehe COMMERCIAL.md.

package p2p

import (
	"testing"

	ma "github.com/multiformats/go-multiaddr"
)

// TestPortPhrase: Firewall-Hinweise nennen die tatsächlichen Ports, nicht 4711.
func TestPortPhrase(t *testing.T) {
	addrs := func(s ...string) []ma.Multiaddr {
		var out []ma.Multiaddr
		for _, x := range s {
			out = append(out, ma.StringCast(x))
		}
		return out
	}
	for _, c := range []struct {
		addrs  []ma.Multiaddr
		family string
		want   string
	}{
		{addrs("/ip4/0.0.0.0/tcp/5000", "/ip6/::/tcp/5000"), "", "Port 5000/tcp"},
		{addrs("/ip4/0.0.0.0/udp/4800/quic-v1", "/ip4/0.0.0.0/tcp/4800", "/ip6/::/tcp/4900"), "", "Ports 4800/tcp, 4800/udp und 4900/tcp"},
		{addrs("/ip4/0.0.0.0/tcp/4800", "/ip6/::/tcp/4900"), "ip6", "Port 4900/tcp"},
		{addrs("/ip4/192.0.2.1/tcp/4711/p2p/12D3KooWDpJ7As7BWAwRMfu1VU2WCqNjvq387JEYKDBj4kx6nXTN/p2p-circuit"), "", "die Ports des Knotens"},
		{nil, "", "die Ports des Knotens"},
	} {
		if got := portPhrase(c.addrs, c.family); got != c.want {
			t.Errorf("%v %q: %q, erwartet %q", c.addrs, c.family, got, c.want)
		}
	}
}
