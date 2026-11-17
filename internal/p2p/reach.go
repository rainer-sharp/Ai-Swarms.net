// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Rainer Sestig. Kommerzielle Lizenz: siehe COMMERCIAL.md.

package p2p

import (
	"context"
	"log/slog"

	"github.com/libp2p/go-libp2p/core/event"
	ma "github.com/multiformats/go-multiaddr"
)

// Erreichbarkeit je Adressfamilie (spec/p2p-schicht.md § 3, § 10). AutoNAT
// prüft jede Adresse einzeln; hinter DS-Lite ist typischerweise IPv6
// erreichbar, IPv4 nie.

// family: ip4 oder ip6 nach der ersten Komponente; Relay-Adressen zählen nicht.
func family(a ma.Multiaddr) string {
	if isCircuit(a) || len(a) == 0 {
		return ""
	}
	switch a[0].Protocol().Code {
	case ma.P_IP4, ma.P_DNS4:
		return "ip4"
	case ma.P_IP6, ma.P_DNS6:
		return "ip6"
	}
	return ""
}

// familyReach fasst die Prüfergebnisse zusammen: public, sobald eine Adresse
// der Familie erreichbar ist, private, wenn geprüfte Adressen alle scheitern,
// sonst unknown. Familien ohne geprüfte Adresse fehlen.
func familyReach(e event.EvtHostReachableAddrsChanged) map[string]string {
	out := map[string]string{}
	set := func(list []ma.Multiaddr, state string) {
		for _, a := range list {
			f := family(a)
			if f == "" {
				continue
			}
			switch {
			case out[f] == "public":
			case state == "public", out[f] == "":
				out[f] = state
			case state == "private" && out[f] == "unknown":
				out[f] = state
			}
		}
	}
	set(e.Reachable, "public")
	set(e.Unreachable, "private")
	set(e.Unknown, "unknown")
	return out
}

// watchAddrReach übernimmt die Prüfergebnisse je Adresse.
func (n *Node) watchAddrReach(ctx context.Context, sub event.Subscription) {
	defer sub.Close()
	for {
		select {
		case <-ctx.Done():
			return
		case e, ok := <-sub.Out():
			if !ok {
				return
			}
			r := familyReach(e.(event.EvtHostReachableAddrsChanged))
			n.mu.Lock()
			changed := !mapsEqual(n.famReach, r)
			n.famReach = r
			n.mu.Unlock()
			if changed {
				slog.Info("erreichbarkeit je adressfamilie", "netz", n.opts.NetworkID, "ip4", r["ip4"], "ip6", r["ip6"])
			}
		}
	}
}

func mapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// FamilyReachability: Erreichbarkeit je Familie (ip4, ip6) im offenen Netz;
// nil, solange nichts geprüft ist.
func (n *Node) FamilyReachability() map[string]string {
	if n.private {
		return nil
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if len(n.famReach) == 0 {
		return nil
	}
	out := make(map[string]string, len(n.famReach))
	for k, v := range n.famReach {
		out[k] = v
	}
	return out
}

// familyHint erklärt eine teilweise Erreichbarkeit, typisch für DS-Lite.
func (n *Node) familyHint() string {
	r := n.FamilyReachability()
	switch {
	case r["ip4"] == "private" && r["ip6"] == "public":
		return "Über IPv4 nicht erreichbar (DS-Lite/CGNAT oder Firewall), über IPv6 schon. " +
			"Knoten ohne IPv6 erreichen diesen Knoten nur über ein Relay."
	case r["ip4"] == "private" && r["ip6"] == "":
		return "Über IPv4 nicht erreichbar und keine geprüfte IPv6-Adresse. Bei DS-Lite/CGNAT hilft nur IPv6 " +
			"(Freigabe in der Firewall des Routers) oder ein Relay."
	case r["ip6"] == "private" && r["ip4"] == "public":
		return "Über IPv6 nicht erreichbar, über IPv4 schon: Die IPv6-Firewall des Routers lässt Port 4711 nicht durch."
	}
	return ""
}
