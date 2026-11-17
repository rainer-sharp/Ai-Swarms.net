// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Rainer Sestig. Kommerzielle Lizenz: siehe COMMERCIAL.md.

package protocol

import "fmt"

type State string

const (
	StateCreated     State = "CREATED"
	StateDiscovering State = "DISCOVERING"
	StateOffered     State = "OFFERED"
	StateAccepted    State = "ACCEPTED"
	StateRunning     State = "RUNNING"
	StateReviewing   State = "REVIEWING"
	StateCompleted   State = "COMPLETED"
	StateRejected    State = "REJECTED"
	StateCancelled   State = "CANCELLED"
	StateExpired     State = "EXPIRED"
	StateFailed      State = "FAILED"
)

var transitions = map[State][]State{
	StateCreated:     {StateDiscovering},
	StateDiscovering: {StateOffered},
	StateOffered:     {StateAccepted, StateRejected, StateDiscovering},
	StateAccepted:    {StateRunning},
	StateRunning:     {StateReviewing, StateCompleted},
	StateReviewing:   {StateCompleted, StateRunning},
	StateCompleted:   nil,
	StateRejected:    nil,
	StateCancelled:   nil,
	StateExpired:     nil,
	StateFailed:      nil,
}

func (s State) Terminal() bool {
	switch s {
	case StateCompleted, StateRejected, StateCancelled, StateExpired, StateFailed:
		return true
	}
	return false
}

// Next prüft einen Zustandsübergang. CANCELLED, EXPIRED und FAILED sind aus
// jedem nicht terminalen Zustand erreichbar. Ein Übergang in denselben Zustand
// ist erlaubt (idempotente Wiederholung).
func (s State) Next(to State) (State, error) {
	if s == to {
		return s, nil
	}
	if s.Terminal() {
		return s, fmt.Errorf("zustand %s ist terminal", s)
	}
	switch to {
	case StateCancelled, StateExpired, StateFailed:
		return to, nil
	}
	for _, t := range transitions[s] {
		if t == to {
			return to, nil
		}
	}
	return s, fmt.Errorf("übergang %s → %s nicht erlaubt", s, to)
}
