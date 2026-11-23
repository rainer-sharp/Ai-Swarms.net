// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Rainer Sestig. Kommerzielle Lizenz: siehe COMMERCIAL.md.

// Package orchestrator führt einen Auftrag nach einem Kooperationsmuster aus:
// single, parallel, Entwurf + Prüfer + Synthese (MVP-Muster) oder
// parallel mit anschließender Prüfung.
// Grundregel: Ersteller und Prüfer stammen aus verschiedenen Modellfamilien.
package orchestrator

import (
	"cmp"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	mrand "math/rand/v2"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/rainer-sharp/Ai-Swarms.net/internal/protocol"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/pseudo"
)

// Agent ist ein Modell auf einem Knoten, lokal oder entfernt.
type Agent struct {
	ID         string   `json:"agent_id"`
	PeerID     string   `json:"peer_id"`
	Network    string   `json:"network,omitempty"` // Netz des entfernten Agenten
	Local      bool     `json:"local"`
	Trusted    bool     `json:"trusted"`
	Family     string   `json:"model_family"`
	Skills     []string `json:"skills"`
	ContextMax int      `json:"context_max"`
	Strength   int      `json:"strength,omitempty"` // 1–10, 0 = unbekannt
	Tools      bool     `json:"tools,omitempty"`    // beherrscht Werkzeug-Aufrufe
	// ToolsRestricted: Dieser Agent sieht nur die Werkzeuge aus ToolsAllow und
	// keinen Verlauf mit anderen Werkzeugen.
	ToolsRestricted bool     `json:"tools_restricted,omitempty"`
	ToolsAllow      []string `json:"tools_allow,omitempty"`
	// Busy: der Peer meldete zuletzt keine freien Slots.
	Busy bool `json:"busy,omitempty"`
	// Slots: zuletzt gemeldete freie Slots des Peers (nur entfernte Agenten).
	Slots int `json:"slots,omitempty"`
}

func (a Agent) key() string { return a.PeerID + "/" + a.ID }

type Task struct {
	TaskID     string
	SubtaskID  string
	Role       protocol.Role
	Skill      string
	Messages   []protocol.ChatMessage
	Limits     protocol.Limits
	JSONSchema string
	// Werkzeuge der aufrufenden Anwendung (OpenAI-Format), unverändert.
	Tools      json.RawMessage
	ToolChoice json.RawMessage
	// Audio: Aufnahme für eine Transkription.
	Audio *protocol.Audio
	// OnDelta: nicht nil, wenn der Schritt gestreamt wird. Der Executor meldet
	// neuen Antworttext (content) und Text der Denkphase des Modells
	// (reasoning); maßgeblich bleibt das Ergebnis.
	OnDelta func(content, reasoning string)
	// flush gibt nach dem Teilauftrag aus, was das Zurücktauschen der
	// Platzhalter beim Streaming noch zurückhält (reduced).
	flush func()
}

type Executor interface {
	Execute(ctx context.Context, a Agent, t Task) (protocol.Result, error)
}

type Directory interface {
	Agents() []Agent
}

type Pattern string

const (
	PatternSingle      Pattern = "single"
	PatternParallel    Pattern = "parallel"
	PatternDraftVerify Pattern = "draft_verify_synthesize"
	// PatternParallelVerify: parallel, danach Prüfung und ggf. Überarbeitung.
	PatternParallelVerify Pattern = "parallel_verify"
	// PatternCascade: erst ein günstiges Modell, Prüfung durch eine andere
	// Familie, nur bei Mängeln löst das beste Modell neu (spec/cooperation.md § 5).
	PatternCascade Pattern = "cascade"
)

// Patterns: alle Muster, die Run kennt.
var Patterns = []Pattern{PatternSingle, PatternParallel, PatternDraftVerify, PatternParallelVerify, PatternCascade}

// Choice beschreibt, zwischen welchen Mustern ein Auftrag wählen kann.
type Choice struct {
	Skill    string
	Feasible []Pattern // mit den verfügbaren Agenten besetzbar
	Default  Pattern   // Muster nach Regel (tasks.pattern)
}

type Request struct {
	Messages []protocol.ChatMessage
	Pattern  Pattern
	Privacy  protocol.PrivacyClass
	Skill    string // leer: Router entscheidet
	Limits   protocol.Limits
	// Agents begrenzt die Auswahl auf diese Agent-IDs (leer: alle). Für
	// Messungen und Ablationen, z. B. um Modellwechsel auf einem Server zu vermeiden.
	Agents []string
	// Trace liefert die Texte der Zwischenschritte in Step.Output mit (für
	// Messungen: Trefferquote des Prüfers, Wirkung der Synthese).
	Trace bool
	// Tools: Werkzeuge der aufrufenden Anwendung, etwa eines Coding-Agenten.
	// Mit Werkzeugen gilt immer das Muster single, nur werkzeugfähige Agenten
	// und nie die Freigabeklasse public (Werkzeug-Aufrufe führt die Anwendung
	// auf dem eigenen Rechner aus).
	Tools      json.RawMessage
	ToolChoice json.RawMessage
	// Audio: Transkription statt Chat. Läuft immer als single mit der
	// Fähigkeit audio.transcribe und nie in der Freigabeklasse public.
	Audio *protocol.Audio
	// PseudonymizeTerms: eigene Begriffe, die bei reduced für fremde Knoten
	// immer ersetzt werden (tasks.pseudonymize_terms).
	PseudonymizeTerms []string
	// Stream: nicht nil, wenn die Antwort gestreamt wird. Run ruft es aus
	// mehreren Goroutinen auf, nie gleichzeitig. Audio wird nicht gestreamt,
	// Werkzeug-Aufrufe nicht als Zwischenstand (nur der Text).
	Stream func(Event)
}

