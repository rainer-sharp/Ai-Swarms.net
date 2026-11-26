// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Rainer Sestig und Mitwirkende.

// Package rem ist der lokale Ressourcen- und Energiemanager. Er lässt fremde
// Aufträge nur innerhalb der Betreibergrenzen zu; kein Koordinator und kein
// Regler kann ihn überstimmen. Eigene Nutzung hat Vorrang.
package rem

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/rainer-sharp/Ai-Swarms.net/internal/config"
)

type Manager struct {
	cfg    config.Resources
	hw     HWSensor
	energy EnergySource
	usage  *UsageCounter
	now    func() time.Time

	mu       sync.Mutex
	pv       *PVController
	hwWin    *window
	paused   bool
	stopped  bool
	since    time.Time // seit wann pausiert oder gestoppt
	ctlPath  string    // Steuerdatei (Persist), leer: nur im Speicher
	ownUse   int
	nextID   int
	foreign  map[int]*Slot
	lastDeny string
}

type Slot struct {
	m       *Manager
	id      int
	started time.Time
	ctx     context.Context
	cancel  context.CancelCauseFunc
	once    sync.Once
}

// Context endet, wenn der REM den Job abbricht (Eigenbedarf, Temperatur, Sofort-Stopp).
func (s *Slot) Context() context.Context { return s.ctx }

func (s *Slot) Release() {
	s.once.Do(func() {
		s.cancel(nil)
		m := s.m
		m.mu.Lock()
		delete(m.foreign, s.id)
		m.mu.Unlock()
		now := m.now()
		if err := m.usage.Add(now, now.Sub(s.started)); err != nil {
			slog.Warn("gpu-minuten nicht gespeichert", "err", err)
		}
	})
}

func New(cfg config.Resources, hw HWSensor, energy EnergySource, usage *UsageCounter) (*Manager, error) {
	if err := ValidateWindows(cfg.Schedule.Windows); err != nil {
		return nil, err
	}
	if usage == nil {
		usage = LoadUsage("")
	}
	span := time.Duration(cfg.Hardware.GPUWindowS) * time.Second
	if span <= 0 {
		span = 120 * time.Second
	}
	return &Manager{
		cfg: cfg, hw: hw, energy: energy, usage: usage, now: time.Now,
		pv: NewPVController(cfg.Energy), hwWin: &window{span: span},
		foreign: make(map[int]*Slot),
	}, nil
}

// Run tastet Hardware und Energie periodisch ab, bis ctx endet.
func (m *Manager) Run(ctx context.Context) {
	hwT := time.NewTicker(5 * time.Second)
	enT := time.NewTicker(15 * time.Second)
	defer hwT.Stop()
	defer enT.Stop()
	m.pollHW(ctx)
	m.pollEnergy(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-hwT.C:
			m.pollHW(ctx)
		case <-enT.C:
			m.pollEnergy(ctx)
		}
	}
}

func (m *Manager) pollHW(ctx context.Context) {
	if m.hw == nil {
		return
	}
	m.ObserveHW(m.hw.Sample(ctx))
}

// ObserveHW verarbeitet einen Hardware-Messwert; bei Übertemperatur werden
// laufende Fremdjobs abgebrochen.
func (m *Manager) ObserveHW(s HWSample) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.hwWin.add(s)
	if lim := m.cfg.Hardware.TemperatureStopC; lim > 0 && s.GPUTempC >= lim {
		m.cancelForeignLocked(fmt.Errorf("gpu-temperatur %.0f °C ≥ %.0f °C", s.GPUTempC, lim))
	}
}

func (m *Manager) pollEnergy(ctx context.Context) {
	if m.energy == nil || m.cfg.Energy.Mode == "off" {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	s, err := m.energy.Read(ctx)
	if err != nil {
		slog.Debug("energiequelle", "err", err)
		return // veraltete Werte führen in Decide zur Ablehnung
	}
	m.ObserveEnergy(s)
}

func (m *Manager) ObserveEnergy(s EnergySample) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pv.Update(s)
}

// BeginOwnUse markiert eigene Nutzung. Mit vorrang_eigene_nutzung werden
// laufende Fremdjobs abgebrochen und neue abgelehnt, bis done aufgerufen wird.
func (m *Manager) BeginOwnUse() (done func()) {
	m.mu.Lock()
	m.ownUse++
	if m.cfg.PrioritizeOwnUse {
		m.cancelForeignLocked(fmt.Errorf("eigenbedarf des anbieters"))
	}
	m.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			m.mu.Lock()
			m.ownUse--
			m.mu.Unlock()
		})
	}
}

func (m *Manager) cancelForeignLocked(cause error) {
	for _, s := range m.foreign {
		s.cancel(cause)
	}
}

// AdmitRequest beschreibt einen fremden Auftrag.
type AdmitRequest struct {
	Public bool // von einem nicht vertrauten Peer
}

