// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Rainer Sestig. Kommerzielle Lizenz: siehe COMMERCIAL.md.

package p2p

import (
	"slices"
	"time"

	bhost "github.com/libp2p/go-libp2p/p2p/host/basic"
)

// Portfreigabe per UPnP oder NAT-PMP (spec/p2p-layer.md § 3), Opt-in über
// port_mapping. libp2p fordert die Freigaben mit kurzer Laufzeit an und
// erneuert sie laufend; endet der Knoten, laufen sie beim Router aus.

// newNATManager ist austauschbar, damit Tests keinen echten Router ansprechen.
var newNATManager = bhost.NewNATManager

// mappingHintAfter: so lange darf die Suche nach einem Router dauern, bevor
// der Status einen Hinweis zeigt.
const mappingHintAfter = time.Minute

// MappingInfo für `swarmainet status`.
type MappingInfo struct {
	Gateway bool     `json:"gateway_found"` // Router mit UPnP/NAT-PMP gefunden
	Mapped  []string `json:"mapped"`        // freigegebene externe Adressen
}

func (n *Node) MappingInfo() *MappingInfo {
	if n.nat == nil {
		return nil
	}
	info := &MappingInfo{Gateway: n.nat.HasDiscoveredNAT(), Mapped: []string{}}
	listen, _ := n.host.Network().InterfaceListenAddresses()
	for _, a := range append(n.host.Network().ListenAddresses(), listen...) {
		if m := n.nat.GetMapping(a); m != nil && !slices.Contains(info.Mapped, m.String()) {
			info.Mapped = append(info.Mapped, m.String())
		}
	}
	slices.Sort(info.Mapped)
	return info
}

// mappingHint meldet eine Portfreigabe, die nicht zustande kommt.
func (n *Node) mappingHint() string {
	m := n.MappingInfo()
	switch {
	case m == nil || time.Since(n.started) < mappingHintAfter:
		return ""
	case !m.Gateway:
		return "port_mapping ist an, aber kein Router mit UPnP oder NAT-PMP gefunden (im Router abgeschaltet oder nicht vorhanden)."
	case len(m.Mapped) == 0:
		return "Router mit UPnP/NAT-PMP gefunden, aber er gibt keinen Port frei. " +
			"FritzBox: beim Gerät „Selbstständige Portfreigaben erlauben“ setzen; bei DS-Lite/CGNAT hilft eine IPv4-Freigabe nicht."
	}
	return ""
}
