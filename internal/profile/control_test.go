// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Rainer Sestig und Mitwirkende.

package profile

import (
	"fmt"
	"math"
	"math/rand/v2"
	"path/filepath"
	"testing"
)

// simTask legt einen Auftrag an und meldet sein Endergebnis objektiv zurück.
func simTask(t *testing.T, p *Profile, i int, skill, pattern string, escalated, good bool) {
	t.Helper()
	id := fmt.Sprintf("t%d", i)
	p.Record(TaskInfo{ID: id, Skill: skill, Pattern: pattern, Escalated: escalated,
		Steps: []Step{{Role: RoleSpecialist, Agent: "a", Family: "x", OK: true}}})
	if err := p.Feedback(Feedback{TaskID: id, Good: good, Objective: true}); err != nil {
		t.Fatal(err)
	}
}

// TestQualityControlSimulation: Künstliche Muster mit bekannter Erfolgsquote.
// Der Qualitätsregler hebt den Mindestaufwand, bis die Quote den Sollwert
// erreicht, und bleibt dann dort, ohne zu schwingen. Nur nach levelForget
// probiert er die tiefere Stufe kurz erneut.
func TestQualityControlSimulation(t *testing.T) {
	quality := map[int]float64{0: 0.72, 1: 0.8, 2: 0.93, 3: 0.96} // je Aufwandsstufe
	pattern := map[int]string{0: "single", 1: "cascade", 2: "draft_verify_synthesize", 3: "parallel_verify"}
	p := Open("")
	p.SetSelection(Selection{Targets: map[string]float64{"general": 0.9}})
	rng := rand.New(rand.NewPCG(1, 2))
	changes, last := 0, 0
	var good, n, atTwo float64
	for i := range 4000 {
		lvl := p.MinEffort("general")
		if lvl != last {
			changes++
			last = lvl
		}
		ok := rng.Float64() < quality[lvl]
		simTask(t, p, i, "general", pattern[lvl], false, ok)
		if i >= 500 {
			n++
			if ok {
				good++
			}
			if lvl == 2 {
				atTwo++
			}
		}
	}
	if lvl := p.MinEffort("general"); lvl != 2 {
		t.Fatalf("mindestaufwand %d, erwartet 2 (erste stufe über dem sollwert); regler %+v", lvl, p.Controls()["general"])
	}
	// Anlauf (0 → 1 → 2) und je Probe hin und zurück.
	if probes := 4000 / levelForget; changes > 2+2*probes {
		t.Fatalf("%d wechsel der stufe: regler schwingt", changes)
	}
	if share := atTwo / n; share < 0.95 {
		t.Fatalf("nur %.2f der zeit auf stufe 2", share)
	}
	if q := good / n; q < 0.9 {
		t.Fatalf("erfolgsquote %.3f unter dem sollwert", q)
	}
}

// TestQualityControlRelaxes: Ist die Quote über dem Sollwert, gibt der
// Regler keinen Mindestaufwand vor.
func TestQualityControlRelaxes(t *testing.T) {
	p := Open("")
	p.SetSelection(Selection{Targets: map[string]float64{"code": 0.8}})
	rng := rand.New(rand.NewPCG(3, 4))
	for i := range 500 {
		simTask(t, p, i, "code.go", "single", false, rng.Float64() < 0.95)
	}
	if lvl := p.MinEffort("code.go"); lvl != 0 {
		t.Fatalf("mindestaufwand %d", lvl)
	}
	if got := withEffort([]string{"single", "cascade", "draft_verify_synthesize"}, 2); len(got) != 1 || got[0] != "draft_verify_synthesize" {
		t.Fatalf("withEffort %v", got)
	}
	if got := withEffort([]string{"single", "cascade"}, 3); len(got) != 1 || got[0] != "cascade" {
		t.Fatalf("withEffort ohne passendes muster %v", got)
	}
}

// TestCascadeThetaSimulation: Die günstige Stufe liegt in 20 % der Fälle
// falsch. Der Prüfer erkennt die Hälfte davon; die übrigen meldet er „ok“,
// aber mit geringerer Konfidenz als bei richtigen Lösungen. Der Regler hebt θ,
// bis die Fehlerquote der nicht eskalierten Aufträge beim Sollwert von 5 %
// liegt (ohne θ wären es gut 11 %).
func TestCascadeThetaSimulation(t *testing.T) {
	p := Open("")
	p.SetSelection(Selection{})
	rng := rand.New(rand.NewPCG(5, 6))
	var errs, kept, escalations float64
	var thetas []float64
	for i := range 6000 {
		theta := p.Theta("general")
		wrong := rng.Float64() < 0.2
		issues := wrong && rng.Float64() < 0.5
		conf := 0.7 + 0.3*rng.Float64()
		if wrong {
			conf = 0.4 + 0.4*rng.Float64()
		}
		escalate := issues || conf < theta
		good := !wrong
		if escalate {
			good = rng.Float64() < 0.97
		}
		simTask(t, p, i, "general", "cascade", escalate, good)
		if i >= 4000 {
			thetas = append(thetas, theta)
			if escalate {
				escalations++
			} else {
				kept++
				if wrong {
					errs++
				}
			}
		}
	}
	rate := errs / kept
	lo, hi := math.Inf(1), math.Inf(-1)
	for _, th := range thetas {
		lo, hi = min(lo, th), max(hi, th)
	}
	t.Logf("θ %.2f–%.2f, fehlerquote %.3f, eskaliert %.0f von 2000", lo, hi, rate, escalations)
	if rate > 0.075 {
		t.Fatalf("fehlerquote der günstigen stufe %.3f über dem sollwert", rate)
	}
	if lo < 0.4 || hi > 0.95 {
		t.Fatalf("θ außerhalb des erwarteten bereichs: %.2f–%.2f", lo, hi)
	}
	// Nicht unnötig teuer: höchstens gut ein Drittel wird eskaliert.
	if escalations > 750 {
		t.Fatalf("%.0f eskalationen von 2000", escalations)
	}
}

// TestControlsPersist: Der Zustand der Regler übersteht einen Neustart.
func TestControlsPersist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profile.json")
	p := Open(path)
	p.SetSelection(Selection{})
	for i := range 60 {
		simTask(t, p, i, "general", "cascade", false, i%3 != 0) // ein Drittel falsch
	}
	theta := p.Theta("general")
	if theta <= 0 {
		t.Fatalf("θ %v nach zu vielen fehlern", theta)
	}
	q := Open(path)
	if got := q.Theta("general"); got != theta || q.Controls()["general"].Quality.Samples != 60 {
		t.Fatalf("nach neustart θ %v (vorher %v), regler %+v", got, theta, q.Controls()["general"])
	}
}
