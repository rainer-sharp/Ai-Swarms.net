// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Rainer Sestig und Mitwirkende.

// Package profile führt das Kompetenzprofil eines Knotens (spec/cooperation.md
// § 2): je Agent und Fähigkeit eine Glicko-2-Bewertung aus eigenen
// Beobachtungen, dazu eine Prüferbewertung. Es sammelt nur; die Auswahl der
// Agenten nutzt es erst ab Stufe 2 (Bandit).
package profile

import (
	"math"
	"time"
)

// Glicko-2 nach Glickman, „Example of the Glicko-2 system“ (2013).
const (
	glickoScale  = 173.7178
	defaultR     = 1500.0
	defaultRD    = 350.0
	defaultSigma = 0.06
	tau          = 0.5
	// ratingPeriod: Zeitraum, nach dem die Unsicherheit eines nicht
	// beobachteten Agenten einmal um σ wächst.
	ratingPeriod = 24 * time.Hour
	convergence  = 1e-6
)

// Rating ist eine Glicko-2-Bewertung auf der üblichen Skala (1500 ± 350).
type Rating struct {
	R       float64   `json:"r"`
	RD      float64   `json:"rd"`
	Sigma   float64   `json:"sigma"`
	Games   float64   `json:"games"` // Summe der Gewichte aller Partien
	Updated time.Time `json:"updated"`
}

// NewRating: Startwert aus der Selbstauskunft strength (1–10, 0 = unbekannt),
// r₀ = 1500 + 50·(strength−5); sie prägt nur den Anfang.
func NewRating(strength int, now time.Time) Rating {
	r := defaultR
	if strength > 0 {
		r += 50 * float64(strength-5)
	}
	return Rating{R: r, RD: defaultRD, Sigma: defaultSigma, Updated: now}
}

// Match ist eine Partie aus Sicht des bewerteten Agenten.
type Match struct {
	OppR, OppRD float64
	Score       float64 // 1 gewonnen, 0 verloren, 0.5 unentschieden
	Weight      float64 // 1 objektiv, 0,5 Prüferurteil, 0,25 Nutzerfeedback
}

func g(phi float64) float64 { return 1 / math.Sqrt(1+3*phi*phi/(math.Pi*math.Pi)) }

func expect(mu, muJ, phiJ float64) float64 { return 1 / (1 + math.Exp(-g(phiJ)*(mu-muJ))) }

// decay lässt die Unsicherheit für jede vergangene Periode ohne Beobachtung
// wachsen, höchstens bis zum Startwert.
func (r Rating) decay(now time.Time) Rating {
	periods := now.Sub(r.Updated).Hours() / ratingPeriod.Hours()
	if periods <= 0 {
		return r
	}
	phi := r.RD / glickoScale
	phi = math.Min(math.Sqrt(phi*phi+r.Sigma*r.Sigma*periods), defaultRD/glickoScale)
	r.RD = phi * glickoScale
	r.Updated = now
	return r
}

// Current: die Bewertung mit dem Unsicherheitszuwachs bis now.
func (r Rating) Current(now time.Time) Rating { return r.decay(now) }

// Update rechnet eine Bewertungsperiode mit den Partien ms. Gewichte gehen als
// Bruchteile einer Partie in v und Δ ein (übliche Erweiterung für Signale
// unterschiedlicher Verlässlichkeit; spec/cooperation.md § 2.2).
func (r Rating) Update(ms []Match, now time.Time) Rating {
	r = r.decay(now)
	mu, phi, sigma := (r.R-defaultR)/glickoScale, r.RD/glickoScale, r.Sigma
	var vInv, sum, games float64
	for _, m := range ms {
		if m.Weight <= 0 {
			continue
		}
		muJ, phiJ := (m.OppR-defaultR)/glickoScale, m.OppRD/glickoScale
		e := expect(mu, muJ, phiJ)
		vInv += m.Weight * g(phiJ) * g(phiJ) * e * (1 - e)
		sum += m.Weight * g(phiJ) * (m.Score - e)
		games += m.Weight
	}
	if vInv == 0 {
		return r
	}
	v := 1 / vInv
	delta := v * sum
	sigma = newSigma(delta, phi, v, sigma)
	phiStar := math.Sqrt(phi*phi + sigma*sigma)
	phiNew := 1 / math.Sqrt(1/(phiStar*phiStar)+1/v)
	muNew := mu + phiNew*phiNew*sum
	return Rating{R: muNew*glickoScale + defaultR, RD: phiNew * glickoScale, Sigma: sigma, Games: r.Games + games, Updated: now}
}

// newSigma: neue Volatilität nach Schritt 5 des Verfahrens (Illinois-Variante
// der Regula falsi).
func newSigma(delta, phi, v, sigma float64) float64 {
	a := math.Log(sigma * sigma)
	f := func(x float64) float64 {
		ex := math.Exp(x)
		d := phi*phi + v + ex
		return ex*(delta*delta-d)/(2*d*d) - (x-a)/(tau*tau)
	}
	A := a
	var B float64
	if delta*delta > phi*phi+v {
		B = math.Log(delta*delta - phi*phi - v)
	} else {
		k := 1.0
		for f(a-k*tau) < 0 {
			k++
		}
		B = a - k*tau
	}
	fA, fB := f(A), f(B)
	for i := 0; math.Abs(B-A) > convergence && i < 100; i++ {
		C := A + (A-B)*fA/(fB-fA)
		fC := f(C)
		if fC*fB <= 0 {
			A, fA = B, fB
		} else {
			fA /= 2
		}
		B, fB = C, fC
	}
	return math.Exp(A / 2)
}
