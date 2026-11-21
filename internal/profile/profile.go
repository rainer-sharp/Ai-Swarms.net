// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Rainer Sestig. Kommerzielle Lizenz: siehe COMMERCIAL.md.

package profile

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"sync"
	"time"
)

// Gewichte der Signale (spec/cooperation.md § 2.2).
const (
	WeightObjective = 1.0
	WeightVerifier  = 0.5
	WeightUser      = 0.25
	// Gegner jeder Partie ist eine feste Referenz.
	referenceR  = 1500.0
	referenceRD = 50.0
	// Ein Prüferurteil zählt erst, wenn der Prüfer so viele objektiv
	// überprüfte Urteile hat und davon mindestens so viele zutrafen.
	verifierMinChecks   = 5
	verifierMinAccuracy = 0.6
	// So viele Aufträge merkt sich das Profil für spätere Rückmeldungen.
	taskMemory = 1024
)

// Rollen wie im Protokoll (protocol.Role), hier als Text.
const (
	RoleSpecialist  = "specialist"
	RoleVerifier    = "verifier"
	RoleSynthesizer = "synthesizer"
)

// Step beschreibt einen Schritt eines Auftrags, so wie ihn das Profil braucht.
type Step struct {
	Role     string
	Agent    string // Schlüssel des Agenten, siehe AgentKey
	Family   string
	OK       bool   // erfolgreich, mit Ergebnis
	Verdict  string // nur Prüfer: "ok" oder "issues", leer ohne gültiges Urteil
	Strength int    // Selbstauskunft für den Startwert
}

// AgentKey: Peer-ID und Agent-ID; eigene Agenten heißen "local/<agent>".
func AgentKey(peerID, agentID string) string {
	if peerID == "" {
		peerID = "local"
	}
	return peerID + "/" + agentID
}

// VerifierStats: Wie oft lag ein Prüfer richtig, gemessen an objektiven
// Signalen zum geprüften Ergebnis.
type VerifierStats struct {
	Hit        int `json:"hit"`         // issues, Ergebnis war falsch
	FalseAlarm int `json:"false_alarm"` // issues, Ergebnis war richtig
	CorrectOK  int `json:"correct_ok"`  // ok, Ergebnis war richtig
	Miss       int `json:"miss"`        // ok, Ergebnis war falsch
}

func (v VerifierStats) Checks() int { return v.Hit + v.FalseAlarm + v.CorrectOK + v.Miss }

func (v VerifierStats) Accuracy() float64 {
	if v.Checks() == 0 {
		return 0
	}
	return float64(v.Hit+v.CorrectOK) / float64(v.Checks())
}

// Trusted: Das Urteil dieses Prüfers zählt als Signal.
func (v VerifierStats) Trusted() bool {
	return v.Checks() >= verifierMinChecks && v.Accuracy() >= verifierMinAccuracy
}

type agentProfile struct {
	Family   string                    `json:"family,omitempty"`
	Skills   map[string]*Rating        `json:"skills"`
	Verifier map[string]*VerifierStats `json:"verifier,omitempty"`
}

type task struct {
	id    string
	skill string
	steps []Step
	given map[int]bool // Schritte mit objektivem oder Nutzer-Feedback (-1: Endergebnis)
}

// Profile ist sicher für gleichzeitige Nutzung.
type Profile struct {
	path string
	now  func() time.Time

	mu     sync.Mutex
	agents map[string]*agentProfile
	tasks  []*task // Ringpuffer, älteste zuerst
}

type fileFormat struct {
	Version int                      `json:"version"`
	Agents  map[string]*agentProfile `json:"agents"`
}

// Open lädt das Profil aus path oder beginnt leer. Eine unlesbare Datei wird
// nach path+".bad" verschoben, damit sie nicht still überschrieben wird.
func Open(path string) *Profile {
	p := &Profile{path: path, now: time.Now, agents: map[string]*agentProfile{}}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return p
	}
	var f fileFormat
	if err == nil {
		err = json.Unmarshal(data, &f)
	}
	if err != nil || f.Version != 1 {
		slog.Warn("kompetenzprofil unlesbar, beginne neu", "datei", path, "err", err)
		_ = os.Rename(path, path+".bad")
		return p
	}
	if f.Agents != nil {
		p.agents = f.Agents
	}
	return p
}

func (p *Profile) agent(key, family string) *agentProfile {
	a := p.agents[key]
	if a == nil {
		a = &agentProfile{Skills: map[string]*Rating{}}
		p.agents[key] = a
	}
	if family != "" {
		a.Family = family
	}
	return a
}

func (p *Profile) play(s Step, skill string, score, weight float64) {
	a := p.agent(s.Agent, s.Family)
	r := a.Skills[skill]
	if r == nil {
		nr := NewRating(s.Strength, p.now())
		r = &nr
		a.Skills[skill] = r
	}
	*r = r.Update([]Match{{OppR: referenceR, OppRD: referenceRD, Score: score, Weight: weight}}, p.now())
}

func (p *Profile) verifierStats(key, skill string) *VerifierStats {
	a := p.agent(key, "")
	if a.Verifier == nil {
		a.Verifier = map[string]*VerifierStats{}
	}
	v := a.Verifier[skill]
	if v == nil {
		v = &VerifierStats{}
		a.Verifier[skill] = v
	}
	return v
}

// reviewed: Index des Schritts, den der Prüfer in Schritt i beurteilt hat,
// also des letzten erfolgreichen Erstellers davor; -1 ohne einen solchen.
func reviewed(steps []Step, i int) int {
	for j := i - 1; j >= 0; j-- {
		if steps[j].OK && (steps[j].Role == RoleSpecialist || steps[j].Role == RoleSynthesizer) {
			return j
		}
	}
	return -1
}

