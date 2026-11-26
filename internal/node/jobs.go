// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Rainer Sestig. Kommerzielle Lizenz: siehe COMMERCIAL.md.

package node

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/rainer-sharp/Ai-Swarms.net/internal/jobs"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/orchestrator"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/p2p"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/protocol"
)

// Asynchrone Aufträge (spec/cooperation.md § 7, Stufe 5): Ein eigener Auftrag
// läuft im Hintergrund, koordiniert von diesem Knoten oder von einem
// vertrauten Peer; das Ergebnis wird später abgeholt. Mit
// cooperation.coordinate_for_others koordiniert dieser Knoten auch Aufträge
// vertrauter Peers.

// Grenzen für Aufträge, die dieser Knoten für andere koordiniert.
const (
	maxJobsForOthers  = 4
	maxJobsPerPeer    = 2
	defaultJobTimeout = time.Hour
)

func (n *Node) jobDeadline() time.Duration {
	if s := n.Cfg.Tasks.JobDeadlineS; s > 0 {
		return time.Duration(s) * time.Second
	}
	return defaultJobTimeout
}

// isSelf: Die Peer-ID gehört diesem Knoten (in irgendeinem Netz).
func (n *Node) isSelf(id string) bool {
	for _, pn := range n.Nets {
		if pn.ID().String() == id {
			return true
		}
	}
	return false
}

// SubmitJob gibt einen eigenen Auftrag ab. coordinator: Peer-ID eines
// vertrauten Peers, der ihn koordiniert; leer: dieser Knoten. substitute:
// Ersatzkoordinator (Peer-ID oder auto), der Prüfpunkte hält und bei einem
// Ausfall übernimmt; leer: keiner.
func (n *Node) SubmitJob(req orchestrator.Request, model, coordinator, substitute string) (jobs.Job, error) {
	t := n.Cfg.Tasks
	if err := checkSubstitute(substitute); err != nil {
		return jobs.Job{}, err
	}
	if substitute != "" && req.Privacy == protocol.PrivacyLocal {
		return jobs.Job{}, fmt.Errorf("%w: freigabeklasse local verlässt diesen knoten nicht, auch nicht als prüfpunkt", orchestrator.ErrPrivacy)
	}
	if req.Privacy == "" {
		req.Privacy = protocol.PrivacyClass(t.PrivacyClass)
	}
	if !req.Privacy.Valid() {
		return jobs.Job{}, fmt.Errorf("%w: privacy_class %q unbekannt", jobs.ErrInvalid, req.Privacy)
	}
	now := time.Now()
	if req.Limits.Deadline.IsZero() {
		req.Limits.Deadline = now.Add(n.jobDeadline())
	}
	req.Stream = nil
	j := jobs.Job{ID: jobs.NewID(), Model: model, Status: protocol.JobRunning, Created: now}
	if coordinator == "" || n.isSelf(coordinator) {
		n.runJob(j, req, substitute)
		return j, nil
	}

	// Ein anderer Knoten koordiniert: Er sieht den ganzen Auftrag.
	if req.Privacy == protocol.PrivacyLocal {
		return jobs.Job{}, fmt.Errorf("%w: freigabeklasse local verlässt diesen knoten nicht, auch nicht zur koordination", orchestrator.ErrPrivacy)
	}
	pid, err := peer.Decode(coordinator)
	if err != nil {
		return jobs.Job{}, fmt.Errorf("%w: koordinator %q ist keine peer-id", jobs.ErrInvalid, coordinator)
	}
	pn := n.coordinatorNet(pid)
	if pn == nil {
		return jobs.Job{}, fmt.Errorf("%w: %s ist nicht verbunden oder in keinem gemeinsamen netz vertraut", jobs.ErrCoordinator, short(coordinator))
	}
	jr := n.jobRequest(req)
	jr.Substitute = substitute
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	st, err := pn.JobCall(ctx, pid, protocol.JobSubmit, j.ID, protocol.JobSubmitPayload{JobID: j.ID, Request: jr})
	if err != nil {
		return jobs.Job{}, fmt.Errorf("%w: %v", jobs.ErrCoordinator, err)
	}
	if st.Status != protocol.JobRunning {
		return jobs.Job{}, fmt.Errorf("%w: %s lehnt ab: %s", jobs.ErrCoordinator, short(coordinator), st.Error)
	}
	j.Coordinator, j.Network = coordinator, pn.NetworkID()
	n.jobs.Add(j, nil)
	slog.Info("auftrag an koordinator übergeben", "job", j.ID, "koordinator", short(coordinator), "netz", j.Network)
	return j, nil
}

