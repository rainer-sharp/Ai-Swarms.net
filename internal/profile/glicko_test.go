// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Rainer Sestig. Kommerzielle Lizenz: siehe COMMERCIAL.md.

package profile

import (
	"math"
	"testing"
	"time"
)

// TestGlickmanExample rechnet das Beispiel aus Glickman, „Example of the
// Glicko-2 system“, nach: Spieler 1500/200/0,06 gegen 1400/30 (gewonnen),
// 1550/100 und 1700/300 (verloren) ergibt 1464,06 / 151,52 / 0,05999.
func TestGlickmanExample(t *testing.T) {
	now := time.Now()
	r := Rating{R: 1500, RD: 200, Sigma: 0.06, Updated: now}
	got := r.Update([]Match{
		{OppR: 1400, OppRD: 30, Score: 1, Weight: 1},
		{OppR: 1550, OppRD: 100, Score: 0, Weight: 1},
		{OppR: 1700, OppRD: 300, Score: 0, Weight: 1},
	}, now)
	if math.Abs(got.R-1464.06) > 0.05 || math.Abs(got.RD-151.52) > 0.05 || math.Abs(got.Sigma-0.05999) > 0.00001 {
		t.Fatalf("r=%.2f rd=%.2f sigma=%.5f", got.R, got.RD, got.Sigma)
	}
	if got.Games != 3 {
		t.Fatalf("games %v", got.Games)
	}
}

// TestDecay: Ohne Beobachtung wächst die Unsicherheit, höchstens bis 350.
func TestDecay(t *testing.T) {
	now := time.Now()
	r := Rating{R: 1700, RD: 60, Sigma: 0.06, Updated: now}
	later := r.Current(now.Add(90 * 24 * time.Hour))
	if later.RD <= 60 || later.R != 1700 {
		t.Fatalf("nach 90 tagen: %+v", later)
	}
	if r.Current(now.Add(100*365*24*time.Hour)).RD > defaultRD+1e-9 {
		t.Fatal("unsicherheit über dem startwert")
	}
}

// TestWeightedMatchesMoveLess: Eine Partie mit halbem Gewicht bewegt weniger
// als eine volle.
func TestWeightedMatchesMoveLess(t *testing.T) {
	now := time.Now()
	r := NewRating(5, now)
	ref := Match{OppR: referenceR, OppRD: referenceRD, Score: 1}
	full, half := ref, ref
	full.Weight, half.Weight = 1, 0.5
	if a, b := r.Update([]Match{full}, now).R, r.Update([]Match{half}, now).R; !(a > b && b > r.R) {
		t.Fatalf("voll %.1f, halb %.1f, start %.1f", a, b, r.R)
	}
	if NewRating(8, now).R != 1650 || NewRating(0, now).R != 1500 {
		t.Fatal("startwert aus strength")
	}
}
