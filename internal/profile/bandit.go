// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Rainer Sestig. Kommerzielle Lizenz: siehe COMMERCIAL.md.

package profile

import (
	"math"
	"math/rand/v2"
	"strings"
)

// Bandit über Rezepte (spec/cooperation.md § 3): je Aufgabenkategorie und
// Muster eine Beta-Verteilung über die Erfolgsquote und der mittlere
// Token-Verbrauch. Gewählt wird per Thompson Sampling das günstigste Muster,
// dessen gezogene Quote den Sollwert erreicht.

// RecipeStats: Beobachtungen zu einem Muster in einer Kategorie. Alpha und
// Beta zählen gewichtete Erfolge und Misserfolge (ohne Vorwissen).
type RecipeStats struct {
	Alpha  float64 `json:"alpha"`
	Beta   float64 `json:"beta"`
	Tasks  int     `json:"tasks"`  // Aufträge mit diesem Muster
	Tokens float64 `json:"tokens"` // mittlerer Verbrauch je Auftrag
}

// DefaultTargets: Qualitäts-Sollwerte je Kategorie, bis die Konfiguration
// andere nennt (spec/cooperation.md § 4.1).
var DefaultTargets = map[string]float64{"code": 0.9, "math": 0.95, "general": 0.85}

// priorTokens: Verbrauch je Muster, solange zu wenige Aufträge beobachtet
// sind; grob nach Benchmark Runde 1 (bench/report-r1.md).
var priorTokens = map[string]float64{
	"single": 500, "cascade": 1000, "draft_verify_synthesize": 1600, "parallel": 2200, "parallel_verify": 3400,
}

const minTasksForCost = 3

// Category leitet die Aufgabenkategorie aus der Fähigkeit ab.
func Category(skill string) string {
	switch {
	case strings.HasPrefix(skill, "code."):
		return "code"
	case strings.HasPrefix(skill, "reasoning.math"):
		return "math"
	case strings.HasPrefix(skill, "vision.") || strings.HasPrefix(skill, "modality.image"):
		return "vision"
	case strings.HasPrefix(skill, "facts"):
		return "facts"
	}
	return "general"
}

// Selection sind die Einstellungen des Bandits.
type Selection struct {
	Exploration float64            // Anteil zufälliger Wahl unter den besetzbaren Mustern
	Targets     map[string]float64 // Sollwert je Kategorie; fehlt er, gilt DefaultTargets bzw. 0,85
}

func (s Selection) target(cat string) float64 {
	if v, ok := s.Targets[cat]; ok {
		return v
	}
	if v, ok := DefaultTargets[cat]; ok {
		return v
	}
	return 0.85
}

// Choose wählt unter feasible ein Muster für eine Aufgabe der Fähigkeit skill.
// def (das Muster nach Regel) bekommt einen Erfolg Vorsprung als Vorwissen.
func (p *Profile) Choose(skill string, feasible []string, def string, sel Selection, rng *rand.Rand) string {
	if len(feasible) == 0 {
		return ""
	}
	if rng == nil {
		rng = rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64()))
	}
	if rng.Float64() < sel.Exploration {
		return feasible[rng.IntN(len(feasible))]
	}
	cat := Category(skill)
	p.mu.Lock()
	stats := p.recipes[cat]
	p.mu.Unlock()
	target := sel.target(cat)
	best, bestSample := "", -1.0
	cheap, cheapCost := "", math.Inf(1)
	for _, pat := range feasible {
		var st RecipeStats
		if s := stats[pat]; s != nil {
			st = *s
		}
		a, b := 1+st.Alpha, 1+st.Beta
		if pat == def {
			a++
		}
		x := sampleBeta(rng, a, b)
		cost := priorTokens[pat]
		if st.Tasks >= minTasksForCost || cost == 0 {
			cost = st.Tokens
		}
		if x > bestSample {
			best, bestSample = pat, x
		}
		if x >= target && cost < cheapCost {
			cheap, cheapCost = pat, cost
		}
	}
	if cheap != "" {
		return cheap
	}
	return best
}

// Rating: gemessene Bewertung eines Agenten in einer Fähigkeit, sobald sie
// über den Startwert hinaus Gewicht hat (mindestens drei Partien).
func (p *Profile) Rating(agent, skill string) (float64, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	a := p.agents[agent]
	if a == nil || a.Skills[skill] == nil || a.Skills[skill].Games < 3 {
		return 0, false
	}
	return a.Skills[skill].R, true
}

func (p *Profile) recipe(cat, pattern string) *RecipeStats {
	if p.recipes == nil {
		p.recipes = map[string]map[string]*RecipeStats{}
	}
	if p.recipes[cat] == nil {
		p.recipes[cat] = map[string]*RecipeStats{}
	}
	s := p.recipes[cat][pattern]
	if s == nil {
		s = &RecipeStats{}
		p.recipes[cat][pattern] = s
	}
	return s
}

// sampleBeta zieht aus Beta(a, b) über zwei Gamma-Ziehungen.
func sampleBeta(rng *rand.Rand, a, b float64) float64 {
	x, y := sampleGamma(rng, a), sampleGamma(rng, b)
	return x / (x + y)
}

// sampleGamma nach Marsaglia und Tsang (2000); für k < 1 mit dem üblichen
// Aufschlag U^(1/k).
func sampleGamma(rng *rand.Rand, k float64) float64 {
	if k < 1 {
		return sampleGamma(rng, k+1) * math.Pow(rng.Float64(), 1/k)
	}
	d := k - 1.0/3
	c := 1 / math.Sqrt(9*d)
	for {
		x := rng.NormFloat64()
		v := 1 + c*x
		if v <= 0 {
			continue
		}
		v = v * v * v
		u := rng.Float64()
		if u < 1-0.0331*x*x*x*x || math.Log(u) < 0.5*x*x+d*(1-v+math.Log(v)) {
			return d * v
		}
	}
}