// coordinatorNet: Netz, in dem der Peer verbunden und vertraut ist.
func (n *Node) coordinatorNet(p peer.ID) *p2p.Node {
	for _, pn := range n.Nets {
		if pn.Trusted(p) && pn.Connected(p) {
			return pn
		}
	}
	return nil
}

func cmpOr(a, b int) int {
	if a != 0 {
		return a
	}
	return b
}

// jobRequest: der Auftrag mit den Standardwerten dieses Knotens, wie er an
// einen Koordinator oder in einen Prüfpunkt geht.
func (n *Node) jobRequest(req orchestrator.Request) protocol.JobRequest {
	t := n.Cfg.Tasks
	jr := protocol.JobRequest{
		Messages: req.Messages, Pattern: string(req.Pattern), PrivacyClass: req.Privacy, Skill: req.Skill, Agents: req.Agents,
		Tools: req.Tools, ToolChoice: req.ToolChoice, MaxTokens: cmpOr(req.Limits.MaxTokens, t.MaxTokens),
		MaxSubtasks: cmpOr(req.Limits.MaxSubtasks, t.MaxSubtasks), Deadline: req.Limits.Deadline,
		PseudonymizeTerms: req.PseudonymizeTerms, Trace: req.Trace,
	}
	if jr.PseudonymizeTerms == nil {
		jr.PseudonymizeTerms = t.PseudonymizeTerms
	}
	return jr
}

// requestFromJob: Auftrag eines anderen Knotens; die Frist höchstens so
// lang wie die eigene.
func (n *Node) requestFromJob(r protocol.JobRequest) orchestrator.Request {
	deadline := time.Now().Add(n.jobDeadline())
	if !r.Deadline.IsZero() && r.Deadline.Before(deadline) {
		deadline = r.Deadline
	}
	return orchestrator.Request{
		Messages: r.Messages, Pattern: orchestrator.Pattern(r.Pattern), Privacy: r.PrivacyClass, Skill: r.Skill, Agents: r.Agents,
		Tools: r.Tools, ToolChoice: r.ToolChoice, Trace: r.Trace, PseudonymizeTerms: r.PseudonymizeTerms,
		Limits: protocol.Limits{MaxTokens: r.MaxTokens, MaxSubtasks: r.MaxSubtasks, Deadline: deadline},
	}
}

// runJob führt einen Auftrag im Hintergrund aus, den dieser Knoten
// koordiniert (eigener oder für j.Owner). Mit substitute hält ein
// Ersatzkoordinator Prüfpunkte (lease.go).
func (n *Node) runJob(j jobs.Job, req orchestrator.Request, substitute string) {
	ctx, cancel := context.WithDeadline(n.jobCtx, req.Limits.Deadline)
	var l *leaser
	if substitute != "" && req.Privacy != protocol.PrivacyLocal {
		if j.Epoch == 0 {
			j.Epoch = 1
		}
		req.Job = &protocol.JobRef{ID: j.ID, Epoch: j.Epoch}
		l = &leaser{n: n, owner: j.Owner, id: j.ID, epoch: j.Epoch, jr: n.jobRequest(req), requested: substitute,
			cancel: cancel, bad: map[peer.ID]bool{}}
		req.OnCheckpoint = l.update
	}
	n.jobs.Add(j, cancel)
	if l != nil {
		go l.run(ctx)
	}
	go func() {
		defer cancel()
		ans, err := n.Ask(ctx, req)
		if l != nil && (l.moved.Load() || n.jobCtx.Err() != nil) {
			// Übernommen, oder dieser Knoten wird beendet: Der Ersatz macht
			// weiter, er bekommt kein Ergebnis und der Auftrag bleibt hier
			// offen (beim Neustart verweist er auf den Ersatz, jobs.Open).
			return
		}
		final, _ := n.jobs.Update(j.Owner, j.ID, func(x *jobs.Job) {
			x.Answer = ans
			if err != nil {
				x.Status, x.Error = protocol.JobFailed, err.Error()
				return
			}
			x.Status, x.Content, x.ToolCalls = protocol.JobDone, ans.Content, ans.ToolCalls
		})
		if l != nil {
			l.finish(final)
		}
		slog.Info("auftrag abgeschlossen", "job", j.ID, "für", short(j.Owner), "err", err)
	}()
}

