// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Rainer Sestig und Mitwirkende.

package profile

import (
	"math"
	"slices"
)

// Regler für den Aufwand (spec/cooperation.md § 4), je Aufgabenkategorie:
//
//   - Qualität: Ist ist die geglättete Erfolgsquote der Endergebnisse
//     (Rückmeldungen), Stellgröße der Mindestaufwand, den der Bandit wählen
//     darf (Stufe 0 bis 3, siehe effort). Liegt die Quote unter dem Sollwert,
//     steigt der Mindestaufwand; liegt sie darüber, fällt er bis 0 (keine
//     Vorgabe).
//   - Kaskade: Ist ist die Fehlerquote der günstigen Stufe (nicht eskalierte
//     Kaskaden mit Rückmeldung), Stellgröße die Konfidenzschwelle θ. Meldet
//     der Prüfer „ok“ mit einer Konfidenz unter θ, wird eskaliert.
//
// Beide sind PI-Regler mit Glättung (EWMA), Totzone und Anti-Windup. Eine
// Rückmeldung zählt mit ihrem Gewicht (objektiv 1, Nutzer ¼), so dass
// Nutzerfeedback den Regler langsamer bewegt.

// effort: Aufwandsstufe je Muster (Modelle und Prüfrunden).
var effort = map[string]int{
	"single": 0, "parallel": 1, "cascade": 1, "draft_verify_synthesize": 2, "parallel_verify": 3,
}

// maxEffort: höchste Stufe.
const maxEffort = 3

// DefaultCascadeError: zulässige Fehlerquote der günstigen Stufe der Kaskade,
// bis die Konfiguration eine andere nennt.
const DefaultCascadeError = 0.05

// ControlState ist der Zustand eines Reglers (GET /api/profile).
type ControlState struct {
	Samples  float64 `json:"samples"`  // gewichtete Zahl der Messungen
	Filtered float64 `json:"filtered"` // geglättete Ist-Größe
	Integral float64 `json:"integral"`
	Output   float64 `json:"output"` // stetige Stellgröße
	// Level: ganzzahlige Stellgröße (nur Qualität), mit Hysterese und Abklingzeit.
	Level       int     `json:"level,omitempty"`
	SinceChange float64 `json:"since_change,omitempty"`
	// Levels: gemessene Quote je Stufe (nur Qualität), damit der Regler
	// nicht immer wieder eine Stufe probiert, die den Sollwert verfehlt.
	Levels map[int]*LevelStat `json:"levels,omitempty"`
}

// LevelStat: Quote auf einer Aufwandsstufe.
type LevelStat struct {
	Quality float64 `json:"quality"` // geglättet
	Samples float64 `json:"samples"`
	// Age: gewichtete Messungen seit der letzten auf dieser Stufe; nach
	// levelForget gilt das Wissen als veraltet, und die Stufe darf wieder
	// probiert werden (Modelle können besser werden).
	Age float64 `json:"age"`
}

// Controls: Regler einer Kategorie.
type Controls struct {
	Quality ControlState `json:"quality"`
	Cascade ControlState `json:"cascade"`
}

type piParams struct {
	target, kp, ki, alpha, deadBand, min, max float64
	// relaxBand: Totzone, wenn die Stellgröße sinken soll (0: wie
	// deadBand). Breiter als deadBand verhindert beim ganzzahligen
	// Aufwand einen Grenzzyklus, wenn keine Stufe den Sollwert genau trifft:
	// Der Aufwand sinkt erst bei deutlichem Überschuss.
	relaxBand float64
	// sign +1: Ist unter Soll erhöht die Stellgröße (Qualität); -1: Ist über
	// Soll erhöht sie (Fehlerquote).
	sign float64
}

// Reglerparameter: kleine Verstärkungen gegen Schwingen, Glättung α = 0,1
// und Totzone ±0,02 wie in spec/cooperation.md § 4.2; beim Qualitätsregler
// sinkt der Aufwand erst ab 0,05 über dem Sollwert.
var (
	qualityParams = piParams{kp: 2, ki: 0.05, alpha: 0.1, deadBand: 0.02, relaxBand: 0.05, min: 0, max: maxEffort, sign: 1}
	cascadeParams = piParams{kp: 0.5, ki: 0.05, alpha: 0.1, deadBand: 0.02, min: 0, max: 0.95, sign: -1}
)

// Hysterese, Abklingzeit und Gedächtnis der ganzzahligen Stellgröße.
const (
	levelHysteresis = 0.15
	levelCooldown   = 20   // gewichtete Messungen zwischen zwei Änderungen
	levelAlpha      = 0.05 // Glättung der Quote je Stufe
	levelMinSamples = 20   // ab so vielen Messungen zählt die Quote einer Stufe
	levelForget     = 1000 // danach darf eine verfehlte Stufe wieder probiert werden
)

