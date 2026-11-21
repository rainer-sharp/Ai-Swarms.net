// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Rainer Sestig. Kommerzielle Lizenz: siehe COMMERCIAL.md.

package profile

import (
	"fmt"
	"math/rand/v2"
	"path/filepath"
	"testing"
)

// simulate legt n Aufträge mit Muster pattern an, deren Endergebnis mit
// Wahrscheinlichkeit q objektiv richtig ist.
func simulate(t *testing.T, p *Profile, rng *rand.Rand, pattern string, tokens int, q float64, n int) {
	t.Helper()
	for range n {
		seq++
		id := fmt.Sprintf("b%d", seq)
		p.Record(TaskInfo{ID: id, Skill: "code.python", Pattern: pattern, Tokens: tokens,
			Steps: []Step{{Role: RoleSpecialist, Agent: "a", Family: "qwen", OK: true}}})
		if err := p.Feedback(Feedback{TaskID: id, Good: rng.Float64() < q, Objective: true}); err != nil {
			t.Fatal(err)
		}
	}
}

func choices(p *Profile, rng *rand.Rand, feasible []string, def string, sel Selection, n int) map[string]int {
	out := map[string]int{}
	for range n {
		out[p.Choose("code.python", feasible, def, sel, rng)]++
	}
	return out
}

var codeRecipes = []string{"single", "draft_verify_synthesize", "parallel_verify"}

// TestBanditPrefersCheapWhenGoodEnough: Erreicht das günstige Muster den
// Sollwert, nimmt der Bandit es statt des teuren.
func TestBanditPrefersCheapWhenGoodEnough(t *testing.T) {
	p := Open("")
	rng := rand.New(rand.NewPCG(3, 4))
	simulate(t, p, rng, "single", 500, 0.97, 60)
	simulate(t, p, rng, "draft_verify_synthesize", 1600, 0.97, 60)
	simulate(t, p, rng, "parallel_verify", 3400, 0.98, 60)
	got := choices(p, rng, codeRecipes, "draft_verify_synthesize", Selection{}, 200)
	if got["single"] < 150 {
		t.Fatalf("wahl %v", got)
	}
}

// TestBanditAvoidsWeakRecipe: Verfehlt das günstige Muster den Sollwert
// deutlich, wählt der Bandit das bessere, auch wenn es teurer ist.
func TestBanditAvoidsWeakRecipe(t *testing.T) {
	p := Open("")
	rng := rand.New(rand.NewPCG(5, 6))
	simulate(t, p, rng, "single", 500, 0.5, 60)
	simulate(t, p, rng, "draft_verify_synthesize", 1600, 0.96, 60)
	got := choices(p, rng, codeRecipes[:2], "single", Selection{}, 200)
	if got["draft_verify_synthesize"] < 150 {
		t.Fatalf("wahl %v", got)
	}
}

// TestBanditPriorAndExploration: Ohne Daten hat das Muster nach Regel einen
// Vorsprung; mit Erkundung 1 wählt der Bandit gleich verteilt; Sollwerte
// lassen sich je Kategorie setzen.
func TestBanditPriorAndExploration(t *testing.T) {
	p := Open("")
	rng := rand.New(rand.NewPCG(7, 8))
	if got := choices(p, rng, codeRecipes, "draft_verify_synthesize", Selection{}, 300); got["draft_verify_synthesize"] < 120 {
		t.Fatalf("ohne daten %v", got)
	}
	got := choices(p, rng, codeRecipes, "draft_verify_synthesize", Selection{Exploration: 1}, 300)
	for _, r := range codeRecipes {
		if got[r] < 70 {
			t.Fatalf("erkundung %v", got)
		}
	}
	if (Selection{Targets: map[string]float64{"code": 0.7}}).target("code") != 0.7 || (Selection{}).target("vision") != 0.85 {
		t.Fatal("sollwerte")
	}
	if p.Choose("code.python", nil, "single", Selection{}, rng) != "" {
		t.Fatal("ohne besetzbare muster")
	}
}

func TestCategoryRatingAndPersistence(t *testing.T) {
	for skill, want := range map[string]string{"code.python": "code", "code.review": "code", "reasoning.math": "math", "vision.ocr": "vision", "general": "general"} {
		if Category(skill) != want {
			t.Fatalf("%s → %s", skill, Category(skill))
		}
	}
	path := filepath.Join(t.TempDir(), "profile.json")
	p := Open(path)
	rng := rand.New(rand.NewPCG(9, 10))
	simulate(t, p, rng, "single", 400, 1, 2)
	if _, ok := p.Rating("a", "code.python"); ok {
		t.Fatal("bewertung nach zwei partien schon gültig")
	}
	simulate(t, p, rng, "single", 600, 1, 1)
	if r, ok := p.Rating("a", "code.python"); !ok || r <= 1500 {
		t.Fatalf("bewertung %v %v", r, ok)
	}
	st := Open(path).Recipes()["code"]["single"]
	if st.Tasks != 3 || st.Alpha != 3 || st.Beta != 0 || st.Tokens < 466 || st.Tokens > 467 {
		t.Fatalf("nach neustart %+v", st)
	}
}
