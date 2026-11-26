// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Rainer Sestig. Kommerzielle Lizenz: siehe COMMERCIAL.md.

package p2p

import (
	"context"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/rainer-sharp/Ai-Swarms.net/internal/protocol"
)

// Lease (spec/cooperation.md § 7): Der Koordinator eines asynchronen
// Auftrags schickt Prüfpunkte an einen Ersatzkoordinator und verlängert
// seinen Lease. Läuft der Lease ab, übernimmt der Ersatz.

// LeaseHolder hält Prüfpunkte als Ersatzkoordinator (implementiert im Paket
// node). from: Peer-ID des Koordinators.
type LeaseHolder interface {
	Checkpoint(from, network string, p protocol.LeaseCheckpointPayload) protocol.LeaseAckPayload
	Renew(from string, p protocol.LeaseRenewPayload) protocol.LeaseAckPayload
	Result(from string, p protocol.LeaseResultPayload) protocol.LeaseAckPayload
}

func (n *Node) handleLease(s network.Stream) {
	from := s.Conn().RemotePeer()
	defer s.Close()
	if !n.Trusted(from) || n.opts.Leases == nil {
		_ = s.Reset()
		return
	}
	var env *protocol.Envelope
	select {
	case env = <-n.incoming(s, from):
	case <-time.After(handshakeTimeout):
	}
	if env == nil {
		_ = s.Reset()
		return
	}
	h, f := n.opts.Leases, from.String()
	var ack protocol.LeaseAckPayload
	var err error
	switch env.Type {
	case protocol.LeaseCheckpoint:
		var p protocol.LeaseCheckpointPayload
		if err = env.Decode(&p); err == nil {
			ack = h.Checkpoint(f, n.NetworkID(), p)
		}
	case protocol.LeaseRenew:
		var p protocol.LeaseRenewPayload
		if err = env.Decode(&p); err == nil {
			ack = h.Renew(f, p)
		}
	case protocol.LeaseResult:
		var p protocol.LeaseResultPayload
		if err = env.Decode(&p); err == nil {
			ack = h.Result(f, p)
		}
	default:
		_ = s.Reset()
		return
	}
	if err != nil || n.send(s, protocol.LeaseAck, ack.JobID, ack) != nil {
		_ = s.Reset()
	}
}

// LeaseCall schickt eine Lease-Nachricht an den Ersatzkoordinator.
func (n *Node) LeaseCall(ctx context.Context, p peer.ID, typ protocol.MsgType, jobID string, payload any) (protocol.LeaseAckPayload, error) {
	var out protocol.LeaseAckPayload
	err := n.call(ctx, p, protocol.ProtoLease, typ, jobID, payload, &out, protocol.LeaseAck)
	return out, err
}

// epochTTL: So lange merkt sich ein Anbieter die Epoche eines Auftrags.
const epochTTL = 6 * time.Hour

// epochs merkt sich je Auftrag die höchste gesehene Lease-Epoche.
type epochs struct {
	mu   sync.Mutex
	seen map[string]epochSeen
}

type epochSeen struct {
	epoch int
	at    time.Time
}

// see: false, wenn für den Auftrag schon eine neuere Epoche bekannt ist.
func (e *epochs) see(id string, epoch int, now time.Time) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.seen == nil {
		e.seen = map[string]epochSeen{}
	}
	for k, v := range e.seen {
		if now.Sub(v.at) > epochTTL {
			delete(e.seen, k)
		}
	}
	if cur, ok := e.seen[id]; ok && epoch < cur.epoch {
		return false
	}
	if cur := e.seen[id]; epoch >= cur.epoch {
		e.seen[id] = epochSeen{epoch, now}
	}
	return true
}