// EventKind: Art eines Zwischenstands beim Streaming.
type EventKind int

const (
	// EventReasoning: Text der Denkphase: Überschriften der Schritte, Entwürfe,
	// Prüfurteile und das Nachdenken des Modells, das die Antwort schreibt.
	EventReasoning EventKind = iota
	// EventContent: Text der Antwort, nur aus einem Schritt, nach dem keine
	// Prüfung mehr folgt.
	EventContent
	// EventRestart: Der bisher gestreamte Antworttext ist verworfen (Schritt
	// fehlgeschlagen); es folgt eine neue Fassung.
	EventRestart
)

// Event ist ein Zwischenstand beim Streaming.
type Event struct {
	Kind EventKind
	Text string
}

// SkillTranscribe: Fähigkeit von Transkriptionsmodellen.
const SkillTranscribe = "audio.transcribe"

type Step struct {
	Role       protocol.Role `json:"role"`
	AgentID    string        `json:"agent_id"`
	PeerID     string        `json:"peer_id"`
	Family     string        `json:"model_family"`
	Status     string        `json:"status"`
	DurationMs int64         `json:"duration_ms"`
	// Tokens laut Backend; 0 bei Fehlschlag.
	PromptTokens     int    `json:"prompt_tokens"`
	CompletionTokens int    `json:"completion_tokens"`
	Error            string `json:"error,omitempty"`
	// Verdict: Urteil eines Prüfers ("ok" oder "issues"), nur bei gültigem
	// Urteil; Grundlage für das Kompetenzprofil (spec/cooperation.md § 2.2).
	Verdict string `json:"verdict,omitempty"`
	// Output: Text des Schritts, nur bei Request.Trace und Erfolg.
	Output string `json:"output,omitempty"`
}

// Answer trägt Herkunft, Prüfurteil und Einschränkungen.
type Answer struct {
	TaskID    string              `json:"task_id"`
	Content   string              `json:"-"`
	ToolCalls []protocol.ToolCall `json:"-"`
	Pattern   Pattern             `json:"pattern"`
	Skill     string              `json:"skill"`
	// SelectedBy: wer das Muster gewählt hat: request, rules oder bandit.
	SelectedBy string `json:"selected_by,omitempty"`
	// Escalated: In der Kaskade hat das beste Modell neu gelöst.
	Escalated   bool     `json:"escalated,omitempty"`
	Verdict     string   `json:"verdict,omitempty"`
	Issues      []string `json:"issues,omitempty"`
	Limitations []string `json:"limitations,omitempty"`
	Steps       []Step   `json:"steps"`
}

type Orchestrator struct {
	Dir      Directory
	Exec     Executor
	Parallel int // Zahl unabhängiger Lösungen im Muster parallel
	// Default: Muster, wenn die Anfrage keines nennt (leer: draft_verify_synthesize).
	Default Pattern
	// Choose wählt das Muster, wenn die Anfrage keines nennt (Bandit, spec/
	// cooperation.md § 3); nil: Default.
	Choose func(Choice) Pattern
	// Rate liefert die gemessene Bewertung eines Agenten in einer Fähigkeit
	// (Kompetenzprofil); ok=false, wenn noch keine vorliegt.
	Rate func(a Agent, skill string) (r float64, ok bool)

	load loadTracker
}

// loadTracker verteilt Teilaufträge auf gleich geeignete Agenten: wer gerade
// weniger zu tun hat und länger nicht dran war, kommt zuerst.
type loadTracker struct {
	mu     sync.Mutex
	seq    uint64
	agents map[string]*agentLoad
	peers  map[string]int // laufende Teilaufträge je Peer
}

type agentLoad struct {
	inflight int
	lastPick uint64 // 0 = noch nie gewählt
	full     bool   // eigene laufende Teilaufträge belegen alle gemeldeten Slots des Peers
}

func (l *loadTracker) get(a Agent) agentLoad {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out agentLoad
	if s := l.agents[a.key()]; s != nil {
		out = *s
	}
	out.full = !a.Local && a.Slots > 0 && l.peers[a.PeerID] >= a.Slots
	return out
}

// peerInflight liefert die eigenen laufenden Teilaufträge bei einem Peer.
func (l *loadTracker) peerInflight(peer string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.peers[peer]
}

