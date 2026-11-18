// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Rainer Sestig. Kommerzielle Lizenz: siehe COMMERCIAL.md.

package p2p

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"

	"github.com/rainer-sharp/Ai-Swarms.net/internal/tracker"
)

// Tracker nach spec/p2p-layer.md § 2.3: eigene Adressen hinterlegen und
// Adressen anderer abholen. Im privaten Netz verschlüsselt, sodass der
// Tracker weder Netz noch Mitglieder erfährt.

// trackerFetchBelow: unter so vielen verbundenen Peers holt der Knoten
// Einträge ab; darüber tragen Peer-Austausch und DHT.
const trackerFetchBelow = 8

func (n *Node) trackerNet() string {
	if n.sealer != nil {
		return n.sealer.Net()
	}
	return n.opts.NetworkID
}

// publishTrackers hinterlegt die aktuellen Adressen bei allen Trackern.
func (n *Node) publishTrackers(ctx context.Context) {
	var addrs []string
	for _, a := range n.host.Addrs() { // im offenen Netz schon ohne private Adressen
		addrs = append(addrs, a.String())
	}
	if len(addrs) == 0 {
		return // nicht anwählbar, Eintrag nutzlos
	}
	rec, err := tracker.NewRecord(n.id.Priv, n.opts.NetworkID, addrs, tracker.MaxTTL)
	if err != nil {
		slog.Warn("tracker-eintrag nicht erzeugt", "err", err)
		return
	}
	e := tracker.Entry{Net: n.trackerNet(), ID: n.host.ID().String()}
	if n.sealer != nil {
		e.ID = n.sealer.ID(n.host.ID())
		e.Record, err = n.sealer.Seal(rec)
	} else {
		e.Record, err = json.Marshal(rec)
	}
	if err != nil {
		slog.Warn("tracker-eintrag nicht erzeugt", "err", err)
		return
	}
	for _, u := range n.opts.Trackers {
		go func() {
			cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
			defer cancel()
			if err := tracker.Publish(cctx, u, e); err != nil {
				slog.Info("tracker nicht erreichbar", "tracker", u, "err", err)
			}
		}()
	}
}

// fetchTrackers holt Einträge, prüft sie und verbindet sich mit bis zu
// pexMaxDials neuen Peers.
func (n *Node) fetchTrackers(ctx context.Context) {
	dials := 0
	for _, u := range n.opts.Trackers {
		cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		raws, _, err := tracker.Fetch(cctx, u, n.trackerNet())
		cancel()
		if err != nil {
			slog.Info("tracker nicht erreichbar", "tracker", u, "err", err)
			continue
		}
		for _, raw := range raws {
			id, addrs, ok := n.trackerRecord(raw)
			if !ok {
				continue
			}
			n.host.Peerstore().AddAddrs(id, addrs, pexAddrTTL)
			n.remember(id)
			if !n.connected(id) && dials < pexMaxDials {
				dials++
				go n.dialKnown(ctx, id)
			}
		}
	}
}

// trackerRecord entschlüsselt (privat) bzw. liest (offen) einen Eintrag und
// prüft Signatur, Ablaufzeit und Adressen. Fremde Einträge, Müll und der
// eigene Eintrag werden verworfen.
func (n *Node) trackerRecord(raw json.RawMessage) (peer.ID, []ma.Multiaddr, bool) {
	var rec *tracker.Record
	if n.sealer != nil {
		r, err := n.sealer.Open(raw)
		if err != nil {
			return "", nil, false
		}
		rec = r
	} else if err := json.Unmarshal(raw, &rec); err != nil || rec == nil {
		return "", nil, false
	}
	if err := rec.Verify(n.opts.NetworkID, time.Now()); err != nil {
		return "", nil, false
	}
	id, err := peer.Decode(rec.PeerID)
	if err != nil || id == n.host.ID() {
		return "", nil, false
	}
	var addrs []ma.Multiaddr
	for _, s := range rec.Addrs {
		// Eigene /p2p-Angabe ist nicht vorgesehen; Circuit-Adressen enden auf /p2p-circuit.
		if a, err := ma.NewMultiaddr(s); err == nil && n.shareable(a) && !strings.HasSuffix(s, "/p2p/"+rec.PeerID) {
			addrs = append(addrs, a)
		}
	}
	return id, addrs, len(addrs) > 0
}

// syncTrackers erneuert den eigenen Eintrag alle tracker.Refresh. Abgeholt
// wird ebenso oft, solange wenige Peers verbunden sind, und ohne jeden Peer
// bei jedem Aufruf (jede Minute), damit ein zuerst gestarteter Knoten die
// später kommenden bald findet.
func (n *Node) syncTrackers(ctx context.Context, lastPub, lastFetch *time.Time) {
	if len(n.opts.Trackers) == 0 {
		return
	}
	if time.Since(*lastPub) >= tracker.Refresh {
		*lastPub = time.Now()
		go n.publishTrackers(ctx)
	}
	peers := len(n.host.Network().Peers())
	if peers == 0 || peers < trackerFetchBelow && time.Since(*lastFetch) >= tracker.Refresh {
		*lastFetch = time.Now()
		go n.fetchTrackers(ctx)
	}
}