// final: Index des Schritts, dessen Ergebnis die Antwort ist.
func final(steps []Step) int {
	for j := len(steps) - 1; j >= 0; j-- {
		if steps[j].OK && (steps[j].Role == RoleSpecialist || steps[j].Role == RoleSynthesizer) {
			return j
		}
	}
	return -1
}

// Record merkt sich einen abgeschlossenen Auftrag und wertet die Urteile
// bewährter Prüfer als Signal für die geprüften Ersteller (Gewicht ½).
func (p *Profile) Record(taskID, skill string, steps []Step) {
	if taskID == "" || skill == "" || len(steps) == 0 {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for i, s := range steps {
		if s.Role != RoleVerifier || s.Verdict == "" {
			continue
		}
		j := reviewed(steps, i)
		if j < 0 || steps[j].Agent == s.Agent || steps[j].Family == s.Family {
			continue // nie sich selbst, nie dieselbe Familie
		}
		if !p.verifierStats(s.Agent, skill).Trusted() {
			continue
		}
		score := 0.0
		if s.Verdict == "ok" {
			score = 1
		}
		p.play(steps[j], skill, score, WeightVerifier)
	}
	p.tasks = append(p.tasks, &task{id: taskID, skill: skill, steps: slices.Clone(steps), given: map[int]bool{}})
	if len(p.tasks) > taskMemory {
		p.tasks = p.tasks[len(p.tasks)-taskMemory:]
	}
	p.saveLocked()
}

// Feedback ist eine Rückmeldung zu einem Auftrag.
type Feedback struct {
	TaskID string `json:"task_id"`
	Good   bool   `json:"good"`
	// Objective: geprüftes Ergebnis (Tests, exakte Lösung), volles Gewicht;
	// sonst Nutzerfeedback mit Gewicht ¼.
	Objective bool `json:"objective,omitempty"`
	// Step: Index in steps[] der Antwort; ohne Angabe das Endergebnis.
	Step *int `json:"step,omitempty"`
}

var (
	ErrUnknownTask = errors.New("auftrag unbekannt oder zu alt")
	ErrBadStep     = errors.New("schritt ist kein erfolgreicher ersteller")
	ErrDuplicate   = errors.New("rückmeldung zu diesem schritt liegt schon vor")
)

// Feedback wertet eine Rückmeldung als Partie für den Ersteller und, wenn sie
// objektiv ist, als Treffer oder Fehlalarm der Prüfer dieses Ergebnisses.
func (p *Profile) Feedback(fb Feedback) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	i := slices.IndexFunc(p.tasks, func(t *task) bool { return t.id == fb.TaskID })
	if i < 0 {
		return ErrUnknownTask
	}
	t := p.tasks[i]
	idx, mark := -1, -1
	if fb.Step == nil {
		idx = final(t.steps)
	} else {
		idx, mark = *fb.Step, *fb.Step
	}
	if idx < 0 || idx >= len(t.steps) || !t.steps[idx].OK ||
		(t.steps[idx].Role != RoleSpecialist && t.steps[idx].Role != RoleSynthesizer) {
		return ErrBadStep
	}
	if t.given[mark] {
		return ErrDuplicate
	}
	t.given[mark] = true
	score, weight := 0.0, WeightUser
	if fb.Good {
		score = 1
	}
	if fb.Objective {
		weight = WeightObjective
	}
	p.play(t.steps[idx], t.skill, score, weight)
	if fb.Objective {
		for k, s := range t.steps {
			if s.Role != RoleVerifier || s.Verdict == "" || reviewed(t.steps, k) != idx {
				continue
			}
			v := p.verifierStats(s.Agent, t.skill)
			switch {
			case s.Verdict == "issues" && !fb.Good:
				v.Hit++
			case s.Verdict == "issues":
				v.FalseAlarm++
			case fb.Good:
				v.CorrectOK++
			default:
				v.Miss++
			}
		}
	}
	p.saveLocked()
	return nil
}

// AgentView ist eine Zeile der Anzeige.
type AgentView struct {
	Agent  string                   `json:"agent"`
	Family string                   `json:"family,omitempty"`
	Skills map[string]Rating        `json:"skills"`
	Verify map[string]VerifierStats `json:"verifier,omitempty"`
}

// Snapshot liefert das Profil mit der Unsicherheit von jetzt, sortiert nach Agent.
func (p *Profile) Snapshot() []AgentView {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.now()
	out := make([]AgentView, 0, len(p.agents))
	for k, a := range p.agents {
		v := AgentView{Agent: k, Family: a.Family, Skills: map[string]Rating{}}
		for s, r := range a.Skills {
			v.Skills[s] = r.Current(now)
		}
		if len(a.Verifier) > 0 {
			v.Verify = map[string]VerifierStats{}
			for s, st := range a.Verifier {
				v.Verify[s] = *st
			}
		}
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Agent < out[j].Agent })
	return out
}

// saveLocked schreibt atomar (temporäre Datei, dann umbenennen). Fehler
// werden protokolliert; das Profil im Speicher bleibt gültig.
func (p *Profile) saveLocked() {
	if p.path == "" {
		return
	}
	data, err := json.MarshalIndent(fileFormat{Version: 1, Agents: p.agents}, "", " ")
	if err == nil {
		err = writeAtomic(p.path, data)
	}
	if err != nil {
		slog.Warn("kompetenzprofil nicht gespeichert", "datei", p.path, "err", err)
	}
}

func writeAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".profile-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("umbenennen: %w", err)
	}
	return nil
}