// begin markiert einen Teilauftrag als laufend; der Rückgabewert beendet ihn.
func (l *loadTracker) begin(a Agent) func() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.agents == nil {
		l.agents = map[string]*agentLoad{}
		l.peers = map[string]int{}
	}
	s := l.agents[a.key()]
	if s == nil {
		s = &agentLoad{}
		l.agents[a.key()] = s
	}
	l.seq++
	s.inflight++
	s.lastPick = l.seq
	l.peers[a.PeerID]++
	return func() {
		l.mu.Lock()
		s.inflight--
		l.peers[a.PeerID]--
		l.mu.Unlock()
	}
}

var ErrNoAgent = errors.New("kein passender agent verfügbar")

// ErrPrivacy: Kein Agent ist wählbar, weil die Freigabeklasse der Anfrage alle
// bekannten ausschließt. Ein solcher Fehler ist zugleich ErrNoAgent; die
// Ursache liegt in der Anfrage, das Gateway antwortet mit 400.
var ErrPrivacy = errors.New("freigabeklasse schließt alle agenten aus")

// privacyError trägt die erklärende Meldung und gilt als ErrNoAgent und ErrPrivacy.
type privacyError struct{ msg string }

func (e privacyError) Error() string        { return e.msg }
func (e privacyError) Is(target error) bool { return target == ErrNoAgent || target == ErrPrivacy }

// ErrToolsNotAllowed: Die Werkzeug-Aufrufe eines Modells führt die Anwendung auf
// dem eigenen Rechner aus. Ohne Agent, dem die Werkzeuge freigegeben sind, läuft
// ein solcher Auftrag nicht (spec/privacy-classes.md).
var ErrToolsNotAllowed = errors.New("kein agent mit freigabe für diese werkzeuge")

// hasTools: Bringt die Anfrage Werkzeuge mit?
func hasTools(req Request) bool {
	t := strings.TrimSpace(string(req.Tools))
	return t != "" && t != "null" && t != "[]"
}

// ErrBusy: Der Anbieter ist gerade ausgelastet (keine freien Slots,
// Ratenlimit). Anders als andere Absagen lohnt hier ein späterer Versuch.
var ErrBusy = errors.New("anbieter ausgelastet")

// Warten auf ausgelastete Anbieter: wachsende Abstände mit Zufall, insgesamt
// höchstens busyMaxWait und nie über die Deadline des Auftrags hinaus.
var (
	busyFirstWait = time.Second
	busyMaxStep   = 8 * time.Second
	busyMaxWait   = 2 * time.Minute
)

// maxVerifyTries begrenzt die Prüfversuche, wenn Urteile unbrauchbar sind.
const maxVerifyTries = 2

func newID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// run hält den Zustand eines Auftragsgraphen.
type run struct {
	o      *Orchestrator
	req    Request
	taskID string
	ans    *Answer
	n      int // verbrauchte Teilaufträge
	mu     sync.Mutex

	// Pseudonymisierung bei reduced, angelegt beim ersten fremden Agenten.
	pzMu  sync.Mutex
	pz    *pseudo.Mapping
	pzErr error

	emitMu   sync.Mutex
	reasoned bool // schon Text in der Denkphase
	blank    bool // Denkphase endet mit einer Leerzeile
}

// target: wohin der Text eines Schritts beim Streaming geht.
type target int

const (
	toNone      target = iota // nur die Überschrift
	toReasoning               // Denkphase: ungeprüfter Text
	toContent                 // Antwort: danach folgt keine Prüfung
)

// out beschreibt das Streaming eines Schritts. label: Überschrift in der
// Denkphase (leer: keine). Der Nullwert streamt nichts.
type out struct {
	label string
	to    target
}

// streaming: Diese Anfrage wird gestreamt. Mit Werkzeugen läuft nur der
// Text live; die Aufrufe kommen erst mit dem geprüften Ergebnis
// (checkToolCalls), damit kein nicht freigegebener Aufruf den Client erreicht.
func (r *run) streaming() bool {
	return r.req.Stream != nil && r.req.Audio == nil
}

func (r *run) emit(k EventKind, s string) {
	if !r.streaming() || s == "" && k != EventRestart {
		return
	}
	r.emitMu.Lock()
	defer r.emitMu.Unlock()
	if k == EventReasoning {
		r.reasoned, r.blank = true, strings.HasSuffix(s, "\n\n")
	}
	r.req.Stream(Event{Kind: k, Text: s})
}

// note schreibt einen eigenen Absatz in die Denkphase.
func (r *run) note(s string) {
	r.emitMu.Lock()
	sep := r.reasoned && !r.blank
	r.emitMu.Unlock()
	if sep {
		s = "\n\n" + s
	}
	r.emit(EventReasoning, s)
}

// agentName: kurzer Name für die Denkphase (Modell ohne Backend-Präfix).
func agentName(a Agent) string {
	return a.ID[strings.LastIndex(a.ID, "/")+1:]
}

