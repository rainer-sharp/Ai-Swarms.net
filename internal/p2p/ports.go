// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Rainer Sestig. Kommerzielle Lizenz: siehe COMMERCIAL.md.

package p2p

import (
	"slices"
	"strings"

	ma "github.com/multiformats/go-multiaddr"
)

// portList nennt die Ports der Adressen für Firewall-Hinweise, etwa
// „4711/tcp und 4711/udp“. family beschränkt auf ip4 oder ip6 (leer: beide).
// Relay-Adressen zählen nicht; ohne Ports ist das Ergebnis leer.
func portList(addrs []ma.Multiaddr, family string) string {
	var out []string
	for _, a := range addrs {
		if _, err := a.ValueForProtocol(ma.P_CIRCUIT); err == nil {
			continue
		}
		if family != "" {
			code := ma.P_IP4
			if family == "ip6" {
				code = ma.P_IP6
			}
			if _, err := a.ValueForProtocol(code); err != nil {
				continue
			}
		}
		for _, p := range []struct {
			code int
			name string
		}{{ma.P_TCP, "tcp"}, {ma.P_UDP, "udp"}} {
			if v, err := a.ValueForProtocol(p.code); err == nil && v != "0" {
				if s := v + "/" + p.name; !slices.Contains(out, s) {
					out = append(out, s)
				}
			}
		}
	}
	slices.Sort(out)
	switch len(out) {
	case 0:
		return ""
	case 1:
		return out[0]
	}
	return strings.Join(out[:len(out)-1], ", ") + " und " + out[len(out)-1]
}

// portPhrase: „Port 4711/tcp“ bzw. „Ports 4711/tcp und 4711/udp“; ohne
// bekannte Ports allgemein „die Ports des Knotens“.
func portPhrase(addrs []ma.Multiaddr, family string) string {
	l := portList(addrs, family)
	switch {
	case l == "":
		return "die Ports des Knotens"
	case strings.Contains(l, " und "):
		return "Ports " + l
	}
	return "Port " + l
}
