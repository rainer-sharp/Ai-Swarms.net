// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Rainer Sestig und Mitwirkende.

package rem

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/rainer-sharp/Ai-Swarms.net/internal/config"
)

func at(s string) time.Time {
	t, err := time.ParseInLocation("2006-01-02 15:04", s, time.Local)
	if err != nil {
		panic(err)
	}
	return t
}

func TestInWindow(t *testing.T) {
	ws := config.Default().Resources.Schedule.Windows // mo–fr 22–07, sa/so ganztägig
	cases := map[string]bool{
		"2026-10-14 23:00": true,  // Mi abends
		"2026-10-15 06:59": true,  // Do früh, Fenster vom Mi
		"2026-10-15 07:00": false, // Ende exklusiv
		"2026-10-14 12:00": false, // Mi mittags
		"2026-10-17 12:00": true,  // Sa
		"2026-10-17 03:00": true,  // Sa früh: Fr-Fenster und Sa ganztägig
		"2026-10-19 03:00": false, // Mo früh: So endet um 24:00, Mo-Fenster beginnt 22:00
	}
	for ts, want := range cases {
		if got := InWindow(at(ts), ws); got != want {
			t.Errorf("%s: got %v want %v", ts, got, want)
		}
	}
	if !InWindow(at("2026-10-14 12:00"), nil) {
		t.Error("ohne fenster muss immer frei sein")
	}
}

func TestPVHysteresis(t *testing.T) {
	cfg := config.Default().Resources.Energy
	cfg.Mode = "pv_only"
	p := NewPVController(cfg)
	t0 := at("2026-10-14 12:00")
	feed := func(offset time.Duration, w float64) {
		p.Update(EnergySample{At: t0.Add(offset), EinspeisungW: w})
	}
	// freigabe = 700-150 = 550 ≥ 500, muss 300 s stabil sein
	feed(0, 700)
	feed(299*time.Second, 700)
	if p.On() {
		t.Fatal("zu früh eingeschaltet")
	}
	feed(300*time.Second, 700)
	if !p.On() {
		t.Fatal("nicht eingeschaltet")
	}
	// kurzer Einbruch unter 250 W freigabe: 120 s nötig
	feed(310*time.Second, 300) // freigabe 150
	feed(400*time.Second, 700) // erholt → Zähler zurück
	feed(500*time.Second, 300)
	feed(619*time.Second, 300)
	if !p.On() {
		t.Fatal("zu früh ausgeschaltet")
	}
	feed(620*time.Second, 300)
	if p.On() {
		t.Fatal("nicht ausgeschaltet")
	}
	// zwischen den Schwellen bleibt der Zustand
	if ok, _ := p.Decide(t0.Add(621 * time.Second)); ok {
		t.Fatal("freigabe trotz aus")
	}
	// veraltete Messwerte → keine neuen Jobs
	if ok, reason := p.Decide(t0.Add(time.Hour)); ok || !strings.Contains(reason, "veraltet") {
		t.Fatalf("veraltete messwerte: %v %q", ok, reason)
	}
}

func TestPVBattery(t *testing.T) {
	cfg := config.Default().Resources.Energy
	cfg.Mode = "pv_preferred"
	p := NewPVController(cfg)
	soc, price := 30.0, 0.10
	now := time.Now()
	p.Update(EnergySample{At: now, AkkuSoc: &soc, PreisEurKWh: &price})
	if ok, _ := p.Decide(now); ok {
		t.Fatal("akku unter mindestladestand ignoriert")
	}
	soc = 80
	p.Update(EnergySample{At: now, AkkuSoc: &soc, PreisEurKWh: &price})
	if ok, r := p.Decide(now); !ok {
		t.Fatalf("günstiger strom abgelehnt: %s", r)
	}
}

func newTestManager(t *testing.T, mutate func(*config.Resources)) *Manager {
	cfg := config.Default().Resources
	cfg.Schedule.Windows = nil
	cfg.Energy.Mode = "off"
	cfg.AcceptPublicTasks = false
	if mutate != nil {
		mutate(&cfg)
	}
	m, err := New(cfg, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestAdmitLimits(t *testing.T) {
	m := newTestManager(t, nil)
	if _, err := m.Admit(AdmitRequest{Public: true}); err == nil {
		t.Fatal("öffentlicher auftrag trotz opt-out angenommen")
	}
	s, err := m.Admit(AdmitRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Admit(AdmitRequest{}); err == nil {
		t.Fatal("max_parallel_jobs überschritten")
	}
	s.Release()
	s.Release() // idempotent
	if _, err := m.Admit(AdmitRequest{}); err != nil {
		t.Fatal(err)
	}
}

func TestOwnUseCancelsForeign(t *testing.T) {
	m := newTestManager(t, nil)
	s, err := m.Admit(AdmitRequest{})
	if err != nil {
		t.Fatal(err)
	}
	done := m.BeginOwnUse()
	select {
	case <-s.Context().Done():
	case <-time.After(time.Second):
		t.Fatal("fremdjob nicht abgebrochen")
	}
	if c := context.Cause(s.Context()); c == nil || !strings.Contains(c.Error(), "eigenbedarf") {
		t.Fatalf("falscher grund: %v", c)
	}
	s.Release()
	if _, err := m.Admit(AdmitRequest{}); err == nil {
		t.Fatal("annahme trotz eigener nutzung")
	}
	done()
	if _, err := m.Admit(AdmitRequest{}); err != nil {
		t.Fatal(err)
	}
}

func TestGPUMeanAndTemperature(t *testing.T) {
	m := newTestManager(t, nil)
	now := time.Now()
	// kurze 100-%-Spitze, Mittel bleibt unter 60 %
	for i, u := range []float64{10, 100, 10, 10} {
		m.ObserveHW(HWSample{At: now.Add(time.Duration(i) * 5 * time.Second), GPUUtil: u, GPUTempC: 60, RAMAvailGiB: -1})
	}
	s, err := m.Admit(AdmitRequest{})
	if err != nil {
		t.Fatalf("kurzzeitige spitze blockiert: %v", err)
	}
	m.ObserveHW(HWSample{At: now.Add(25 * time.Second), GPUUtil: 10, GPUTempC: 90, RAMAvailGiB: -1})
	if s.Context().Err() == nil {
		t.Fatal("übertemperatur bricht nicht ab")
	}
}

func TestGPUMinutesPersist(t *testing.T) {
	path := t.TempDir() + "/usage.json"
	u := LoadUsage(path)
	now := time.Now()
	if err := u.Add(now, 3*time.Minute); err != nil {
		t.Fatal(err)
	}
	if got := LoadUsage(path).Used(now); got != 3*time.Minute {
		t.Fatalf("nach neustart %v", got)
	}
	if got := LoadUsage(path).Used(now.Add(48 * time.Hour)); got != 0 {
		t.Fatalf("tageswechsel nicht zurückgesetzt: %v", got)
	}
}

func TestEmergencyStop(t *testing.T) {
	m := newTestManager(t, nil)
	s, _ := m.Admit(AdmitRequest{})
	m.EmergencyStop()
	if s.Context().Err() == nil {
		t.Fatal("sofort-stopp bricht nicht ab")
	}
	s.Release()
	if _, err := m.Admit(AdmitRequest{}); err == nil {
		t.Fatal("annahme nach sofort-stopp")
	}
}