func (o *Orchestrator) Run(ctx context.Context, req Request) (*Answer, error) {
	if !req.Privacy.Valid() {
		return nil, fmt.Errorf("privacy_class %q unbekannt", req.Privacy)
	}
	if req.Audio != nil {
		// Stimmen sind personenbezogene Daten: Audio nur bis trusted.
		if req.Privacy == protocol.PrivacyPublic {
			return nil, privacyError{"audio geht nicht an fremde knoten (freigabeklasse public); höchstens trusted"}
		}
		if len(req.Audio.Data) == 0 || len(req.Audio.Data) > protocol.MaxAudioBytes {
			return nil, fmt.Errorf("aufnahme leer oder größer als %d bytes", protocol.MaxAudioBytes)
		}
		req.Skill, req.Pattern, req.Tools = SkillTranscribe, PatternSingle, nil
		// Eine Aufnahme lässt sich nicht pseudonymisieren: reduced bleibt
		// bei Audio im vertrauten Kreis.
		if req.Privacy == protocol.PrivacyReduced {
			req.Privacy = protocol.PrivacyTrusted
		}
	}
	if req.Skill == "" {
		req.Skill = Route(req.Messages)
	}
	if !req.Limits.Deadline.IsZero() {
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadline(ctx, req.Limits.Deadline)
		defer cancel()
	}
	req.Limits.PrivacyClass = req.Privacy
	if hasTools(req) {
		req.Pattern = PatternSingle // Prüfer und Synthese passen nicht in den Werkzeug-Kreislauf
	}
	r := &run{o: o, req: req, taskID: newID()}
	r.ans = &Answer{TaskID: r.taskID, Pattern: req.Pattern, Skill: req.Skill, SelectedBy: "request"}
	if req.Pattern == "" {
		r.req.Pattern, r.ans.SelectedBy = cmp.Or(o.Default, PatternDraftVerify), "rules"
		if o.Choose != nil {
			if p := o.Choose(Choice{Skill: req.Skill, Feasible: r.feasible(), Default: r.req.Pattern}); p != "" {
				r.req.Pattern, r.ans.SelectedBy = p, "bandit"
			}
		}
		r.ans.Pattern = r.req.Pattern
	}
	req = r.req

	var err error
	switch req.Pattern {
	case PatternSingle:
		err = r.single(ctx)
	case PatternParallel:
		err = r.parallel(ctx)
	case PatternParallelVerify:
		err = r.parallelVerify(ctx)
	case PatternDraftVerify:
		err = r.draftVerify(ctx)
	case PatternCascade:
		err = r.cascade(ctx)
	default:
		err = fmt.Errorf("pattern %q unbekannt", req.Pattern)
	}
	switch {
	case hasTools(req) && errors.Is(err, ErrNoAgent):
		err = ErrToolsNotAllowed
	case errors.Is(err, ErrNoAgent):
		err = noAgentReason(o.Dir.Agents(), req)
	case errors.Is(err, ErrNotPseudonymized):
		// Nur fremde Agenten, und für sie fehlt die Pseudonymisierung: die
		// Ursache liegt in Anfrage und Einrichtung, nicht bei einem Peer.
		err = privacyError{fmt.Sprintf("%v: freigabeklasse reduced schickt fremden knoten nur pseudonymisierte anfragen, "+
			"und namen erkennt ein eigenes oder vertrautes modell; das ging nicht (%v). "+
			"darf der inhalt öffentlich sein, die anfrage mit X-SwarmAiNet-Privacy: public senden", ErrNoAgent, r.pzErr)}
	}
	if err != nil {
		return r.ans, err
	}
	return r.ans, nil
}

// candidates filtert hart (Freigabeklasse) und sortiert nach Eignung.
func (r *run) candidates(skill string, preferLocal bool, exclude func(Agent) bool) []Agent {
	return r.rank(skill, preferLocal, true, exclude)
}

// verifierCandidates sortiert Prüfer ohne Rotation: Bei gleicher Eignung
// zählt die Modellstärke mehr als die Auslastung, denn ein schwacher Prüfer,
// der vorschnell "ok" meldet, entwertet die Prüfung.
func (r *run) verifierCandidates(exclude func(Agent) bool) []Agent {
	return r.rank(r.req.Skill, false, false, exclude)
}

// rank filtert und sortiert. balance: bei Gleichstand nach Auslastung im
// Wechsel verteilen; sonst nach Stärke, Kontextgröße, eigene vor entfernten.
// Ausgelastete Peers kommen in beiden Fällen nach hinten.
func (r *run) rank(skill string, preferLocal, balance bool, exclude func(Agent) bool) []Agent {
	var out []Agent
	for _, a := range r.o.Dir.Agents() {
		if !allowed(a, r.req.Privacy) || (exclude != nil && exclude(a)) || !toolsUsable(a, r.req) ||
			(len(r.req.Agents) > 0 && !slices.Contains(r.req.Agents, a.ID)) {
			continue
		}
		out = append(out, a)
	}
	out = fitting(out, skill)
	loads := make(map[string]agentLoad, len(out))
	for _, a := range out {
		loads[a.key()] = r.o.load.get(a)
	}
	sort.SliceStable(out, func(i, j int) bool {
		si, sj := skillScore(out[i], skill), skillScore(out[j], skill)
		if preferLocal {
			if out[i].Local {
				si += 0.5
			}
			if out[j].Local {
				sj += 0.5
			}
		}
		if si != sj {
			return si > sj
		}
		// Gleich geeignet: Lastverteilung vor Kontextgröße.
		li, lj := loads[out[i].key()], loads[out[j].key()]
		if bi, bj := out[i].Busy || li.full, out[j].Busy || lj.full; bi != bj {
			return !bi
		}
		if !balance {
			if out[i].Strength != out[j].Strength {
				return out[i].Strength > out[j].Strength
			}
			if out[i].ContextMax != out[j].ContextMax {
				return out[i].ContextMax > out[j].ContextMax
			}
			return out[i].Local && !out[j].Local
		}
		if li.inflight != lj.inflight {
			return li.inflight < lj.inflight
		}
		if li.lastPick != lj.lastPick {
			return li.lastPick < lj.lastPick
		}
		if out[i].Strength != out[j].Strength {
			return out[i].Strength > out[j].Strength
		}
		return out[i].ContextMax > out[j].ContextMax
	})
	return out
}

