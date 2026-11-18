// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Rainer Sestig. Kommerzielle Lizenz: siehe COMMERCIAL.md.

package p2p

import (
	"context"
	"log/slog"
	"math/rand/v2"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
	madns "github.com/multiformats/go-multiaddr-dns"
)

// DNS-Seeds (spec/p2p-layer.md § 2): Unter _dnsaddr.<domain> stehen
// TXT-Einträge der Form "dnsaddr=/ip4/…/tcp/4711/p2p/<peer-id>". Sie sind nur
// ein Hinweis; die Peer-ID prüft der Verbindungsaufbau.
const seedMaxDials = 8

var seedResolver = madns.DefaultResolver

func (n *Node) connectSeed(ctx context.Context, domain string) {
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	addrs, err := seedResolver.Resolve(cctx, ma.StringCast("/dnsaddr/"+domain))
	if err != nil {
		slog.Info("dns-seed nicht auflösbar", "seed", domain, "err", err)
		return
	}
	infos, err := peer.AddrInfosFromP2pAddrs(addrs...)
	if err != nil || len(infos) == 0 {
		slog.Info("dns-seed ohne gültige einträge", "seed", domain, "err", err)
		return
	}
	rand.Shuffle(len(infos), func(i, j int) { infos[i], infos[j] = infos[j], infos[i] })
	for _, ai := range infos[:min(len(infos), seedMaxDials)] {
		if ai.ID == n.host.ID() || n.connected(ai.ID) {
			continue
		}
		go func() {
			dctx, cancel := context.WithTimeout(allowLimited(ctx), 10*time.Second)
			defer cancel()
			if err := n.host.Connect(dctx, ai); err != nil {
				slog.Log(ctx, n.entryLogLevel(), "einstiegsknoten aus dns-seed nicht erreichbar", "seed", domain, "peer", ai.ID, "err", err)
			}
		}()
	}
}
