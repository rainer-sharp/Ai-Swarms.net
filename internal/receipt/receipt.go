// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Rainer Sestig und Mitwirkende.

// Package receipt erzeugt und speichert von beiden Seiten signierte Quittungen.
// Ab Phase 1 werden sie nur gesammelt; Verrechnung (gegenseitiger Kredit) folgt in Phase 4.
package receipt

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/rainer-sharp/Ai-Swarms.net/internal/identity"
)

type Receipt struct {
	TaskID           string    `json:"task_id"`
	SubtaskID        string    `json:"subtask_id"`
	Coordinator      string    `json:"coordinator"`
	Provider         string    `json:"provider"`
	Role             string    `json:"role"`
	AgentID          string    `json:"agent_id"`
	ModelFamily      string    `json:"model_family"`
	Status           string    `json:"status"`
	FinishedAt       time.Time `json:"finished_at"`
	DurationMs       int64     `json:"duration_ms"`
	PromptTokens     int       `json:"prompt_tokens"`
	CompletionTokens int       `json:"completion_tokens"`
	ResultDigest     string    `json:"result_digest"`
	CoordinatorSig   []byte    `json:"coordinator_sig,omitempty"`
	ProviderSig      []byte    `json:"provider_sig,omitempty"`
}

func (r *Receipt) signingBytes() []byte {
	c := *r
	c.CoordinatorSig, c.ProviderSig = nil, nil
	c.FinishedAt = c.FinishedAt.UTC()
	b, _ := json.Marshal(c)
	return append([]byte("swarmainet/receipt/v0.1\n"), b...)
}

func (r *Receipt) SignAsCoordinator(id *identity.Identity) error {
	if r.Coordinator != id.ID.String() {
		return errors.New("quittung: falscher koordinator")
	}
	sig, err := id.Sign(r.signingBytes())
	r.CoordinatorSig = sig
	return err
}

func (r *Receipt) SignAsProvider(id *identity.Identity) error {
	if r.Provider != id.ID.String() {
		return errors.New("quittung: falscher anbieter")
	}
	sig, err := id.Sign(r.signingBytes())
	r.ProviderSig = sig
	return err
}

// Verify prüft beide Signaturen.
func (r *Receipt) Verify() error {
	data := r.signingBytes()
	if err := identity.Verify(r.Coordinator, data, r.CoordinatorSig); err != nil {
		return fmt.Errorf("koordinator: %w", err)
	}
	if err := identity.Verify(r.Provider, data, r.ProviderSig); err != nil {
		return fmt.Errorf("anbieter: %w", err)
	}
	return nil
}

// Store ist ein append-only JSONL-Speicher; doppelte Teilaufträge werden
// ignoriert (idempotent, nichts wird doppelt abgerechnet).
type Store struct {
	mu   sync.Mutex
	path string
	seen map[string]bool
	all  []Receipt
}

func OpenStore(path string) (*Store, error) {
	s := &Store{path: path, seen: make(map[string]bool)}
	if path == "" {
		return s, nil
	}
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		var r Receipt
		if json.Unmarshal(sc.Bytes(), &r) == nil {
			s.seen[key(&r)] = true
			s.all = append(s.all, r)
		}
	}
	return s, sc.Err()
}

func key(r *Receipt) string { return r.TaskID + "/" + r.SubtaskID + "/" + r.Provider }

// Add speichert eine vollständig signierte Quittung.
func (s *Store) Add(r Receipt) error {
	if err := r.Verify(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.seen[key(&r)] {
		return nil
	}
	if s.path != "" {
		if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
			return err
		}
		f, err := os.OpenFile(s.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		line, _ := json.Marshal(r)
		_, err = f.Write(append(line, '\n'))
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return err
		}
	}
	s.seen[key(&r)] = true
	s.all = append(s.all, r)
	return nil
}

func (s *Store) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.all)
}

// Summary: geleistete und empfangene Arbeit pro Peer (Grundlage für den Kredit ab Phase 4).
type Summary struct {
	Given    int `json:"given"`    // ich war Anbieter
	Received int `json:"received"` // ich war Koordinator
}

func (s *Store) Summary(self string) map[string]Summary {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]Summary{}
	for _, r := range s.all {
		switch self {
		case r.Provider:
			v := out[r.Coordinator]
			v.Given++
			out[r.Coordinator] = v
		case r.Coordinator:
			v := out[r.Provider]
			v.Received++
			out[r.Provider] = v
		}
	}
	return out
}