// noAgentReason erklärt ErrNoAgent, wenn es an der Freigabeklasse liegt
// (dann auch ErrPrivacy) oder gar kein Agent bekannt ist.
// errors.Is(err, ErrNoAgent) bleibt erhalten.
func noAgentReason(agents []Agent, req Request) error {
	if len(req.Agents) > 0 {
		return ErrNoAgent // ausdrücklich beschränkt, die Klasse ist nicht der Grund
	}
	if len(agents) == 0 {
		return fmt.Errorf("%w: keine modelle in der konfiguration und keine anbieter im netz gefunden", ErrNoAgent)
	}
	for _, a := range agents {
		if allowed(a, req.Privacy) {
			return ErrNoAgent // es gäbe Agenten, sie passen aus anderen Gründen nicht
		}
	}
	if req.Privacy == protocol.PrivacyLocal {
		return privacyError{fmt.Sprintf("%v: freigabeklasse local erlaubt nur modelle dieses knotens, hier ist keins eingerichtet "+
			"(bekannt: %d agenten anderer knoten)", ErrNoAgent, len(agents))}
	}
	return privacyError{fmt.Sprintf("%v: freigabeklasse %s erlaubt nur eigene und vertraute knoten (privates netz, mitgliedsnetz, "+
		"trusted_peers), bekannt sind nur %d agenten fremder knoten; darf der inhalt öffentlich sein, "+
		"die anfrage mit dem header X-SwarmAiNet-Privacy: public senden", ErrNoAgent, req.Privacy, len(agents))}
}

// allowed setzt die Freigabeklassen um. Bei reduced kommen auch fremde
// Agenten in Frage; sie bekommen nur pseudonymisierte Teilaufträge
// (pseudo.go), und scheitert die Pseudonymisierung, gar keine.
func allowed(a Agent, p protocol.PrivacyClass) bool {
	switch p {
	case protocol.PrivacyLocal:
		return a.Local
	case protocol.PrivacyTrusted:
		return a.Local || a.Trusted
	case protocol.PrivacyReduced, protocol.PrivacyPublic:
		return true
	}
	return false
}

// audioOnly: Der Agent bietet nur Audio-Fähigkeiten an (z. B. Transkription).
func audioOnly(a Agent) bool {
	return len(a.Skills) > 0 && !slices.ContainsFunc(a.Skills, func(s string) bool { return !strings.HasPrefix(s, "audio.") })
}

// fitting: Reine Audio-Agenten bekommen nur Audio-Aufgaben. Passt mindestens
// ein Agent zur Fähigkeit, fallen Agenten ganz ohne passende Fähigkeit weg;
// passt keiner, bleiben sie als Rückfall.
func fitting(agents []Agent, skill string) []Agent {
	if !strings.HasPrefix(skill, "audio.") {
		agents = slices.DeleteFunc(agents, audioOnly)
	}
	if slices.ContainsFunc(agents, func(a Agent) bool { return skillScore(a, skill) > 0 }) {
		agents = slices.DeleteFunc(agents, func(a Agent) bool { return skillScore(a, skill) == 0 })
	}
	return agents
}

func skillScore(a Agent, skill string) float64 {
	best := 0.0
	domain, _, _ := strings.Cut(skill, ".")
	for _, s := range a.Skills {
		switch {
		case s == skill:
			return 3
		case domain != "" && (s == domain || strings.HasPrefix(s, domain+".")):
			best = max(best, 2)
		case s == "general" && !strings.HasPrefix(skill, "audio."): // Text-Generalisten verarbeiten kein Audio
			best = max(best, 1)
		}
	}
	return best
}

// exec führt eine Teilaufgabe aus und weicht bei Ausfall auf den nächsten
// Kandidaten aus. Ein Peer-Ausfall blockiert den Auftrag nicht. Sind alle
// Kandidaten nur ausgelastet, wartet exec und versucht es erneut, solange
// Deadline und busyMaxWait es zulassen.
func (r *run) exec(ctx context.Context, role protocol.Role, cands []Agent, msgs []protocol.ChatMessage, schema string, o out) (protocol.Result, Agent, error) {
	if len(cands) == 0 {
		return protocol.Result{}, Agent{}, ErrNoAgent
	}
	start := time.Now()
	wait := busyFirstWait
	for {
		res, a, err := r.tryCandidates(ctx, role, cands, msgs, schema, o)
		if err == nil || !errors.Is(err, ErrBusy) {
			return res, a, err
		}
		pause := wait + time.Duration(mrand.Int64N(int64(wait/2)+1))
		if d, ok := ctx.Deadline(); time.Since(start)+pause > busyMaxWait || ok && time.Until(d) < pause {
			return res, a, err
		}
		slog.Info("alle kandidaten ausgelastet, neuer versuch", "role", role, "in", pause.Round(100*time.Millisecond))
		select {
		case <-ctx.Done():
			return protocol.Result{}, Agent{}, ctx.Err()
		case <-time.After(pause):
		}
		wait = min(2*wait, busyMaxStep)
	}
}

