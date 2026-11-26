// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Rainer Sestig. Kommerzielle Lizenz: siehe COMMERCIAL.md.

// Package jobs verwaltet asynchrone Aufträge (spec/cooperation.md § 7): eigene,
// die dieser Knoten oder ein vertrauter Peer koordiniert, und solche, die
// dieser Knoten für einen vertrauten Peer koordiniert. Abgeschlossene
// Aufträge bleiben eine Weile abrufbar, auch über einen Neustart hinweg.
package jobs

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/rainer-sharp/Ai-Swarms.net/internal/orchestrator"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/protocol"
)

const (
	// Retention: So lange bleibt ein abgeschlossener Auftrag abrufbar.
	Retention = 24 * time.Hour
	// MaxJobs: höchstens so viele Aufträge; darüber fallen die ältesten
	// abgeschlossenen weg.
	MaxJobs = 200
)

var (
	// ErrUnknown: kein solcher Auftrag (oder schon abgelaufen).
	ErrUnknown = errors.New("auftrag unbekannt oder abgelaufen")
	// ErrInvalid: Die Anfrage selbst ist fehlerhaft.
	ErrInvalid = errors.New("ungültiger auftrag")
	// ErrCoordinator: Der koordinierende Peer ist nicht erreichbar oder lehnt ab.
	ErrCoordinator = errors.New("koordinator")
)

// Job ist ein asynchroner Auftrag.
type Job struct {
	ID string `json:"id"`
	// Owner: Peer-ID des Auftraggebers, wenn dieser Knoten für ihn
	// koordiniert; leer bei eigenen Aufträgen.
	Owner string `json:"owner,omitempty"`
	// Coordinator: Peer-ID des koordinierenden Peers; leer, wenn dieser
	// Knoten selbst koordiniert. Network: das Netz, über das er erreichbar ist.
	Coordinator string    `json:"coordinator,omitempty"`
	Network     string    `json:"network,omitempty"`
	Model       string    `json:"model,omitempty"`
	Status      string    `json:"status"` // protocol.JobRunning, JobDone …
	Created     time.Time `json:"created"`
	Finished    time.Time `json:"finished,omitzero"`
	Error       string    `json:"error,omitempty"`
	// Antwort bei done: Text, Werkzeug-Aufrufe und Metadaten.
	Content   string               `json:"content,omitempty"`
	ToolCalls []protocol.ToolCall  `json:"tool_calls,omitempty"`
	Answer    *orchestrator.Answer `json:"answer,omitempty"`
}

// NewID erzeugt eine Kennung für einen Auftrag.
func NewID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return "job-" + hex.EncodeToString(b)
}

// Store hält die Aufträge und speichert sie in einer Datei (Rechte 0600,
// denn sie enthält Antworten). Sicher für gleichzeitige Nutzung.
type Store struct {
	mu      sync.Mutex
	path    string
	jobs    map[string]*Job
	cancels map[string]context.CancelFunc
	now     func() time.Time
}

func key(owner, id string) string { return owner + "/" + id }

// Open lädt die Aufträge aus path (leer: nur im Speicher). Aufträge, die
// dieser Knoten selbst koordinierte und die beim Beenden noch liefen, sind
// verloren und gelten als gescheitert.
func Open(path string) *Store {
	s := &Store{path: path, jobs: map[string]*Job{}, cancels: map[string]context.CancelFunc{}, now: time.Now}
	if path == "" {
		return s
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			slog.Warn("aufträge nicht gelesen", "datei", path, "err", err)
		}
		return s
	}
	var list []*Job
	if err := json.Unmarshal(b, &list); err != nil {
		slog.Warn("aufträge nicht lesbar, beginne leer", "datei", path, "err", err)
		return s
	}
	for _, j := range list {
		if j.Status == protocol.JobRunning && j.Coordinator == "" {
			j.Status, j.Error, j.Finished = protocol.JobFailed, "abgebrochen: der koordinierende knoten wurde beendet", s.now()
		}
		s.jobs[key(j.Owner, j.ID)] = j
	}
	return s
}

