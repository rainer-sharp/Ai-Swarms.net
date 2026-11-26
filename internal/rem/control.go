// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Rainer Sestig und Mitwirkende.

package rem

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"time"
)

// control ist der dauerhaft gespeicherte Bedienzustand: Pause und Sofort-Stopp
// gelten auch nach einem Neustart, bis resume sie aufhebt. Die Datei gibt es
// nur, solange einer von beiden gilt.
type control struct {
	Paused  bool      `json:"paused,omitempty"`
	Stopped bool      `json:"stopped,omitempty"`
	Since   time.Time `json:"since"`
}

// ErrControlUnreadable: Die Steuerdatei ist da, aber nicht lesbar. Der
// Manager geht dann sicherheitshalber in den Sofort-Stopp.
var ErrControlUnreadable = errors.New("steuerdatei unlesbar, sicherheitshalber sofort-stopp")

// Persist liest Pause und Sofort-Stopp aus path und speichert sie dort künftig.
// Fehlt die Datei, nimmt der Knoten wie gewohnt teil.
func (m *Manager) Persist(path string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ctlPath = path
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	var c control
	if err == nil {
		err = json.Unmarshal(data, &c)
	}
	if err != nil {
		m.stopped, m.since = true, m.now()
		return fmt.Errorf("%w (%s): %v", ErrControlUnreadable, path, err)
	}
	m.paused, m.stopped, m.since = c.Paused, c.Stopped, c.Since
	if !c.Paused && !c.Stopped {
		m.since = time.Time{}
	}
	return nil
}

// saveLocked schreibt den Bedienzustand atomar oder löscht die Datei, wenn
// weder Pause noch Sofort-Stopp gilt. Ohne Persist: nichts zu tun.
func (m *Manager) saveLocked() error {
	if m.ctlPath == "" {
		return nil
	}
	if !m.paused && !m.stopped {
		if err := os.Remove(m.ctlPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		return nil
	}
	data, err := json.Marshal(control{Paused: m.paused, Stopped: m.stopped, Since: m.since})
	if err != nil {
		return err
	}
	tmp := m.ctlPath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, m.ctlPath)
}
