// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Rainer Sestig. Kommerzielle Lizenz: siehe COMMERCIAL.md.

package node

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/rainer-sharp/Ai-Swarms.net/internal/jobs"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/orchestrator"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/p2p"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/protocol"
)

// Lease-Koordinator (spec/cooperation.md § 7, Stufe 5): Wünscht der
// Auftraggeber einen Ersatzkoordinator, schickt der Koordinator ihm
// Prüfpunkte (Auftrag, bei decompose Plan und fertige Teilergebnisse) und
// verlängert seinen Lease. Bleibt die Verlängerung aus, übernimmt der Ersatz
// mit der nächsten Epoche und setzt am letzten Prüfpunkt fort; Anbieter
// lehnen danach Teilaufträge der alten Epoche ab. Erfährt der alte
// Koordinator von der Übernahme, bricht er ab.

// Zeiten des Leases; Variablen, damit Tests sie verkürzen können.
var (
	leaseDuration = 60 * time.Second
	leaseRenew    = 20 * time.Second
	leaseWatch    = 5 * time.Second
)

// maxLeasesHeld: So viele Prüfpunkte hält ein Ersatz höchstens.
const maxLeasesHeld = 8

// SubstituteAuto: Der Koordinator wählt den Ersatz unter seinen vertrauten Peers.
const SubstituteAuto = "auto"

// checkSubstitute prüft den gewünschten Ersatz: leer, auto oder eine Peer-ID.
func checkSubstitute(s string) error {
	if s == "" || s == SubstituteAuto {
		return nil
	}
	if _, err := peer.Decode(s); err != nil {
		return fmt.Errorf("%w: ersatzkoordinator %q ist weder auto noch eine peer-id", jobs.ErrInvalid, s)
	}
	return nil
}

// leaser hält beim Koordinator den Lease eines Auftrags.
type leaser struct {
	n         *Node
	owner, id string
	epoch     int
	jr        protocol.JobRequest
	requested string // Peer-ID oder auto
	cancel    context.CancelFunc

	mu     sync.Mutex
	cp     orchestrator.Checkpoint
	dirty  bool // neuer Prüfpunkt seit dem letzten Senden
	sub    peer.ID
	net    *p2p.Node
	bad    map[peer.ID]bool
	moved  atomic.Bool // vom Ersatz übernommen
	warned bool
}

// update merkt sich einen neuen Prüfpunkt (OnCheckpoint des Orchestrators).
func (l *leaser) update(cp orchestrator.Checkpoint) {
	l.mu.Lock()
	l.cp, l.dirty = cp, true
	l.mu.Unlock()
}

