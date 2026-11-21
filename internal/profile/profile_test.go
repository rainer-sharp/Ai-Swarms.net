// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Rainer Sestig. Kommerzielle Lizenz: siehe COMMERCIAL.md.

package profile

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"testing"
)

func ptr(i int) *int { return &i }

func rating(t *testing.T, p *Profile, agent, skill string) Rating {
	t.Helper()
	for _, a := range p.Snapshot() {
		if a.Agent == agent {
			return a.Skills[skill]
		}
	}
	t.Fatalf("agent %s fehlt", agent)
	return Rating{}
}

var seq int

// draftTask legt einen Auftrag „Entwurf + Prüfer“ an und liefert seine ID.
func draftTask(p *Profile, creator, verifier, verdict string) string {
	seq++
	id := fmt.Sprintf("t%d", seq)
	steps := []Step{{Role: RoleSpecialist, Agent: creator, Family: "fam-" + creator, OK: true}}
	if verifier != "" {
		steps = append(steps, Step{Role: RoleVerifier, Agent: verifier, Family: "fam-" + verifier, OK: true, Verdict: verdict})
	}
	p.Record(id, "code.python", steps)
	return id
}

// TestKnownQualitySeparates: Zwei künstliche Agenten mit 90 % und 40 %
// Trefferquote trennen sich nach objektiven Signalen klar (spec § 11, Stufe 1).
func TestKnownQualitySeparates(t *testing.T) {
	p := Open(filepath.Join(t.TempDir(), "profile.json"))
	rng := rand.New(rand.NewPCG(1, 2))
	for range 60 {
		for agent, q := range map[string]float64{"good": 0.9, "bad": 0.4} {
			id := draftTask(p, agent, "", "")
			if err := p.Feedback(Feedback{TaskID: id, Good: rng.Float64() < q, Objective: true}); err != nil {
				t.Fatal(err)
			}
		}
	}
	g, b := rating(t, p, "good", "code.python"), rating(t, p, "bad", "code.python")
	if g.R-b.R < 200 || g.RD > 150 || b.RD > 150 {
		t.Fatalf("gut %.0f±%.0f, schlecht %.0f±%.0f", g.R, g.RD, b.R, b.RD)
	}
	if g.Games != 60 {
		t.Fatalf("partien %v", g.Games)
	}
}

// TestVerifierEarnsWeight: Ein Prüferurteil zählt erst, wenn der Prüfer
// genug objektiv bestätigte Urteile hat; ein Prüfer mit vielen Fehlalarmen
// bekommt kein Gewicht.
func TestVerifierEarnsWeight(t *testing.T) {
	p := Open("")
	start := func() float64 { return rating(t, p, "creator", "code.python").R }

	// Ohne Prüferbewertung bewegt das Urteil nichts.
	draftTask(p, "creator", "careful", "ok")
	if a := p.agents["creator"]; a != nil && a.Skills["code.python"] != nil {
		t.Fatal("urteil eines unbewerteten prüfers gezählt")
	}

	// Fünf zutreffende Urteile, objektiv bestätigt.
	for range verifierMinChecks {
		id := draftTask(p, "other", "careful", "ok")
		if err := p.Feedback(Feedback{TaskID: id, Good: true, Objective: true}); err != nil {
			t.Fatal(err)
		}
	}
	if !p.agents["careful"].Verifier["code.python"].Trusted() {
		t.Fatalf("prüfer nicht anerkannt: %+v", p.agents["careful"].Verifier["code.python"])
	}
	draftTask(p, "creator", "careful", "ok")
	before := start()
	draftTask(p, "creator", "careful", "ok")
	if start() <= before {
		t.Fatal("urteil eines bewährten prüfers zählt nicht")
	}

	// Ein Prüfer, der richtige Ergebnisse bemängelt, sammelt Fehlalarme.
	for range 2 * verifierMinChecks {
		id := draftTask(p, "other", "alarmist", "issues")
		if err := p.Feedback(Feedback{TaskID: id, Good: true, Objective: true}); err != nil {
			t.Fatal(err)
		}
	}
	st := *p.agents["alarmist"].Verifier["code.python"]
	if st.Trusted() || st.FalseAlarm != 2*verifierMinChecks {
		t.Fatalf("fehlalarme: %+v", st)
	}
	before = start()
	draftTask(p, "creator", "alarmist", "issues")
	if start() != before {
		t.Fatal("urteil eines unzuverlässigen prüfers gezählt")
	}
}

