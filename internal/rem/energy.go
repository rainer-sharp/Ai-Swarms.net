// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Rainer Sestig. Kommerzielle Lizenz: siehe COMMERCIAL.md.

package rem

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/rainer-sharp/Ai-Swarms.net/internal/config"
)

// EnergySample ist ein lokaler Messwert; er verlässt den Knoten nie.
type EnergySample struct {
	At           time.Time
	EinspeisungW float64  // positiv = Überschuss ins Netz
	AkkuSoc      *float64 // Prozent, optional
	PreisEurKWh  *float64 // optional
}

type EnergySource interface {
	Read(ctx context.Context) (EnergySample, error)
}

// StaticSource liefert fest eingestellte Werte (Tests, Inselbetrieb ohne Messung).
type StaticSource struct {
	mu sync.Mutex
	S  EnergySample
}

func (s *StaticSource) Set(sample EnergySample) {
	s.mu.Lock()
	s.S = sample
	s.mu.Unlock()
}

func (s *StaticSource) Read(context.Context) (EnergySample, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.S
	out.At = time.Now()
	return out, nil
}

// HomeAssistantSource liest Zustände über die REST-API von Home Assistant (nur lesend).
type HomeAssistantSource struct {
	Cfg    config.HomeAssistant
	Client *http.Client
}

func (h *HomeAssistantSource) state(ctx context.Context, entity string) (float64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.Cfg.URL+"/api/states/"+entity, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+os.Getenv(h.Cfg.TokenEnv))
	resp, err := h.Client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("home assistant %s: %s", entity, resp.Status)
	}
	var st struct {
		State string `json:"state"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&st); err != nil {
		return 0, err
	}
	return strconv.ParseFloat(st.State, 64)
}

func (h *HomeAssistantSource) Read(ctx context.Context) (EnergySample, error) {
	s := EnergySample{At: time.Now()}
	if h.Cfg.GridExport == "" {
		return s, fmt.Errorf("home_assistant.einspeisung nicht gesetzt")
	}
	v, err := h.state(ctx, h.Cfg.GridExport)
	if err != nil {
		return s, err
	}
	s.EinspeisungW = v
	if h.Cfg.BatterySoc != "" {
		if v, err := h.state(ctx, h.Cfg.BatterySoc); err == nil {
			s.AkkuSoc = &v
		}
	}
	if h.Cfg.Price != "" {
		if v, err := h.state(ctx, h.Cfg.Price); err == nil {
			s.PreisEurKWh = &v
		}
	}
	return s, nil
}

// PVController schaltet die Energiefreigabe über Schwellen mit Hysterese.
//
//	freigabe_W = max(0, einspeisung_W − reserve_W)
//	ein,  wenn freigabe_W ≥ start_above_w für start_stable_s
//	aus,  wenn freigabe_W < stop_below_w  für stop_stable_s
type PVController struct {
	cfg       config.Energy
	on        bool
	condSince time.Time
	last      *EnergySample
}

func NewPVController(cfg config.Energy) *PVController { return &PVController{cfg: cfg} }

func (p *PVController) Update(s EnergySample) {
	p.last = &s
	free := s.EinspeisungW - p.cfg.ReserveW
	if free < 0 {
		free = 0
	}
	var cond bool
	var hold time.Duration
	if p.on {
		cond = free < p.cfg.StopBelowW
		hold = time.Duration(p.cfg.StopStableS) * time.Second
	} else {
		cond = free >= p.cfg.StartAboveW
		hold = time.Duration(p.cfg.StartStableS) * time.Second
	}
	if !cond {
		p.condSince = time.Time{}
		return
	}
	if p.condSince.IsZero() {
		p.condSince = s.At
	}
	if s.At.Sub(p.condSince) >= hold {
		p.on = !p.on
		p.condSince = time.Time{}
	}
}

func (p *PVController) On() bool { return p.on }

// Decide meldet, ob neue energieabhängige Jobs starten dürfen.
func (p *PVController) Decide(now time.Time) (bool, string) {
	c := p.cfg
	if c.Mode == "off" || c.Mode == "" {
		return true, ""
	}
	maxAge := time.Duration(c.ReadingMaxAgeS) * time.Second
	if p.last == nil || now.Sub(p.last.At) > maxAge {
		if c.StaleReadings == "ignore" {
			return true, ""
		}
		return false, "energie-messwerte fehlen oder sind veraltet"
	}
	if s := p.last.AkkuSoc; s != nil && *s < c.BatterySocMin {
		return false, fmt.Sprintf("hausakku %.0f %% unter mindestladestand", *s)
	}
	priceOK := p.last.PreisEurKWh != nil && *p.last.PreisEurKWh <= c.PriceMaxEurKWh
	switch c.Mode {
	case "pv_only":
		if p.on {
			return true, ""
		}
		return false, "kein pv-überschuss"
	case "price_only":
		if priceOK {
			return true, ""
		}
		return false, "strompreis über grenze oder unbekannt"
	case "pv_preferred":
		if p.on || priceOK {
			return true, ""
		}
		return false, "kein pv-überschuss und strompreis über grenze"
	}
	return false, "unbekannter energiemodus"
}
