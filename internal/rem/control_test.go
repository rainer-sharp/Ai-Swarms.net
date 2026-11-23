// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Rainer Sestig. Kommerzielle Lizenz: siehe COMMERCIAL.md.

package rem

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// restart legt einen neuen Manager an, der den gespeicherten Zustand liest,
// wie nach einem Neustart des Knotens.
func restart(t *testing.T, path string) (*Manager, error) {
	t.Helper()
	m := newTestManager(t, nil)
	return m, m.Persist(path)
}

// TestStopSurvivesRestart: Ein Sofort-Stopp gilt nach einem Neustart weiter,
// bis resume ihn aufhebt; die Datei hat Rechte 0600 und verschwindet danach.
func TestStopSurvivesRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "control.json")
	m, err := restart(t, path)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.EmergencyStop(); err != nil {
		t.Fatal(err)
	}
	since := m.Status().Since
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("steuerdatei: %v %v", fi, err)
	}

	m, err = restart(t, path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Admit(AdmitRequest{}); err == nil || !strings.Contains(err.Error(), "sofort-stopp") {
		t.Fatalf("annahme nach neustart: %v", err)
	}
	if st := m.Status(); !st.Stopped || st.SlotsNow != 0 || st.Since == nil || !st.Since.Equal(*since) {
		t.Fatalf("status nach neustart: %+v", st)
	}

	if err := m.Resume(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("steuerdatei nach resume: %v", err)
	}
	m, _ = restart(t, path)
	if s, err := m.Admit(AdmitRequest{}); err != nil || m.Status().Since != nil {
		t.Fatalf("nach resume und neustart: %v", err)
	} else {
		s.Release()
	}
}

// TestPauseSurvivesRestart: Die Pause gilt nach einem Neustart weiter.
func TestPauseSurvivesRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "control.json")
	m, _ := restart(t, path)
	if err := m.Pause(); err != nil {
		t.Fatal(err)
	}
	m, _ = restart(t, path)
	if st := m.Status(); !st.Paused || st.Stopped {
		t.Fatalf("%+v", st)
	}
	if _, err := m.Admit(AdmitRequest{}); err == nil || !strings.Contains(err.Error(), "pausiert") {
		t.Fatalf("annahme: %v", err)
	}
}

// TestUnreadableControlStops: Eine kaputte Steuerdatei hebt einen Stopp nicht
// stillschweigend auf, sondern führt zum Sofort-Stopp.
func TestUnreadableControlStops(t *testing.T) {
	path := filepath.Join(t.TempDir(), "control.json")
	if err := os.WriteFile(path, []byte(`{"stopped": tr`), 0o600); err != nil {
		t.Fatal(err)
	}
	m, err := restart(t, path)
	if !errors.Is(err, ErrControlUnreadable) || !m.Status().Stopped {
		t.Fatalf("%v %+v", err, m.Status())
	}
	if _, err := m.Admit(AdmitRequest{}); err == nil {
		t.Fatal("annahme trotz unlesbarer steuerdatei")
	}
}

// TestControlSaveFails: Lässt sich der Zustand nicht speichern, gilt er
// trotzdem sofort; der Fehler sagt nur, dass er keinen Neustart übersteht.
func TestControlSaveFails(t *testing.T) {
	m, _ := restart(t, filepath.Join(t.TempDir(), "fehlt", "control.json"))
	if err := m.EmergencyStop(); err == nil {
		t.Fatal("speicherfehler nicht gemeldet")
	}
	if _, err := m.Admit(AdmitRequest{}); err == nil {
		t.Fatal("sofort-stopp wirkt nicht")
	}
}