// Job liefert einen eigenen Auftrag. Koordiniert ihn ein anderer Peer und
// läuft er noch, fragt Job dort nach; ist der Koordinator nicht erreichbar,
// kommt der letzte bekannte Stand mit einem Fehler, der jobs.ErrCoordinator trägt.
func (n *Node) Job(ctx context.Context, id string) (jobs.Job, error) {
	j, ok := n.jobs.Get("", id)
	if !ok {
		return jobs.Job{}, jobs.ErrUnknown
	}
	if j.Coordinator == "" || protocol.JobFinal(j.Status) {
		return j, nil
	}
	st, err := n.askJob(ctx, j, protocol.JobStatus)
	if err != nil {
		return j, err
	}
	return n.applyStatus(j, st), nil
}

// jobCall fragt einen Peer nach einem eigenen Auftrag.
func (n *Node) jobCall(ctx context.Context, peerID, network string, typ protocol.MsgType, id string) (protocol.JobStatusPayload, error) {
	pid, err := peer.Decode(peerID)
	if err != nil {
		return protocol.JobStatusPayload{}, err
	}
	pn := n.coordinatorNet(pid)
	if pn == nil {
		pn = n.net(network)
	}
	return pn.JobCall(ctx, pid, typ, id, protocol.JobQueryPayload{JobID: id})
}

// askJob fragt den Koordinator; antwortet er nicht oder kennt er den
// Auftrag nicht mehr, den Ersatzkoordinator.
func (n *Node) askJob(ctx context.Context, j jobs.Job, typ protocol.MsgType) (protocol.JobStatusPayload, error) {
	st, err := n.jobCall(ctx, j.Coordinator, j.Network, typ, j.ID)
	sub := cmpStr(j.Substitute, st.Substitute) // der Koordinator verweist nach einer Übernahme auf den Ersatz
	if (err != nil || st.Status == protocol.JobUnknown) && sub != "" && sub != j.Coordinator {
		if st2, err2 := n.jobCall(ctx, sub, j.Network, typ, j.ID); err2 == nil && st2.Status != protocol.JobUnknown {
			if st2.Substitute == "" {
				st2.Substitute = sub
			}
			return st2, nil
		}
	}
	if err != nil {
		return st, fmt.Errorf("%w: %v", jobs.ErrCoordinator, err)
	}
	return st, nil
}

// applyStatus übernimmt die Antwort des Koordinators.
func (n *Node) applyStatus(j jobs.Job, st protocol.JobStatusPayload) jobs.Job {
	if st.Substitute != "" && st.Substitute != j.Substitute {
		j, _ = n.jobs.Update("", j.ID, func(x *jobs.Job) { x.Substitute = st.Substitute })
	}
	if st.Status == protocol.JobRunning {
		return j
	}
	out, _ := n.jobs.Update("", j.ID, func(x *jobs.Job) {
		switch {
		case st.Status == protocol.JobUnknown:
			x.Status, x.Error = protocol.JobFailed, "der koordinator kennt den auftrag nicht mehr (neu gestartet oder abgelaufen)"
		case protocol.JobFinal(st.Status):
			x.Status, x.Error, x.Content, x.ToolCalls, x.Finished = st.Status, st.Error, st.Content, st.ToolCalls, st.Finished
			if len(st.Answer) > 0 {
				var a orchestrator.Answer
				if json.Unmarshal(st.Answer, &a) == nil {
					x.Answer = &a
				}
			}
		}
	})
	return out
}

// ListJobs listet die eigenen Aufträge, die neuesten zuerst.
func (n *Node) ListJobs() []jobs.Job { return n.jobs.List("") }

// CancelJob bricht einen laufenden eigenen Auftrag ab (auch beim
// Koordinator) oder löscht einen abgeschlossenen. removed: gelöscht.
func (n *Node) CancelJob(ctx context.Context, id string) (j jobs.Job, removed bool, err error) {
	j, ok := n.jobs.Get("", id)
	if !ok {
		return jobs.Job{}, false, jobs.ErrUnknown
	}
	if protocol.JobFinal(j.Status) {
		return j, n.jobs.Remove("", id), nil
	}
	if j.Coordinator == "" {
		j, _ = n.jobs.Cancel("", id)
		return j, false, nil
	}
	st, err := n.askJob(ctx, j, protocol.JobCancel)
	if err != nil {
		return j, false, fmt.Errorf("abbruch nicht zugestellt: %w", err)
	}
	return n.applyStatus(j, st), false, nil
}