// Admit prüft alle Grenzen; es gilt die strengste. Bei Erfolg muss der
// Aufrufer Slot.Release aufrufen.
func (m *Manager) Admit(req AdmitRequest) (*Slot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if reason := m.denyLocked(req); reason != "" {
		m.lastDeny = reason
		return nil, fmt.Errorf("abgelehnt: %s", reason)
	}
	ctx, cancel := context.WithCancelCause(context.Background())
	m.nextID++
	s := &Slot{m: m, id: m.nextID, started: m.now(), ctx: ctx, cancel: cancel}
	m.foreign[s.id] = s
	return s, nil
}

func (m *Manager) denyLocked(req AdmitRequest) string {
	c := m.cfg
	now := m.now()
	switch {
	case m.stopped:
		return "teilnahme beendet (sofort-stopp)"
	case m.paused:
		return "manuell pausiert"
	case req.Public && !c.AcceptPublicTasks:
		return "fremde aufträge nicht freigegeben (accept_public_tasks)"
	case c.PrioritizeOwnUse && m.ownUse > 0:
		return "eigene nutzung hat vorrang"
	case len(m.foreign) >= c.Hardware.MaxParallelJobs:
		return "keine freien slots"
	case !InWindow(now, c.Schedule.Windows):
		return "außerhalb der zeitfenster"
	}
	if lim := c.Schedule.GPUMinutesPerDay; lim > 0 && m.usage.Used(now) >= time.Duration(lim*float64(time.Minute)) {
		return "gpu-minuten für heute verbraucht"
	}
	if lim := c.Hardware.GPUUtilMeanMax; lim > 0 {
		if u := m.hwWin.meanGPUUtil(); u >= lim {
			return fmt.Sprintf("mittlere gpu-auslastung %.0f %% ≥ %.0f %%", u, lim)
		}
	}
	if s, ok := m.hwWin.latest(); ok {
		if lim := c.Hardware.TemperatureStopC; lim > 0 && s.GPUTempC >= lim {
			return "gpu-temperatur zu hoch"
		}
		if lim := c.Hardware.RAMReserveGiB; lim > 0 && s.RAMAvailGiB >= 0 && s.RAMAvailGiB < lim {
			return fmt.Sprintf("ram-reserve unterschritten (%.1f GiB frei)", s.RAMAvailGiB)
		}
	}
	if ok, reason := m.pv.Decide(now); !ok {
		return reason
	}
	return ""
}

// Pause, Resume und EmergencyStop wirken sofort. Der Fehler sagt nur, dass
// der Zustand nicht gespeichert werden konnte und einen Neustart nicht übersteht.

func (m *Manager) Pause() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.paused && !m.stopped {
		m.since = m.now()
	}
	m.paused = true
	return m.saveLocked()
}

func (m *Manager) Resume() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.paused, m.stopped, m.since = false, false, time.Time{}
	return m.saveLocked()
}

// EmergencyStop beendet die Teilnahme: laufende Fremdjobs werden abgebrochen,
// künftige Annahmen abgelehnt, auch nach einem Neustart.
func (m *Manager) EmergencyStop() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.stopped {
		m.since = m.now()
	}
	m.stopped = true
	m.cancelForeignLocked(fmt.Errorf("sofort-stopp"))
	return m.saveLocked()
}

// SlotsNow liefert die aktuell freien Slots (0, wenn eine Grenze greift).
// Nur diese Zahl geht ins Netz, nicht der Grund.
func (m *Manager) SlotsNow() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.denyLocked(AdmitRequest{}) != "" {
		return 0
	}
	return m.cfg.Hardware.MaxParallelJobs - len(m.foreign)
}

type Status struct {
	Paused         bool       `json:"paused"`
	Stopped        bool       `json:"stopped"`
	Since          *time.Time `json:"since,omitempty"` // seit wann pausiert oder gestoppt
	OwnUse         bool       `json:"own_use"`
	ForeignRunning int        `json:"foreign_running"`
	SlotsNow       int        `json:"slots_now"`
	GPUMinutesUsed float64    `json:"gpu_minutes_used_today"`
	GPUUtilMean    float64    `json:"gpu_util_mean"`
	PVOn           bool       `json:"pv_on"`
	Blocked        string     `json:"blocked,omitempty"`
}

// Status ist nur für die lokale Oberfläche bestimmt.
func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	reason := m.denyLocked(AdmitRequest{})
	slots := 0
	if reason == "" {
		slots = m.cfg.Hardware.MaxParallelJobs - len(m.foreign)
	}
	var since *time.Time
	if m.paused || m.stopped {
		s := m.since
		since = &s
	}
	return Status{
		Paused: m.paused, Stopped: m.stopped, Since: since, OwnUse: m.ownUse > 0,
		ForeignRunning: len(m.foreign), SlotsNow: slots,
		GPUMinutesUsed: m.usage.Used(m.now()).Minutes(),
		GPUUtilMean:    m.hwWin.meanGPUUtil(),
		PVOn:           m.pv.On(), Blocked: reason,
	}
}