// tryCandidates probiert die Kandidaten einmal der Reihe nach. Der Fehler
// trägt ErrBusy, wenn alle nur ausgelastet waren; solche Absagen zählen
// nicht gegen das Budget und erscheinen nicht als Schritt.
func (r *run) tryCandidates(ctx context.Context, role protocol.Role, cands []Agent, msgs []protocol.ChatMessage, schema string, o out) (protocol.Result, Agent, error) {
	var lastErr error
	allBusy := true
	for _, a := range cands {
		if ctx.Err() != nil {
			return protocol.Result{}, Agent{}, ctx.Err()
		}
		r.mu.Lock()
		if lim := r.req.Limits.MaxSubtasks; lim > 0 && r.n >= lim {
			r.mu.Unlock()
			return protocol.Result{}, Agent{}, fmt.Errorf("budget: maximal %d teilaufträge", lim)
		}
		r.n++
		r.mu.Unlock()

		tools, choice := toolsFor(a, r.req)
		t := Task{TaskID: r.taskID, SubtaskID: newID(), Role: role, Skill: r.req.Skill, Messages: msgs, Limits: r.req.Limits, JSONSchema: schema,
			Tools: tools, ToolChoice: choice, Audio: r.req.Audio}
		// reduced: für fremde Agenten erst die Pseudonymisierung (vor der
		// Überschrift des Schritts, denn die Erkennung ist ein eigener Schritt).
		var pz *pseudo.Mapping
		if r.needsPseudo(a) {
			m, err := r.pseudonymizer(ctx)
			if err != nil {
				r.mu.Lock()
				r.n-- // keine Arbeit angefallen
				r.mu.Unlock()
				lastErr, allBusy = err, false
				continue
			}
			pz = m
		}
		streamed := false // schon Antworttext gestreamt
		if r.streaming() {
			if o.label != "" {
				r.note("**" + o.label + " · " + agentName(a) + "**\n\n")
			}
			switch o.to {
			case toReasoning:
				t.OnDelta = func(c, _ string) { r.emit(EventReasoning, c) }
			case toContent:
				thinking := false // Nachdenken des Modells als eigener Absatz
				t.OnDelta = func(c, rs string) {
					if rs != "" && !thinking {
						thinking = true
						r.note(rs)
						rs = ""
					}
					r.emit(EventReasoning, rs)
					if c != "" {
						streamed = true
						r.emit(EventContent, c)
					}
				}
			}
		}
		if pz != nil {
			pseudoTask(pz, &t)
		}
		start := time.Now()
		done := r.o.load.begin(a)
		res, err := r.o.Exec.Execute(ctx, a, t)
		done()
		if t.flush != nil {
			t.flush()
		}
		if err == nil && pz != nil {
			revertResult(pz, &res)
		}
		if err == nil && res.Result.Status == protocol.StatusFailed {
			err = fmt.Errorf("agent meldet fehler: %s", res.Result.Summary)
		}
		if err == nil && strings.TrimSpace(res.Result.Content) == "" && len(res.Result.ToolCalls) == 0 {
			err = errors.New("leere antwort")
		}
		if err == nil {
			err = checkToolCalls(a, r.req, res.Result.ToolCalls)
		}
		if err == nil && !a.Local && r.req.Audio == nil && looksLikeInjection(res.Result.Content) {
			err = errors.New("ergebnis enthält anweisungsmuster, verworfen")
		}
		if err != nil && streamed {
			r.emit(EventRestart, "")
		}
		if err != nil && o.label != "" {
			r.note("*nicht verwendet: " + shortErr(err) + "*")
		}
		if errors.Is(err, ErrBusy) {
			r.mu.Lock()
			r.n-- // keine Arbeit angefallen
			r.mu.Unlock()
			slog.Info("anbieter ausgelastet", "role", role, "agent", a.ID, "peer", short(a.PeerID))
			lastErr = err
			continue
		}
		allBusy = false
		step := Step{Role: role, AgentID: a.ID, PeerID: a.PeerID, Family: a.Family, DurationMs: time.Since(start).Milliseconds()}
		if err != nil {
			step.Status, step.Error = protocol.StatusFailed, err.Error()
			r.addStep(step)
			slog.Info("teilauftrag fehlgeschlagen, nächster kandidat", "role", role, "agent", a.ID, "peer", short(a.PeerID), "err", err)
			lastErr = err
			continue
		}
		step.Status = res.Result.Status
		step.PromptTokens, step.CompletionTokens = res.Usage.PromptTokens, res.Usage.CompletionTokens
		if r.req.Trace {
			step.Output = res.Result.Content
		}
		r.addStep(step)
		return res, a, nil
	}
	if allBusy {
		return protocol.Result{}, Agent{}, fmt.Errorf("alle kandidaten für %s ausgelastet: %w", role, lastErr)
	}
	return protocol.Result{}, Agent{}, fmt.Errorf("alle kandidaten für %s fehlgeschlagen: %w", role, lastErr)
}