// jobCoordinator nimmt Aufträge vertrauter Peers an (p2p.Coordinator).
type jobCoordinator struct{ n *Node }

func (c jobCoordinator) SubmitJob(from, network string, p protocol.JobSubmitPayload) error {
	n := c.n
	r := p.Request
	switch {
	case !n.Cfg.Cooperation.CoordinateForOthers:
		return errors.New("dieser knoten koordiniert keine aufträge anderer (cooperation.coordinate_for_others)")
	case r.PrivacyClass == protocol.PrivacyLocal || !r.PrivacyClass.Valid():
		return errors.New("freigabeklasse erlaubt keine koordination durch andere")
	case len(r.Messages) == 0:
		return errors.New("auftrag ohne nachrichten")
	case n.jobs.RunningFor(from) >= maxJobsPerPeer:
		return fmt.Errorf("schon %d laufende aufträge dieses peers", maxJobsPerPeer)
	case n.jobs.RunningFor("") >= maxJobsForOthers:
		return errors.New("koordinator ausgelastet")
	}
	if _, dup := n.jobs.Get(from, p.JobID); dup {
		return errors.New("auftrag existiert schon")
	}
	if err := checkSubstitute(r.Substitute); err != nil {
		return err
	}
	n.runJob(jobs.Job{ID: p.JobID, Owner: from, Network: network, Status: protocol.JobRunning, Created: time.Now()},
		n.requestFromJob(r), r.Substitute)
	slog.Info("koordiniere auftrag", "job", p.JobID, "für", short(from), "netz", network)
	return nil
}

func (c jobCoordinator) JobStatus(from, jobID string) protocol.JobStatusPayload {
	j, ok := c.n.jobs.Get(from, jobID)
	switch {
	case !ok:
		return c.n.heldStatus(from, jobID, false)
	case forwarded(j):
		return protocol.JobStatusPayload{JobID: jobID, Status: protocol.JobUnknown, Substitute: j.Coordinator}
	}
	return statusPayload(j)
}

func (c jobCoordinator) CancelJob(from, jobID string) protocol.JobStatusPayload {
	if j, ok := c.n.jobs.Get(from, jobID); ok && forwarded(j) {
		return protocol.JobStatusPayload{JobID: jobID, Status: protocol.JobUnknown, Substitute: j.Coordinator}
	}
	j, ok := c.n.jobs.Cancel(from, jobID)
	if !ok {
		return c.n.heldStatus(from, jobID, true)
	}
	return statusPayload(j)
}

// heldStatus: Stand eines Auftrags, für den dieser Knoten nur Ersatz ist.
// cancel: Der Auftraggeber bricht ab; dann übernimmt der Ersatz nicht mehr.
func (n *Node) heldStatus(owner, jobID string, cancel bool) protocol.JobStatusPayload {
	n.heldMu.Lock()
	defer n.heldMu.Unlock()
	l := n.held[owner+"/"+jobID]
	switch {
	case l == nil || l.taken:
		return protocol.JobStatusPayload{JobID: jobID, Status: protocol.JobUnknown}
	case l.result != nil:
		return *l.result
	case cancel:
		l.result = &protocol.JobStatusPayload{JobID: jobID, Status: protocol.JobCanceled, Error: "abgebrochen", Finished: time.Now()}
		l.cp, l.updated = protocol.LeaseCheckpointPayload{}, time.Now()
		return *l.result
	}
	return protocol.JobStatusPayload{JobID: jobID, Status: protocol.JobRunning}
}

func statusPayload(j jobs.Job) protocol.JobStatusPayload {
	st := protocol.JobStatusPayload{JobID: j.ID, Status: j.Status, Error: j.Error, Finished: j.Finished, Substitute: j.Substitute}
	if j.Status == protocol.JobDone {
		st.Content, st.ToolCalls = j.Content, j.ToolCalls
	}
	if j.Answer != nil {
		st.Answer, _ = json.Marshal(j.Answer)
	}
	return st
}

// short kürzt eine Peer-ID für Logs und Meldungen.
func short(id string) string {
	if len(id) > 12 {
		return id[len(id)-8:]
	}
	return id
}
