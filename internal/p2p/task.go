// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Rainer Sestig und Mitwirkende.

package p2p

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	lp2pproto "github.com/libp2p/go-libp2p/core/protocol"
	"github.com/libp2p/go-libp2p/p2p/net/swarm"

	"github.com/rainer-sharp/Ai-Swarms.net/internal/orchestrator"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/protocol"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/receipt"
)

const (
	handshakeTimeout = 30 * time.Second
	receiptTimeout   = 30 * time.Second
	defaultTaskTTL   = 5 * time.Minute
)

func digest(s string) string {
	h := sha256.Sum256([]byte(s))
	return "sha256:" + hex.EncodeToString(h[:])
}

// incoming liest Nachrichten eines Streams in einen Kanal und prüft jede.
func (n *Node) incoming(s network.Stream, from peer.ID) <-chan *protocol.Envelope {
	ch := make(chan *protocol.Envelope, 4)
	go func() {
		defer close(ch)
		r := bufio.NewReader(s)
		for {
			env, err := readMsg(r)
			if err != nil {
				return
			}
			if err := env.Verify(time.Now(), n.replay); err != nil || env.Sender != from.String() {
				slog.Warn("ungültige nachricht verworfen", "peer", from, "err", err)
				return
			}
			ch <- env
		}
	}()
	return ch
}

func (n *Node) send(s network.Stream, typ protocol.MsgType, taskID string, payload any) error {
	env, err := protocol.Seal(n.id, typ, taskID, defaultTaskTTL, payload)
	if err != nil {
		return err
	}
	_ = s.SetWriteDeadline(time.Now().Add(handshakeTimeout))
	return writeMsg(s, env)
}

// --- Anbieterseite ---

func (n *Node) acquireStream(p peer.ID) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.streams[p] >= maxStreamsPeer {
		return false
	}
	n.streams[p]++
	return true
}

func (n *Node) releaseStream(p peer.ID) {
	n.mu.Lock()
	n.streams[p]--
	n.mu.Unlock()
}

