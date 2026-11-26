// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Rainer Sestig. Kommerzielle Lizenz: siehe COMMERCIAL.md.

package p2p

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	lp2pproto "github.com/libp2p/go-libp2p/core/protocol"

	"github.com/rainer-sharp/Ai-Swarms.net/internal/protocol"
)

// Asynchrone Aufträge (spec/cooperation.md § 7): Ein Auftraggeber gibt einen
// ganzen Auftrag an einen vertrauten Peer, der ihn koordiniert. Jede Anfrage
// ist ein eigener Stream mit einer Nachricht hin und einer zurück.

// Coordinator nimmt Aufträge anderer an (implementiert im Paket node).
// from ist die Peer-ID des Auftraggebers; er sieht nur seine eigenen Aufträge.
type Coordinator interface {
	// SubmitJob startet den Auftrag; ein Fehler ist der Ablehnungsgrund.
	SubmitJob(from, network string, p protocol.JobSubmitPayload) error
	JobStatus(from, jobID string) protocol.JobStatusPayload
	CancelJob(from, jobID string) protocol.JobStatusPayload
}

// jobTimeout: Antwortzeit des Koordinators auf eine Nachricht.
const jobTimeout = 15 * time.Second

// ErrNotTrusted: Der Peer ist in keinem gemeinsamen Netz vertraut.
var ErrNotTrusted = errors.New("peer nicht vertraut")

func (n *Node) handleJob(s network.Stream) {
	from := s.Conn().RemotePeer()
	defer s.Close()
	if !n.Trusted(from) || n.opts.Coordinator == nil {
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
	c := n.opts.Coordinator
	var reply protocol.JobStatusPayload
	switch env.Type {
	case protocol.JobSubmit:
		var p protocol.JobSubmitPayload
		if err := env.Decode(&p); err != nil || p.JobID == "" {
			_ = s.Reset()
			return
		}
		reply = protocol.JobStatusPayload{JobID: p.JobID, Status: protocol.JobRunning}
		if err := c.SubmitJob(from.String(), n.NetworkID(), p); err != nil {
			reply.Status, reply.Error = protocol.JobRejected, err.Error()
		}
	case protocol.JobStatus, protocol.JobCancel:
		var q protocol.JobQueryPayload
		if err := env.Decode(&q); err != nil {
			_ = s.Reset()
			return
		}
		if env.Type == protocol.JobCancel {
			reply = c.CancelJob(from.String(), q.JobID)
		} else {
			reply = c.JobStatus(from.String(), q.JobID)
		}
	default:
		_ = s.Reset()
		return
	}
	typ := protocol.JobStatus
	if protocol.JobFinal(reply.Status) {
		typ = protocol.JobResult
	}
	if err := n.send(s, typ, reply.JobID, reply); err != nil {
		_ = s.Reset()
	}
}

// JobCall schickt eine Nachricht an einen koordinierenden Peer und liefert
// seine Antwort. typ: JOB_SUBMIT (payload JobSubmitPayload), JOB_STATUS oder
// JOB_CANCEL (payload JobQueryPayload).
func (n *Node) JobCall(ctx context.Context, p peer.ID, typ protocol.MsgType, jobID string, payload any) (protocol.JobStatusPayload, error) {
	var out protocol.JobStatusPayload
	err := n.call(ctx, p, protocol.ProtoJob, typ, jobID, payload, &out, protocol.JobStatus, protocol.JobResult)
	if err == nil && out.JobID != jobID {
		err = errors.New("antwort zu einem anderen auftrag")
	}
	return out, err
}

// call schickt eine Nachricht an einen vertrauten Peer und liest seine
// Antwort (einer der Typen in replies) nach out.
func (n *Node) call(ctx context.Context, p peer.ID, proto string, typ protocol.MsgType, jobID string, payload, out any, replies ...protocol.MsgType) error {
	if !n.Trusted(p) {
		return ErrNotTrusted
	}
	ctx, cancel := context.WithTimeout(ctx, jobTimeout)
	defer cancel()
	s, err := n.host.NewStream(allowLimited(ctx), p, lp2pproto.ID(proto))
	if err != nil {
		return fmt.Errorf("nicht erreichbar: %w", err)
	}
	defer s.Close()
	_ = s.SetDeadline(time.Now().Add(jobTimeout))
	env, err := protocol.Seal(n.id, typ, jobID, defaultTaskTTL, payload)
	if err != nil {
		return err
	}
	if err := writeMsg(s, env); err != nil {
		_ = s.Reset()
		return err
	}
	renv, err := readMsg(bufio.NewReader(s))
	if err != nil {
		return fmt.Errorf("keine antwort (koordiniert der peer für andere? cooperation.coordinate_for_others): %w", err)
	}
	if err := renv.Verify(time.Now(), n.replay); err != nil || renv.Sender != p.String() || !slices.Contains(replies, renv.Type) {
		return fmt.Errorf("ungültige antwort: %v", err)
	}
	return renv.Decode(out)
}

// Connected: Mit dem Peer besteht eine Verbindung in diesem Netz.
func (n *Node) Connected(p peer.ID) bool {
	return n.host.Network().Connectedness(p) == network.Connected
}