// Add legt einen Auftrag an. cancel bricht ihn ab, solange er hier läuft.
func (s *Store) Add(j Job, cancel context.CancelFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := key(j.Owner, j.ID)
	s.jobs[k] = &j
	if cancel != nil {
		s.cancels[k] = cancel
	}
	s.saveLocked()
}

// Get liefert eine Kopie des Auftrags.
func (s *Store) Get(owner, id string) (Job, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	j := s.jobs[key(owner, id)]
	if j == nil {
		return Job{}, false
	}
	return *j, true
}

// Update ändert einen Auftrag. Ist er danach abgeschlossen, fällt seine
// Abbruchfunktion weg. Ein abgeschlossener Auftrag ändert sich nicht mehr.
func (s *Store) Update(owner, id string, fn func(*Job)) (Job, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := key(owner, id)
	j := s.jobs[k]
	if j == nil {
		return Job{}, false
	}
	if protocol.JobFinal(j.Status) {
		return *j, true
	}
	fn(j)
	if protocol.JobFinal(j.Status) {
		if j.Finished.IsZero() {
			j.Finished = s.now()
		}
		delete(s.cancels, k)
	}
	s.saveLocked()
	return *j, true
}

// Cancel bricht einen laufenden Auftrag ab, den dieser Knoten koordiniert.
func (s *Store) Cancel(owner, id string) (Job, bool) {
	s.mu.Lock()
	cancel := s.cancels[key(owner, id)]
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return s.Update(owner, id, func(j *Job) {
		j.Status, j.Error = protocol.JobCanceled, "abgebrochen"
	})
}

// Remove löscht einen Auftrag.
func (s *Store) Remove(owner, id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := key(owner, id)
	if s.jobs[k] == nil {
		return false
	}
	delete(s.jobs, k)
	delete(s.cancels, k)
	s.saveLocked()
	return true
}

// List liefert die Aufträge eines Auftraggebers (leer: die eigenen), die
// neuesten zuerst.
func (s *Store) List(owner string) []Job {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Job
	for _, j := range s.jobs {
		if j.Owner == owner {
			out = append(out, *j)
		}
	}
	slices.SortFunc(out, func(a, b Job) int { return b.Created.Compare(a.Created) })
	return out
}

// RunningFor zählt die laufenden Aufträge, die dieser Knoten für andere
// koordiniert: für owner, oder für alle, wenn owner leer ist.
func (s *Store) RunningFor(owner string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, j := range s.jobs {
		if j.Owner != "" && j.Status == protocol.JobRunning && (owner == "" || j.Owner == owner) {
			n++
		}
	}
	return n
}

// saveLocked räumt auf und schreibt die Datei (unter s.mu).
func (s *Store) saveLocked() {
	now := s.now()
	var finished []*Job
	for k, j := range s.jobs {
		if !protocol.JobFinal(j.Status) {
			continue
		}
		if now.Sub(j.Finished) > Retention {
			delete(s.jobs, k)
			continue
		}
		finished = append(finished, j)
	}
	if over := len(s.jobs) - MaxJobs; over > 0 {
		slices.SortFunc(finished, func(a, b *Job) int { return a.Finished.Compare(b.Finished) })
		for _, j := range finished[:min(over, len(finished))] {
			delete(s.jobs, key(j.Owner, j.ID))
		}
	}
	if s.path == "" {
		return
	}
	list := make([]*Job, 0, len(s.jobs))
	for _, j := range s.jobs {
		list = append(list, j)
	}
	b, err := json.Marshal(list)
	if err == nil {
		err = writeFile(s.path, b)
	}
	if err != nil {
		slog.Warn("aufträge nicht gespeichert", "datei", s.path, "err", err)
	}
}

// writeFile schreibt atomar mit Rechten 0600.
func writeFile(path string, b []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".jobs-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