func (n *Node) handleTask(s network.Stream) {
	from := s.Conn().RemotePeer()
	if !n.acquireStream(from) {
		_ = s.Reset()
		return
	}
	defer n.releaseStream(from)
	defer s.Close()

	in := n.incoming(s, from)
	var env *protocol.Envelope
	select {
	case env = <-in:
	case <-time.After(handshakeTimeout):
	}
	if env == nil || env.Type != protocol.TaskPropose {
		_ = s.Reset()
		return
	}
	var p protocol.ProposePayload
	if err := env.Decode(&p); err != nil {
		_ = s.Reset()
		return
	}
	reject := func(reason string) {
		_ = n.send(s, protocol.TaskReject, env.TaskID, protocol.RejectPayload{SubtaskID: p.SubtaskID, Reason: reason})
	}
	if n.limited(from) && !n.taskLimit.allow(from, time.Now()) {
		reject(rejectBusy) // vor jeder weiteren Prüfung, ohne Ressourcenmanager
		return
	}

	trusted := n.Trusted(from)
	switch {
	case p.Limits.PrivacyClass == protocol.PrivacyLocal || !p.Limits.PrivacyClass.Valid():
		reject("freigabeklasse erlaubt keine fremdverarbeitung")
		return
	case !trusted && p.Limits.PrivacyClass != protocol.PrivacyPublic && p.Limits.PrivacyClass != protocol.PrivacyReduced:
		reject("nur für vertrauten kreis bestimmt")
		return
	case !trusted && !n.opts.AcceptsPublic:
		// accept_public_tasks gilt je Netz (spec/p2p-layer.md § 9), für
		// public und für pseudonymisierte reduced-Aufträge.
		reject("keine öffentlichen aufträge in diesem netz")
		return
	case !p.Limits.Deadline.IsZero() && time.Now().After(p.Limits.Deadline):
		reject("deadline überschritten")
		return
	case p.Limits.MaxDepth > 0 && p.Limits.Depth > p.Limits.MaxDepth:
		reject("maximale delegationstiefe erreicht")
		return
	case p.Job != nil && !n.epochs.see(p.Job.ID, p.Job.Epoch, time.Now()):
		// Ein Ersatzkoordinator hat den Auftrag übernommen (spec/cooperation.md § 7.3).
		reject(rejectStaleEpoch)
		return
	}
	var agent *protocol.AgentInfo
	for _, a := range n.opts.Provider.Offer() {
		if a.AgentID == p.AgentID {
			agent = &a
		}
	}
	if agent == nil {
		reject("agent nicht angeboten")
		return
	}
	// Werkzeug-Aufrufe führt der Auftraggeber aus und prüft sie auch dort; der
	// Anbieter rechnet nur und gefährdet sich damit nicht.
	if withTools := len(p.Tools) > 0 && string(p.Tools) != "null" && string(p.Tools) != "[]"; withTools && !agent.Tools {
		reject("agent beherrscht keine werkzeuge")
		return
	}
	// Audio: Stimmen sind personenbezogene Daten, deshalb nie public; nur an
	// Agenten, die Audio verarbeiten (spec/protocol-v0.1.md § 4.2).
	if p.Audio != nil {
		switch {
		case p.Limits.PrivacyClass == protocol.PrivacyPublic || !trusted:
			// Auch nicht als reduced: eine Aufnahme lässt sich nicht pseudonymisieren.
			reject("audio nur im vertrauten kreis")
			return
		case !slices.Contains(agent.Skills, orchestrator.SkillTranscribe):
			reject("agent verarbeitet kein audio")
			return
		case len(p.Audio.Data) == 0 || len(p.Audio.Data) > protocol.MaxAudioBytes:
			reject("aufnahme leer oder zu groß")
			return
		}
	}
	// Bilder: nur an Agenten, die Bilder verstehen; bei reduced nie von
	// Fremden, weil sich ein Bild nicht pseudonymisieren lässt
	// (spec/protocol-v0.1.md § 4.2).
	if slices.ContainsFunc(p.Messages, func(m protocol.ChatMessage) bool { return len(m.Images) > 0 }) {
		switch size := protocol.ImageBytes(p.Messages); {
		case p.Limits.PrivacyClass == protocol.PrivacyReduced && !trusted:
			reject("bilder bei reduced nur im vertrauten kreis")
			return
		case !slices.Contains(agent.Modalities, "image"):
			reject("agent verarbeitet keine bilder")
			return
		case size > protocol.MaxImageBytes || !protocol.ValidImages(p.Messages):
			reject("bilder zu groß oder in unbekanntem format")
			return
		}
	}
	slot, err := n.opts.Provider.Admit(!trusted)
	if err != nil {
		// Der genaue Grund (Energie, Last, Zeitfenster) bleibt lokal.
		slog.Info("teilauftrag abgelehnt", "peer", from, "grund", err)
		reject(rejectNoCapacity)
		return
	}
	defer slot.Release()

	lease := time.Now().Add(defaultTaskTTL)
	if !p.Limits.Deadline.IsZero() && p.Limits.Deadline.Before(lease) {
		lease = p.Limits.Deadline
	}
	if err := n.send(s, protocol.TaskAccept, env.TaskID, protocol.AcceptPayload{SubtaskID: p.SubtaskID, LeaseExpiry: lease}); err != nil {
		return
	}

	ctx, cancel := context.WithDeadline(slot.Context(), lease)
	defer cancel()
	// Abbruch durch den Koordinator oder Verbindungsende
	stopWatch := make(chan struct{})
	receipts := make(chan *protocol.Envelope, 1)
	go func() {
		for {
			select {
			case m, ok := <-in:
				if !ok {
					cancel()
					return
				}
				switch m.Type {
				case protocol.TaskCancel:
					cancel()
					return
				case protocol.TaskReceipt:
					receipts <- m
					return
				}
			case <-stopWatch:
				return
			}
		}
	}()

	task := orchestrator.Task{TaskID: env.TaskID, SubtaskID: p.SubtaskID, Role: p.Role, Skill: p.Skill,
		Messages: p.Messages, Limits: p.Limits, JSONSchema: p.JSONSchema, Tools: p.Tools, ToolChoice: p.ToolChoice, Audio: p.Audio}
	var prog *progress
	if p.Stream && p.Audio == nil {
		prog = &progress{send: func(pp protocol.ProgressPayload) error {
			return n.send(s, protocol.TaskProgress, env.TaskID, pp)
		}, subtask: p.SubtaskID}
		task.OnDelta = prog.add
	}
	res, err := n.opts.Provider.RunLocal(ctx, p.AgentID, task)
	if prog != nil {
		prog.flush()
	}
	if err != nil {
		slog.Info("teilauftrag nicht ausgeführt", "peer", from, "agent", p.AgentID, "err", err)
		// Nur Gründe ohne Inhalte gehen an den Auftraggeber.
		reason := "ausführung fehlgeschlagen"
		switch {
		case context.Cause(slot.Context()) != nil:
			reason = "abbruch durch anbieter"
		case errors.Is(err, protocol.ErrTokenLimit):
			reason = protocol.ReasonTokenLimit
		}
		_ = n.send(s, protocol.TaskCancel, env.TaskID, protocol.CancelPayload{SubtaskID: p.SubtaskID, Reason: reason})
		close(stopWatch)
		return
	}
	res.TaskID, res.SubtaskID, res.ProducerPeer = env.TaskID, p.SubtaskID, n.host.ID().String()
	res.Evidence = append(res.Evidence, protocol.Evidence{Type: "content", Digest: digest(res.Result.Content)})
	if err := n.send(s, protocol.TaskResult, env.TaskID, res); err != nil {
		close(stopWatch)
		return
	}

	// Quittung: Koordinator signiert zuerst, wir gegenzeichnen.
	var renv *protocol.Envelope
	select {
	case renv = <-receipts:
	case <-time.After(receiptTimeout):
		close(stopWatch)
		slog.Info("keine quittung erhalten", "peer", from, "task", env.TaskID)
		return
	}
	var rc receipt.Receipt
	if err := renv.Decode(&rc); err != nil {
		return
	}
	if rc.Provider != n.host.ID().String() || rc.Coordinator != from.String() || rc.SubtaskID != p.SubtaskID ||
		rc.TaskID != env.TaskID || rc.ResultDigest != digest(res.Result.Content) ||
		rc.CompletionTokens != res.Usage.CompletionTokens || rc.PromptTokens != res.Usage.PromptTokens {
		slog.Warn("quittung passt nicht zur leistung", "peer", from)
		return
	}
	if err := rc.SignAsProvider(n.id); err != nil {
		return
	}
	if err := n.opts.Receipts.Add(rc); err != nil {
		slog.Warn("quittung ungültig", "peer", from, "err", err)
		return
	}
	_ = n.send(s, protocol.TaskReceipt, env.TaskID, rc)
}