// update verarbeitet eine Messung x mit Gewicht w (0 < w ≤ 1).
func (c *ControlState) update(p piParams, x, w float64) {
	if c.Samples == 0 {
		c.Filtered = x
	} else {
		c.Filtered += p.alpha * w * (x - c.Filtered)
	}
	c.Samples += w
	c.SinceChange += w
	e := p.sign * (p.target - c.Filtered)
	band := p.deadBand
	if e < 0 && p.relaxBand > 0 {
		band = p.relaxBand
	}
	if math.Abs(e) > band {
		// Anti-Windup: an der Grenze wird nicht weiter integriert.
		integral := c.Integral + e*w
		u := p.kp*e + p.ki*integral
		switch {
		case u > p.max:
			u = p.max
		case u < p.min:
			u = p.min
		default:
			c.Integral = integral
		}
		c.Output = u
	}
}

// observeLevel merkt sich die Messung x für die aktuelle Stufe.
func (c *ControlState) observeLevel(x, w float64) {
	if c.Levels == nil {
		c.Levels = map[int]*LevelStat{}
	}
	for _, s := range c.Levels {
		s.Age += w
	}
	s := c.Levels[c.Level]
	if s == nil {
		s = &LevelStat{Quality: x}
		c.Levels[c.Level] = s
	}
	s.Quality += levelAlpha * w * (x - s.Quality)
	s.Samples += w
	s.Age = 0
}

// applyLevel überträgt die stetige Stellgröße auf eine ganze Stufe: erst wenn
// sie die Mitte zur Nachbarstufe deutlich überschreitet und die letzte
// Änderung lange genug zurückliegt. Nach unten nur, wenn die tiefere Stufe
// den Sollwert nicht nachweislich (und vor kurzem) verfehlt hat; sonst
// bleibt die Stufe, und das Integral wird so begrenzt, dass es nicht weiter
// nach unten drängt.
func (c *ControlState) applyLevel(p piParams) {
	want := c.Level
	if c.Output > float64(c.Level)+0.5+levelHysteresis || c.Output < float64(c.Level)-0.5-levelHysteresis {
		want = int(math.Round(c.Output))
	}
	if want < c.Level {
		if s := c.Levels[want]; s != nil && s.Samples >= levelMinSamples && s.Age < levelForget && s.Quality < p.target-p.deadBand {
			want = c.Level
			c.Integral = max(c.Integral, (float64(c.Level)-0.5)/p.ki)
		}
	}
	if want != c.Level && c.SinceChange >= levelCooldown {
		c.Level, c.SinceChange = want, 0
	}
}

func (p *Profile) controls(cat string) *Controls {
	if p.ctl == nil {
		p.ctl = map[string]*Controls{}
	}
	c := p.ctl[cat]
	if c == nil {
		c = &Controls{}
		p.ctl[cat] = c
	}
	return c
}

// SetSelection übernimmt Sollwerte und Einstellungen der Konfiguration für
// die Regler.
func (p *Profile) SetSelection(sel Selection) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sel = sel
}

// controlFeedback: Rückmeldung zum Endergebnis eines Auftrags (unter p.mu).
func (p *Profile) controlFeedback(t *task, score, weight float64) {
	cat := Category(t.skill)
	c := p.controls(cat)
	qp := qualityParams
	qp.target = p.sel.target(cat)
	c.Quality.observeLevel(score, weight)
	c.Quality.update(qp, score, weight)
	c.Quality.applyLevel(qp)
	if t.pattern == "cascade" && !t.escalated {
		cp := cascadeParams
		cp.target = p.sel.cascadeError(cat)
		c.Cascade.update(cp, 1-score, weight)
	}
}

// Theta: Konfidenzschwelle der Kaskade für die Kategorie von skill; 0 heißt,
// das Kriterium ist aus.
func (p *Profile) Theta(skill string) float64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	if c := p.ctl[Category(skill)]; c != nil {
		return c.Cascade.Output
	}
	return 0
}

// MinEffort: Mindestaufwand, den der Qualitätsregler für die Kategorie von
// skill vorgibt.
func (p *Profile) MinEffort(skill string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	if c := p.ctl[Category(skill)]; c != nil {
		return c.Quality.Level
	}
	return 0
}

// Controls liefert den Zustand aller Regler (GET /api/profile).
func (p *Profile) Controls() map[string]Controls {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make(map[string]Controls, len(p.ctl))
	for k, v := range p.ctl {
		out[k] = *v
	}
	return out
}

// withEffort behält die Muster mit mindestens min Aufwand; erfüllt keins die
// Vorgabe, die aufwendigsten verfügbaren.
func withEffort(feasible []string, min int) []string {
	if min <= 0 {
		return feasible
	}
	out := slices.DeleteFunc(slices.Clone(feasible), func(p string) bool { return effort[p] < min })
	if len(out) > 0 {
		return out
	}
	top := -1
	for _, p := range feasible {
		top = max(top, effort[p])
	}
	return slices.DeleteFunc(slices.Clone(feasible), func(p string) bool { return effort[p] < top })
}