func (r *run) addStep(s Step) {
	r.mu.Lock()
	r.ans.Steps = append(r.ans.Steps, s)
	r.mu.Unlock()
}

// markVerdict trägt das Urteil am letzten Prüfschritt dieses Agenten ein.
func (r *run) markVerdict(a Agent, verdict string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := len(r.ans.Steps) - 1; i >= 0; i-- {
		s := &r.ans.Steps[i]
		if s.Role == protocol.RoleVerifier && s.AgentID == a.ID && s.PeerID == a.PeerID && s.Status != protocol.StatusFailed {
			s.Verdict = verdict
			return
		}
	}
}

func (r *run) limit(s string) {
	r.mu.Lock()
	r.ans.Limitations = append(r.ans.Limitations, s)
	r.mu.Unlock()
}

// shortErr: Fehler in einer Zeile für die Denkphase.
func shortErr(err error) string {
	if errors.Is(err, ErrBusy) {
		return "ausgelastet"
	}
	s, _, _ := strings.Cut(err.Error(), "\n")
	if len(s) > 120 {
		s = s[:120] + "…"
	}
	return s
}

func short(id string) string {
	if len(id) > 12 {
		return id[len(id)-8:]
	}
	return id
}

func (r *run) single(ctx context.Context) error {
	res, _, err := r.exec(ctx, protocol.RoleSpecialist, r.candidates(r.req.Skill, false, nil), r.req.Messages, "", out{to: toContent})
	if err != nil {
		return err
	}
	r.ans.Content, r.ans.ToolCalls = res.Result.Content, res.Result.ToolCalls
	return nil
}

func (r *run) draftVerify(ctx context.Context) error {
	draft, drafter, err := r.exec(ctx, protocol.RoleSpecialist, r.candidates(r.req.Skill, false, nil), r.req.Messages, "", out{"Entwurf", toReasoning})
	if err != nil {
		return err
	}
	r.ans.Content = draft.Result.Content
	r.verifyAndRevise(ctx, drafter)
	return nil
}

// verifyAndRevise lässt r.ans.Content von einem Prüfer aus einer anderen
// Familie als author prüfen und bei Mängeln einmal überarbeiten. Fehlschläge
// führen nicht zum Abbruch, sondern zu Einschränkungen in der Antwort.
func (r *run) verifyAndRevise(ctx context.Context, author Agent) {
	v, ok := r.verify(ctx, author)
	if !ok || v.Verdict == "ok" {
		return
	}
	synth, _, err := r.exec(ctx, protocol.RoleSynthesizer, r.candidates("general", true, nil), synthPrompt(r.req.Messages, []string{r.ans.Content}, v.Issues), "", out{"Überarbeitung", toContent})
	if err != nil {
		r.limit("synthese fehlgeschlagen: antwort mit bekannten mängeln")
		return
	}
	r.ans.Content = synth.Result.Content
}

// verify lässt r.ans.Content von einem Prüfer aus einer anderen Familie als
// author prüfen. ok=false: ungeprüft (als Einschränkung vermerkt).
func (r *run) verify(ctx context.Context, author Agent) (verdict, bool) {
	verifiers := r.verifierCandidates(func(a Agent) bool { return a.Family == author.Family })
	if len(verifiers) == 0 {
		verifiers = r.verifierCandidates(func(a Agent) bool { return a.ID == author.ID && a.PeerID == author.PeerID })
		if len(verifiers) > 0 {
			r.limit("kein prüfer aus anderer modellfamilie verfügbar")
		}
	}
	if len(verifiers) == 0 {
		r.limit("ungeprüft: kein prüfer verfügbar")
		return verdict{}, false
	}
	// Ein unbrauchbares Urteil (kein JSON, Schema statt Antwort …) ist kein
	// Mangelbefund: dann prüft der nächste Kandidat, höchstens maxVerifyTries.
	schema := verdictSchema
	if isMath(r.req.Skill) {
		schema = mathVerdictSchema
	}
	var v verdict
	var judge Agent
	valid, tried := false, 0
	for len(verifiers) > 0 && tried < maxVerifyTries && !valid {
		review, used, err := r.exec(ctx, protocol.RoleVerifier, verifiers, verifyPrompt(r.req.Messages, r.ans.Content, r.req.Skill), schema, out{label: "Prüfung"})
		if err != nil {
			break // alle verbleibenden Kandidaten fehlgeschlagen
		}
		tried++
		var perr error
		if v, perr = parseVerdict(review.Result.Content); perr == nil {
			valid, judge = true, used
			break
		}
		slog.Info("prüferurteil nicht auswertbar, nächster prüfer", "agent", used.ID, "peer", short(used.PeerID), "err", perr)
		verifiers = slices.DeleteFunc(verifiers, func(a Agent) bool { return a.key() == used.key() })
	}
	if !valid {
		r.note("Urteil: keines, Antwort ungeprüft")
		if tried > 0 {
			r.limit("ungeprüft: prüferurteil nicht auswertbar")
		} else {
			r.limit("ungeprüft: prüfung fehlgeschlagen")
		}
		return verdict{}, false
	}
	// Mathe: Mängel, die am Ergebnis nichts ändern, lösen keine Überarbeitung aus.
	if v.Verdict == "issues" && isMath(r.req.Skill) && sameResult(v.Result, r.ans.Content) {
		slog.Info("prüfer bestätigt das ergebnis, mängelbefund verworfen", "result", v.Result, "issues", len(v.Issues))
		v.Verdict, v.Issues = "ok", nil
	}
	r.ans.Verdict, r.ans.Issues = v.Verdict, v.Issues
	r.markVerdict(judge, v.Verdict)
	switch {
	case v.Verdict == "ok":
		r.note("Urteil: in Ordnung")
	case len(v.Issues) == 0:
		r.note("Urteil: Mängel")
	default:
		r.note("Urteil: Mängel\n- " + strings.Join(v.Issues, "\n- "))
	}
	return v, true
}