// progressEvery: Höchstens so oft schickt der Anbieter einen Zwischenstand;
// jeder ist eine signierte Nachricht.
const progressEvery = 100 * time.Millisecond

// progressMaxBytes: Ab so viel gesammeltem Text geht ein Zwischenstand sofort.
const progressMaxBytes = 16 << 10

// progress sammelt gestreamten Text und schickt ihn gebündelt als
// TASK_PROGRESS. Läuft in der Goroutine des Teilauftrags, daher ohne Sperre.
type progress struct {
	send            func(protocol.ProgressPayload) error
	subtask         string
	content, reason strings.Builder
	last            time.Time
	failed          bool
}

func (p *progress) add(content, reasoning string) {
	p.content.WriteString(content)
	p.reason.WriteString(reasoning)
	if time.Since(p.last) >= progressEvery || p.content.Len()+p.reason.Len() >= progressMaxBytes {
		p.flush()
	}
}

func (p *progress) flush() {
	if p.failed || p.content.Len()+p.reason.Len() == 0 {
		return
	}
	err := p.send(protocol.ProgressPayload{SubtaskID: p.subtask, Status: protocol.ProgressDelta,
		Delta: p.content.String(), Reasoning: p.reason.String()})
	p.content.Reset()
	p.reason.Reset()
	p.last = time.Now()
	p.failed = err != nil // Verbindung weg: das Ergebnis scheitert ohnehin
}

