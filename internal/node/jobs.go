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
// vertrauten Peers, der ihn koordiniert; leer: dieser Knoten.
func (n *Node) SubmitJob(req orchestrator.Request, model, coordinator string) (jobs.Job, error) {
	t := n.Cfg.Tasks
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
		n.runJob(j, req)
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
	jr := protocol.JobRequest{
		Messages: req.Messages, Pattern: string(req.Pattern), PrivacyClass: req.Privacy, Skill: req.Skill, Agents: req.Agents,
		Tools: req.Tools, ToolChoice: req.ToolChoice, MaxTokens: cmpOr(req.Limits.MaxTokens, t.MaxTokens),
		MaxSubtasks: cmpOr(req.Limits.MaxSubtasks, t.MaxSubtasks), Deadline: req.Limits.Deadline,
		PseudonymizeTerms: req.PseudonymizeTerms, Trace: req.Trace,
	}
	if jr.PseudonymizeTerms == nil {
		jr.PseudonymizeTerms = t.PseudonymizeTerms
	}
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

// runJob führt einen Auftrag im Hintergrund aus, den dieser Knoten
// koordiniert (eigener oder für j.Owner).
func (n *Node) runJob(j jobs.Job, req orchestrator.Request) {
	ctx, cancel := context.WithDeadline(n.jobCtx, req.Limits.Deadline)
	n.jobs.Add(j, cancel)
	go func() {
		defer cancel()
		ans, err := n.Ask(ctx, req)
		n.jobs.Update(j.Owner, j.ID, func(x *jobs.Job) {
			x.Answer = ans
			if err != nil {
				x.Status, x.Error = protocol.JobFailed, err.Error()
				return
			}
			x.Status, x.Content, x.ToolCalls = protocol.JobDone, ans.Content, ans.ToolCalls
		})
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
	pid, err := peer.Decode(j.Coordinator)
	if err != nil {
		return j, fmt.Errorf("%w: %v", jobs.ErrCoordinator, err)
	}
	st, err := n.net(j.Network).JobCall(ctx, pid, protocol.JobStatus, id, protocol.JobQueryPayload{JobID: id})
	if err != nil {
		return j, fmt.Errorf("%w: %v", jobs.ErrCoordinator, err)
	}
	return n.applyStatus(j, st), nil
}

// applyStatus übernimmt die Antwort des Koordinators.
func (n *Node) applyStatus(j jobs.Job, st protocol.JobStatusPayload) jobs.Job {
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
	pid, err := peer.Decode(j.Coordinator)
	if err != nil {
		return j, false, fmt.Errorf("%w: %v", jobs.ErrCoordinator, err)
	}
	st, err := n.net(j.Network).JobCall(ctx, pid, protocol.JobCancel, id, protocol.JobQueryPayload{JobID: id})
	if err != nil {
		return j, false, fmt.Errorf("%w: abbruch nicht zugestellt: %v", jobs.ErrCoordinator, err)
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
	deadline := time.Now().Add(n.jobDeadline())
	if !r.Deadline.IsZero() && r.Deadline.Before(deadline) {
		deadline = r.Deadline
	}
	req := orchestrator.Request{
		Messages: r.Messages, Pattern: orchestrator.Pattern(r.Pattern), Privacy: r.PrivacyClass, Skill: r.Skill, Agents: r.Agents,
		Tools: r.Tools, ToolChoice: r.ToolChoice, Trace: r.Trace, PseudonymizeTerms: r.PseudonymizeTerms,
		Limits: protocol.Limits{MaxTokens: r.MaxTokens, MaxSubtasks: r.MaxSubtasks, Deadline: deadline},
	}
	n.runJob(jobs.Job{ID: p.JobID, Owner: from, Network: network, Status: protocol.JobRunning, Created: time.Now()}, req)
	slog.Info("koordiniere auftrag", "job", p.JobID, "für", short(from), "netz", network)
	return nil
}

func (c jobCoordinator) JobStatus(from, jobID string) protocol.JobStatusPayload {
	j, ok := c.n.jobs.Get(from, jobID)
	if !ok {
		return protocol.JobStatusPayload{JobID: jobID, Status: protocol.JobUnknown}
	}
	return statusPayload(j)
}

func (c jobCoordinator) CancelJob(from, jobID string) protocol.JobStatusPayload {
	j, ok := c.n.jobs.Cancel(from, jobID)
	if !ok {
		return protocol.JobStatusPayload{JobID: jobID, Status: protocol.JobUnknown}
	}
	return statusPayload(j)
}

func statusPayload(j jobs.Job) protocol.JobStatusPayload {
	st := protocol.JobStatusPayload{JobID: j.ID, Status: j.Status, Error: j.Error, Finished: j.Finished}
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