// TestFeedbackTargets: Rückmeldungen treffen nur erfolgreiche Ersteller,
// jeder Schritt nur einmal; Prüfer derselben Familie zählen nicht.
func TestFeedbackTargets(t *testing.T) {
	p := Open("")
	steps := []Step{
		{Role: RoleSpecialist, Agent: "a", Family: "qwen", OK: true},
		{Role: RoleSpecialist, Agent: "b", Family: "llama", OK: false},
		{Role: RoleVerifier, Agent: "c", Family: "qwen", OK: true, Verdict: "issues"},
		{Role: RoleSynthesizer, Agent: "d", Family: "gpt-oss", OK: true},
	}
	p.Record("x", "general", steps)
	for _, tc := range []struct {
		fb   Feedback
		want error
	}{
		{Feedback{TaskID: "fehlt", Good: true}, ErrUnknownTask},
		{Feedback{TaskID: "x", Good: true, Step: ptr(1)}, ErrBadStep},
		{Feedback{TaskID: "x", Good: true, Step: ptr(2)}, ErrBadStep},
		{Feedback{TaskID: "x", Good: true, Step: ptr(9)}, ErrBadStep},
		{Feedback{TaskID: "x", Good: true}, nil},
		{Feedback{TaskID: "x", Good: false}, ErrDuplicate},
		{Feedback{TaskID: "x", Good: true, Step: ptr(0), Objective: true}, nil},
	} {
		if err := p.Feedback(tc.fb); !errors.Is(err, tc.want) {
			t.Fatalf("%+v: %v, erwartet %v", tc.fb, err, tc.want)
		}
	}
	// Endergebnis ist die Synthese (d), Nutzerfeedback mit Gewicht ¼.
	if r := rating(t, p, "d", "general"); r.Games != WeightUser {
		t.Fatalf("synthese: %+v", r)
	}
	// Der Prüfer c hat a beurteilt, aber gleiche Familie: kein Prüfersignal,
	// wohl aber ein Fehlalarm in seiner Statistik.
	if st := p.agents["c"].Verifier["general"]; st == nil || st.FalseAlarm != 1 {
		t.Fatalf("prüferstatistik: %+v", st)
	}
	if AgentKey("", "m") != "local/m" || AgentKey("p1", "m") != "p1/m" {
		t.Fatal("agentkey")
	}
}

// TestPersistence: Das Profil übersteht einen Neustart; eine kaputte Datei
// wird beiseitegelegt statt überschrieben.
func TestPersistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "profile.json")
	p := Open(path)
	id := draftTask(p, "a", "", "")
	if err := p.Feedback(Feedback{TaskID: id, Good: true, Objective: true}); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(path)
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("datei: %v %v", st, err)
	}
	want := rating(t, p, "a", "code.python")
	if got := rating(t, Open(path), "a", "code.python"); got.R != want.R || got.Games != want.Games {
		t.Fatalf("nach neustart %+v, vorher %+v", got, want)
	}

	if err := os.WriteFile(path, []byte("{kaputt"), 0o600); err != nil {
		t.Fatal(err)
	}
	if len(Open(path).Snapshot()) != 0 {
		t.Fatal("kaputte datei gelesen")
	}
	if _, err := os.Stat(path + ".bad"); err != nil {
		t.Fatal("kaputte datei nicht beiseitegelegt")
	}
}