// --- Koordinatorseite ---

var ErrRejected = errors.New("teilauftrag abgelehnt")

// Absagegründe, die nur vorübergehende Auslastung bedeuten.
const (
	rejectNoCapacity = "keine kapazität" // Ressourcenmanager
	rejectBusy       = "busy"            // Ratenlimit (spec/p2p-layer.md § 8)
	rejectStaleEpoch = "veraltete lease-epoche"
)

// newTaskStream öffnet einen Auftrags-Stream. Trifft er auf eine Wählsperre
// (dial backoff), etwa nach einem gescheiterten gleichzeitigen Neuwählen
// beider Seiten (redialLost), hebt er sie für diesen Peer auf und versucht es
// einmal neu: Der Auftrag ist wichtiger als die Schonung des Netzes.
func (n *Node) newTaskStream(ctx context.Context, p peer.ID) (network.Stream, error) {
	s, err := n.host.NewStream(allowLimited(ctx), p, lp2pproto.ID(protocol.ProtoTask))
	if err == nil || !errors.Is(err, swarm.ErrDialBackoff) {
		return s, err
	}
	sw, ok := n.host.Network().(*swarm.Swarm)
	if !ok {
		return nil, err
	}
	slog.Debug("wählsperre aufgehoben, neuer versuch", "peer", p)
	sw.Backoff().Clear(p)
	return n.host.NewStream(allowLimited(ctx), p, lp2pproto.ID(protocol.ProtoTask))
}