func (r *run) parallel(ctx context.Context) error {
	_, err := r.combine(ctx, toContent)
	return err
}

// parallelVerify: unabhängige Lösungen, Synthese, dann Prüfung durch eine
// andere Familie als die der Synthese und bei Mängeln eine Überarbeitung.
func (r *run) parallelVerify(ctx context.Context) error {
	author, err := r.combine(ctx, toReasoning)
	if err != nil {
		return err
	}
	r.verifyAndRevise(ctx, author)
	return nil
}

// combine holt unabhängige Lösungen (je Familie eine) und führt sie zusammen.
// Ergebnis steht in r.ans.Content; zurück kommt der Agent, der es verfasst hat.
// synthTo: wohin die Synthese gestreamt wird (Antwort nur ohne spätere Prüfung).
func (r *run) combine(ctx context.Context, synthTo target) (Agent, error) {
	n := r.o.Parallel
	if n <= 1 {
		n = 3
	}
	// je Familie nur ein Agent: Vielfalt vor Menge. Die Lösungen laufen
	// gleichzeitig, also zählen schon geplante Teilaufträge gegen die Slots
	// eines Peers. Familien, die es nur auf einem Peer gibt, bekommen dort
	// einen Slot reserviert; Familien mit Alternativen weichen aus.
	cands := r.candidates(r.req.Skill, false, nil)
	famPeers := map[string]map[string]bool{}
	for _, a := range cands {
		if famPeers[a.Family] == nil {
			famPeers[a.Family] = map[string]bool{}
		}
		famPeers[a.Family][a.PeerID] = true
	}
	onlyOn := func(fam, peer string) bool { return len(famPeers[fam]) == 1 && famPeers[fam][peer] }
	seen := map[string]bool{}
	planned := map[string]int{}
	fits := func(a Agent) bool {
		if a.Local || a.Slots <= 0 {
			return true
		}
		reserved := 0
		for fam := range famPeers {
			if fam != a.Family && !seen[fam] && onlyOn(fam, a.PeerID) {
				reserved++
			}
		}
		return r.o.load.peerInflight(a.PeerID)+planned[a.PeerID]+reserved < a.Slots
	}
	var picks, spare []Agent
	for _, a := range cands {
		if seen[a.Family] {
			continue
		}
		if !fits(a) {
			spare = append(spare, a)
			continue
		}
		seen[a.Family] = true
		planned[a.PeerID]++
		picks = append(picks, a)
	}
	// Familien, die nur auf vollen Peers vorkommen: lieber versuchen als weglassen.
	for _, a := range spare {
		if !seen[a.Family] {
			seen[a.Family] = true
			picks = append(picks, a)
		}
	}
	if len(picks) > n {
		picks = picks[:n]
	}
	if len(picks) == 0 {
		return Agent{}, ErrNoAgent
	}
	answers := make([]string, len(picks))
	var wg sync.WaitGroup
	for i, a := range picks {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, _, err := r.exec(ctx, protocol.RoleSpecialist, []Agent{a}, r.req.Messages, "", out{label: "Lösung"})
			if err == nil {
				answers[i] = res.Result.Content
			}
		}()
	}
	wg.Wait()
	var got []string
	var first Agent
	for i, a := range answers {
		if a != "" {
			if len(got) == 0 {
				first = picks[i]
			}
			got = append(got, a)
		}
	}
	switch len(got) {
	case 0:
		return Agent{}, errors.New("keine unabhängige lösung erhalten")
	case 1:
		r.ans.Content = got[0]
		r.limit("nur eine unabhängige lösung")
		return first, nil
	}
	synth, synthesizer, err := r.exec(ctx, protocol.RoleSynthesizer, r.candidates("general", true, nil), synthPrompt(r.req.Messages, got, nil), "", out{"Synthese", synthTo})
	if err != nil {
		r.ans.Content = got[0]
		r.limit("synthese fehlgeschlagen: erste lösung ungeprüft")
		return first, nil
	}
	r.ans.Content = synth.Result.Content
	return synthesizer, nil
}