func (l *leaser) run(ctx context.Context) {
	t := time.NewTicker(leaseRenew)
	defer t.Stop()
	for {
		l.tick(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (l *leaser) checkpointPayload() protocol.LeaseCheckpointPayload {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.dirty = false
	return protocol.LeaseCheckpointPayload{JobID: l.id, Owner: l.owner, Epoch: l.epoch, Expiry: time.Now().Add(leaseDuration),
		Request: l.jr, Plan: l.cp.Plan, Results: l.cp.Results}
}

// tick: Ersatz suchen oder Lease verlängern (mit neuem Prüfpunkt, falls es einen gibt).
func (l *leaser) tick(ctx context.Context) {
	l.mu.Lock()
	sub, pn, dirty := l.sub, l.net, l.dirty
	l.mu.Unlock()
	if sub == "" {
		l.pick(ctx)
		return
	}
	var ack protocol.LeaseAckPayload
	var err error
	if dirty {
		ack, err = pn.LeaseCall(ctx, sub, protocol.LeaseCheckpoint, l.id, l.checkpointPayload())
	} else {
		ack, err = pn.LeaseCall(ctx, sub, protocol.LeaseRenew, l.id, protocol.LeaseRenewPayload{
			JobID: l.id, Owner: l.owner, Epoch: l.epoch, Expiry: time.Now().Add(leaseDuration)})
	}
	switch {
	case err != nil:
		// Ersatz nicht erreichbar: beim nächsten Mal einen anderen suchen
		// (ein ausdrücklich gewünschter wird erneut versucht).
		slog.Warn("ersatzkoordinator nicht erreichbar", "job", l.id, "ersatz", short(sub.String()), "err", err)
		l.mu.Lock()
		l.sub, l.dirty = "", true
		if l.requested == SubstituteAuto {
			l.bad[sub] = true
		}
		l.mu.Unlock()
	case ack.TakenOver:
		l.takenOver(sub, pn, ack.Epoch)
	case !ack.OK:
		l.mu.Lock()
		l.dirty = true // Ersatz kennt den Prüfpunkt nicht (Neustart): neu schicken
		l.mu.Unlock()
	}
}

// candidates: Ersatzkandidaten in der Reihenfolge der Versuche.
func (l *leaser) candidates() []peer.ID {
	if l.requested != SubstituteAuto {
		pid, _ := peer.Decode(l.requested)
		return []peer.ID{pid}
	}
	var out []peer.ID
	for _, pn := range l.n.Nets {
		for _, pi := range pn.Peers() {
			pid, err := peer.Decode(pi.PeerID)
			if err != nil || pi.PeerID == l.owner || l.bad[pid] || slices.Contains(out, pid) || !pn.Trusted(pid) {
				continue
			}
			out = append(out, pid)
		}
	}
	return out
}

func (l *leaser) pick(ctx context.Context) {
	l.mu.Lock()
	cands := l.candidates()
	l.mu.Unlock()
	var reasons []string
	for _, pid := range cands {
		pn := l.n.coordinatorNet(pid)
		if pn == nil {
			reasons = append(reasons, short(pid.String())+": nicht verbunden")
			continue
		}
		ack, err := pn.LeaseCall(ctx, pid, protocol.LeaseCheckpoint, l.id, l.checkpointPayload())
		if err == nil && ack.OK {
			l.mu.Lock()
			l.sub, l.net = pid, pn
			l.mu.Unlock()
			l.n.jobs.Update(l.owner, l.id, func(j *jobs.Job) { j.Substitute = pid.String() })
			slog.Info("ersatzkoordinator hält prüfpunkt", "job", l.id, "ersatz", short(pid.String()))
			return
		}
		if err == nil {
			err = errors.New(ack.Error)
		}
		reasons = append(reasons, short(pid.String())+": "+err.Error())
		l.mu.Lock()
		if l.requested == SubstituteAuto {
			l.bad[pid] = true
		}
		l.mu.Unlock()
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.dirty = true
	if !l.warned {
		l.warned = true
		slog.Warn("kein ersatzkoordinator", "job", l.id, "gründe", reasons)
	}
}

// takenOver: Der Ersatz hat übernommen. Dieser Knoten bricht seinen Lauf ab
// und vermerkt den Ersatz als Koordinator: Ein eigener Auftrag wird ab jetzt
// dort abgefragt; einer für andere bleibt als Verweis stehen, damit der
// Auftraggeber erfährt, wo er nachfragen muss (forwarded).
func (l *leaser) takenOver(sub peer.ID, pn *p2p.Node, epoch int) {
	if l.moved.Swap(true) {
		return
	}
	slog.Warn("auftrag vom ersatzkoordinator übernommen, breche ab", "job", l.id, "ersatz", short(sub.String()), "epoche", epoch)
	l.cancel()
	l.n.jobs.Update(l.owner, l.id, func(j *jobs.Job) {
		j.Coordinator, j.Network, j.Substitute, j.Epoch = sub.String(), pn.NetworkID(), "", epoch
	})
}

// forwarded: Auftrag eines anderen, den inzwischen der Ersatz koordiniert.
func forwarded(j jobs.Job) bool { return j.Owner != "" && j.Coordinator != "" }

// finish gibt dem Ersatz das Ergebnis, damit er nicht mehr übernimmt und es
// bereithält, falls dieser Knoten danach ausfällt.
func (l *leaser) finish(j jobs.Job) {
	l.mu.Lock()
	sub, pn := l.sub, l.net
	l.mu.Unlock()
	if sub == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if _, err := pn.LeaseCall(ctx, sub, protocol.LeaseResult, l.id, protocol.LeaseResultPayload{
		JobID: l.id, Owner: l.owner, Epoch: l.epoch, Result: statusPayload(j)}); err != nil {
		slog.Warn("ergebnis nicht an ersatzkoordinator übergeben", "job", l.id, "err", err)
	}
}

// heldLease: Prüfpunkt eines Auftrags, den dieser Knoten als Ersatz hält.
type heldLease struct {
	owner, network, coordinator string
	epoch                       int
	expiry, updated             time.Time
	cp                          protocol.LeaseCheckpointPayload
	taken                       bool                       // übernommen: läuft hier als Auftrag
	result                      *protocol.JobStatusPayload // abgeschlossen beim Koordinator
}

// leaseHolder implementiert p2p.LeaseHolder.
type leaseHolder struct{ n *Node }

func (n *Node) heldLease(owner, id string) *heldLease {
	n.heldMu.Lock()
	defer n.heldMu.Unlock()
	return n.held[owner+"/"+id]
}

// trustedAnywhere: Der Peer ist in einem Netz dieses Knotens vertraut.
func (n *Node) trustedAnywhere(id string) bool {
	pid, err := peer.Decode(id)
	if err != nil {
		return false
	}
	return slices.ContainsFunc(n.Nets, func(pn *p2p.Node) bool { return pn.Trusted(pid) })
}

func (h leaseHolder) Checkpoint(from, network string, p protocol.LeaseCheckpointPayload) protocol.LeaseAckPayload {
	n := h.n
	no := func(msg string) protocol.LeaseAckPayload { return protocol.LeaseAckPayload{JobID: p.JobID, Error: msg} }
	owner := p.Owner
	if owner == "" {
		owner = from // der Koordinator ist selbst der Auftraggeber
	}
	switch {
	case !n.Cfg.Cooperation.CoordinateForOthers:
		return no("dieser knoten koordiniert keine aufträge anderer (cooperation.coordinate_for_others)")
	case p.Request.PrivacyClass == protocol.PrivacyLocal || !p.Request.PrivacyClass.Valid():
		return no("freigabeklasse erlaubt keinen ersatzkoordinator")
	case n.isSelf(owner):
		return no("der auftraggeber kann nicht sein eigener ersatz sein")
	case !n.trustedAnywhere(owner):
		return no("auftraggeber hier nicht vertraut: er könnte das ergebnis nicht abholen")
	case p.JobID == "" || len(p.Request.Messages) == 0:
		return no("prüfpunkt ohne auftrag")
	}
	now := time.Now()
	n.heldMu.Lock()
	defer n.heldMu.Unlock()
	k := owner + "/" + p.JobID
	l := n.held[k]
	if l != nil && l.taken {
		return protocol.LeaseAckPayload{JobID: p.JobID, TakenOver: true, Epoch: l.epoch}
	}
	if l == nil {
		active := 0
		for _, x := range n.held {
			if !x.taken && x.result == nil {
				active++
			}
		}
		if active >= maxLeasesHeld {
			return no("ersatzkoordinator ausgelastet")
		}
		l = &heldLease{owner: owner}
		n.held[k] = l
	}
	if p.Epoch < l.epoch {
		return no("veraltete lease-epoche")
	}
	l.coordinator, l.network, l.epoch, l.cp, l.updated = from, network, p.Epoch, p, now
	l.expiry = minTime(p.Expiry, now.Add(2*leaseDuration))
	return protocol.LeaseAckPayload{JobID: p.JobID, OK: true, Epoch: l.epoch}
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func (h leaseHolder) Renew(from string, p protocol.LeaseRenewPayload) protocol.LeaseAckPayload {
	n := h.n
	owner := cmpStr(p.Owner, from)
	n.heldMu.Lock()
	defer n.heldMu.Unlock()
	l := n.held[owner+"/"+p.JobID]
	switch {
	case l == nil || l.coordinator != from:
		return protocol.LeaseAckPayload{JobID: p.JobID, Error: "kein prüfpunkt für diesen auftrag"}
	case l.taken:
		return protocol.LeaseAckPayload{JobID: p.JobID, TakenOver: true, Epoch: l.epoch}
	}
	now := time.Now()
	l.expiry, l.updated = minTime(p.Expiry, now.Add(2*leaseDuration)), now
	return protocol.LeaseAckPayload{JobID: p.JobID, OK: true, Epoch: l.epoch}
}

func (h leaseHolder) Result(from string, p protocol.LeaseResultPayload) protocol.LeaseAckPayload {
	n := h.n
	owner := cmpStr(p.Owner, from)
	n.heldMu.Lock()
	defer n.heldMu.Unlock()
	l := n.held[owner+"/"+p.JobID]
	if l == nil || l.coordinator != from || l.taken {
		return protocol.LeaseAckPayload{JobID: p.JobID, Error: "kein offener prüfpunkt für diesen auftrag"}
	}
	r := p.Result
	r.Substitute = ""
	l.result, l.updated = &r, time.Now()
	l.cp = protocol.LeaseCheckpointPayload{} // Auftrag und Teilergebnisse nicht länger halten
	return protocol.LeaseAckPayload{JobID: p.JobID, OK: true, Epoch: l.epoch}
}

func cmpStr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// watchLeases übernimmt Aufträge, deren Lease abgelaufen ist, und räumt auf.
func (n *Node) watchLeases(ctx context.Context) {
	t := time.NewTicker(leaseWatch)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		now := time.Now()
		var due []*heldLease
		n.heldMu.Lock()
		for k, l := range n.held {
			switch {
			case (l.taken || l.result != nil) && now.Sub(l.updated) > jobs.Retention:
				delete(n.held, k)
			case !l.taken && l.result == nil && now.After(l.expiry):
				l.taken, l.updated = true, now
				l.epoch++
				due = append(due, l)
			}
		}
		n.heldMu.Unlock()
		for _, l := range due {
			n.takeOver(l)
		}
	}
}

// takeOver setzt einen Auftrag am letzten Prüfpunkt fort, mit neuer Epoche.
func (n *Node) takeOver(l *heldLease) {
	req := n.requestFromJob(l.cp.Request)
	req.Job = &protocol.JobRef{ID: l.cp.JobID, Epoch: l.epoch}
	if len(l.cp.Plan) > 0 {
		req.Resume = &orchestrator.Checkpoint{Plan: l.cp.Plan, Results: l.cp.Results}
	}
	slog.Warn("lease abgelaufen, übernehme auftrag", "job", l.cp.JobID, "für", short(l.owner), "koordinator", short(l.coordinator),
		"epoche", l.epoch, "fertige_teilaufgaben", len(l.cp.Results))
	n.runJob(jobs.Job{ID: l.cp.JobID, Owner: l.owner, Network: l.network, Status: protocol.JobRunning, Created: time.Now(), Epoch: l.epoch},
		req, "")
}