// Execute schickt einen Teilauftrag an einen anderen Knoten (implementiert
// orchestrator.Executor für entfernte Agenten).
func (n *Node) Execute(ctx context.Context, a orchestrator.Agent, t orchestrator.Task) (protocol.Result, error) {
	pid, err := peer.Decode(a.PeerID)
	if err != nil {
		return protocol.Result{}, err
	}
	if n.member != nil && (!n.Trusted(pid) || !n.memberCanTask()) {
		return protocol.Result{}, errors.New("mitgliedsnetz: aufträge nur zwischen zugelassenen mitgliedern")
	}
	s, err := n.newTaskStream(ctx, pid)
	if err != nil {
		return protocol.Result{}, err
	}
	defer s.Close()
	var wmu sync.Mutex // Abbruch und Quittung können gleichzeitig schreiben wollen
	send := func(typ protocol.MsgType, payload any) error {
		wmu.Lock()
		defer wmu.Unlock()
		return n.send(s, typ, t.TaskID, payload)
	}
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			_ = send(protocol.TaskCancel, protocol.CancelPayload{SubtaskID: t.SubtaskID, Reason: "abbruch durch koordinator"})
			_ = s.Reset()
		case <-done:
		}
	}()

	limits := t.Limits
	limits.Depth++
	if err := send(protocol.TaskPropose, protocol.ProposePayload{
		SubtaskID: t.SubtaskID, Role: t.Role, Skill: t.Skill, AgentID: a.ID,
		Messages: t.Messages, Limits: limits, JSONSchema: t.JSONSchema,
		Tools: t.Tools, ToolChoice: t.ToolChoice, Audio: t.Audio, Stream: t.OnDelta != nil, Job: t.Job,
	}); err != nil {
		return protocol.Result{}, err
	}
	in := n.incoming(s, pid)
	state := protocol.StateOffered
	for {
		var env *protocol.Envelope
		select {
		case env = <-in:
		case <-ctx.Done():
			return protocol.Result{}, ctx.Err()
		}
		if env == nil {
			return protocol.Result{}, fmt.Errorf("verbindung zu %s beendet (zustand %s)", pid, state)
		}
		if env.TaskID != t.TaskID {
			return protocol.Result{}, errors.New("antwort zu fremdem auftrag")
		}
		switch env.Type {
		case protocol.TaskReject:
			var r protocol.RejectPayload
			_ = env.Decode(&r)
			if r.Reason == rejectNoCapacity || r.Reason == rejectBusy {
				// ausgelastet: der Orchestrator wartet und versucht es erneut
				return protocol.Result{}, fmt.Errorf("%w: %s: %w", ErrRejected, r.Reason, orchestrator.ErrBusy)
			}
			return protocol.Result{}, fmt.Errorf("%w: %s", ErrRejected, r.Reason)
		case protocol.TaskAccept:
			if state, err = state.Next(protocol.StateAccepted); err != nil {
				return protocol.Result{}, err
			}
			state = protocol.StateRunning
		case protocol.TaskProgress:
			var pp protocol.ProgressPayload
			if t.OnDelta != nil && state == protocol.StateRunning && env.Decode(&pp) == nil &&
				pp.SubtaskID == t.SubtaskID && pp.Status == protocol.ProgressDelta {
				t.OnDelta(pp.Delta, pp.Reasoning)
			}
		case protocol.TaskCancel:
			var c protocol.CancelPayload
			_ = env.Decode(&c)
			if c.Reason == protocol.ReasonTokenLimit {
				return protocol.Result{}, fmt.Errorf("anbieter brach ab: %w", protocol.ErrTokenLimit)
			}
			return protocol.Result{}, fmt.Errorf("anbieter brach ab: %s", c.Reason)
		case protocol.TaskResult:
			if state != protocol.StateRunning {
				return protocol.Result{}, errors.New("ergebnis ohne annahme")
			}
			var res protocol.Result
			if err := env.Decode(&res); err != nil {
				return protocol.Result{}, err
			}
			if res.SubtaskID != t.SubtaskID || res.ProducerPeer != pid.String() {
				return protocol.Result{}, errors.New("ergebnis passt nicht zum teilauftrag")
			}
			n.exchangeReceipt(send, in, pid, a, t, res)
			return res, nil
		}
	}
}

func (n *Node) exchangeReceipt(send func(protocol.MsgType, any) error, in <-chan *protocol.Envelope, pid peer.ID, a orchestrator.Agent, t orchestrator.Task, res protocol.Result) {
	rc := receipt.Receipt{
		TaskID: t.TaskID, SubtaskID: t.SubtaskID,
		Coordinator: n.host.ID().String(), Provider: pid.String(),
		Role: string(t.Role), AgentID: a.ID, ModelFamily: a.Family, Status: res.Result.Status,
		FinishedAt: time.Now().UTC().Truncate(time.Millisecond), DurationMs: res.Usage.DurationMs,
		PromptTokens: res.Usage.PromptTokens, CompletionTokens: res.Usage.CompletionTokens,
		ResultDigest: digest(res.Result.Content),
	}
	if err := rc.SignAsCoordinator(n.id); err != nil {
		return
	}
	if err := send(protocol.TaskReceipt, rc); err != nil {
		return
	}
	select {
	case env := <-in:
		if env == nil || env.Type != protocol.TaskReceipt {
			return
		}
		var signed receipt.Receipt
		if err := env.Decode(&signed); err != nil {
			return
		}
		signed.CoordinatorSig = rc.CoordinatorSig // eigene Signatur ist maßgeblich
		if err := n.opts.Receipts.Add(signed); err != nil {
			slog.Warn("gegengezeichnete quittung ungültig", "peer", pid, "err", err)
		}
	case <-time.After(receiptTimeout):
	}
}
