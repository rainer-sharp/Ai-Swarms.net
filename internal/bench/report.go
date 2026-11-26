// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Rainer Sestig und Mitwirkende.

package bench

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"math"
	"math/rand/v2"
	"os"
	"slices"
	"sort"
	"strings"

	"github.com/rainer-sharp/Ai-Swarms.net/internal/orchestrator"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/protocol"
)

// LoadResults liest eine Ergebnisdatei. Bei mehrfachen Zeilen für dieselbe
// Aufgabe in demselben Lauf (Wiederholung nach Fehler) gilt die letzte.
func LoadResults(path string) ([]Result, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	idx := map[string]int{}
	var out []Result
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	for n := 1; sc.Scan(); n++ {
		var r Result
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			return nil, fmt.Errorf("%s:%d: %w", path, n, err)
		}
		k := r.Run + "|" + r.TaskID
		if i, ok := idx[k]; ok {
			out[i] = r
			continue
		}
		idx[k] = len(out)
		out = append(out, r)
	}
	return out, sc.Err()
}

// Summary sind die Kennzahlen eines Laufs für eine Aufgabenart.
type Summary struct {
	Run, Kind      string
	N, Solved      int
	Errors         int
	Tokens         int
	MedianMs       int64
	TokensPerSolve float64    // Tokens / gelöste Aufgabe
	TokensCI       [2]float64 // 95-%-Bootstrap-Intervall
	// Vergleich mit dem Referenzlauf (gepaart, gleiche Aufgaben)
	Ref       string
	OnlyRef   int     // nur Referenz gelöst
	OnlyThis  int     // nur dieser Lauf gelöst
	PValue    float64 // McNemar, exakt, zweiseitig
	DiffCI    [2]float64
	DiffPoint float64
}

type byTask map[string]Result

// Summarize berechnet je Lauf und Aufgabenart die Kennzahlen. ref: Name des
// Referenzlaufs; leer = bestes Einzelmodell ohne Self-Consistency je Art.
func Summarize(results []Result, ref string) []Summary {
	runs := map[string]map[string]byTask{} // kind -> run -> task
	var order []string
	for _, r := range results {
		if runs[r.Kind] == nil {
			runs[r.Kind] = map[string]byTask{}
		}
		if runs[r.Kind][r.Run] == nil {
			runs[r.Kind][r.Run] = byTask{}
			if !slices.Contains(order, r.Run) {
				order = append(order, r.Run)
			}
		}
		runs[r.Kind][r.Run][r.TaskID] = r
	}
	kinds := make([]string, 0, len(runs))
	for k := range runs {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)

	var out []Summary
	for _, kind := range kinds {
		refRun := ref
		if refRun == "" {
			refRun = bestSingle(runs[kind])
		}
		for _, name := range order {
			tasks, ok := runs[kind][name]
			if !ok {
				continue
			}
			s := summarize(name, kind, tasks)
			if rt, ok := runs[kind][refRun]; ok && name != refRun {
				s.Ref = refRun
				compare(&s, rt, tasks)
			}
			out = append(out, s)
		}
	}
	return out
}

// bestSingle: Einzelmodell (eine Stichprobe, ohne Schwarm) mit der höchsten Quote.
func bestSingle(runs map[string]byTask) string {
	best, rate := "", -1.0
	names := make([]string, 0, len(runs))
	for n := range runs {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		tasks := runs[n]
		solved, single := 0, true
		for _, r := range tasks {
			if r.Solved {
				solved++
			}
			if r.Samples > 1 || len(r.Steps) > 0 {
				single = false
			}
		}
		if q := float64(solved) / float64(len(tasks)); single && q > rate {
			best, rate = n, q
		}
	}
	return best
}

func summarize(name, kind string, tasks byTask) Summary {
	s := Summary{Run: name, Kind: kind, N: len(tasks)}
	var ms []int64
	var tok, sol []float64
	// sortiert, damit das Bootstrap-Intervall bei festem Seed reproduzierbar ist
	for _, id := range slices.Sorted(maps.Keys(tasks)) {
		r := tasks[id]
		t := r.PromptTokens + r.CompletionTokens
		s.Tokens += t
		tok = append(tok, float64(t))
		if r.Solved {
			s.Solved++
			sol = append(sol, 1)
		} else {
			sol = append(sol, 0)
		}
		if r.Error != "" {
			s.Errors++
		}
		ms = append(ms, r.DurationMs)
	}
	slices.Sort(ms)
	if len(ms) > 0 {
		s.MedianMs = ms[len(ms)/2]
	}
	s.TokensPerSolve = ratio(tok, sol)
	s.TokensCI = bootstrap(len(tok), func(idx []int) float64 {
		var a, b []float64
		for _, i := range idx {
			a, b = append(a, tok[i]), append(b, sol[i])
		}
		return ratio(a, b)
	})
	return s
}

func ratio(tok, sol []float64) float64 {
	var t, s float64
	for i := range tok {
		t, s = t+tok[i], s+sol[i]
	}
	if s == 0 {
		return math.Inf(1)
	}
	return t / s
}

// compare: gepaarter Vergleich auf den gemeinsamen Aufgaben.
func compare(s *Summary, ref, this byTask) {
	var d []float64 // je Aufgabe: dieser - Referenz (−1, 0, 1)
	for _, id := range slices.Sorted(maps.Keys(this)) {
		r := this[id]
		rr, ok := ref[id]
		if !ok {
			continue
		}
		switch {
		case r.Solved && !rr.Solved:
			s.OnlyThis++
			d = append(d, 1)
		case !r.Solved && rr.Solved:
			s.OnlyRef++
			d = append(d, -1)
		default:
			d = append(d, 0)
		}
	}
	s.PValue = mcNemar(s.OnlyRef, s.OnlyThis)
	mean := func(idx []int) float64 {
		var sum float64
		for _, i := range idx {
			sum += d[i]
		}
		return sum / float64(len(idx))
	}
	all := make([]int, len(d))
	for i := range all {
		all[i] = i
	}
	if len(d) > 0 {
		s.DiffPoint = mean(all)
	}
	s.DiffCI = bootstrap(len(d), mean)
}

// mcNemar: exakter zweiseitiger Test auf den diskordanten Paaren.
func mcNemar(b, c int) float64 {
	n := b + c
	if n == 0 {
		return 1
	}
	k := min(b, c)
	var p float64
	for i := 0; i <= k; i++ {
		p += math.Exp(lnChoose(n, i) - float64(n)*math.Ln2)
	}
	return math.Min(1, 2*p)
}

func lnChoose(n, k int) float64 {
	a, _ := math.Lgamma(float64(n + 1))
	b, _ := math.Lgamma(float64(k + 1))
	c, _ := math.Lgamma(float64(n - k + 1))
	return a - b - c
}

// bootstrap liefert das 95-%-Perzentilintervall (2000 Ziehungen, fester Seed).
func bootstrap(n int, stat func(idx []int) float64) [2]float64 {
	if n == 0 {
		return [2]float64{math.NaN(), math.NaN()}
	}
	rng := rand.New(rand.NewPCG(1, 2))
	vals := make([]float64, 0, 2000)
	idx := make([]int, n)
	for range 2000 {
		for i := range idx {
			idx[i] = rng.IntN(n)
		}
		vals = append(vals, stat(idx))
	}
	sort.Float64s(vals)
	return [2]float64{vals[49], vals[1949]}
}

// FailureOverlap: für je zwei Einzelmodelle (eine Stichprobe, ohne Schwarm)
// der Anteil gemeinsamer Fehlschläge an allen Aufgaben, die mindestens eines
// der beiden nicht gelöst hat (1 = immer dieselben Fehler, 0 = nie).
func FailureOverlap(results []Result, kind string) (names []string, m [][]float64) {
	runs := map[string]byTask{}
	multi := map[string]bool{}
	for _, r := range results {
		if r.Samples > 1 || len(r.Steps) > 0 {
			multi[r.Run] = true
		}
	}
	for _, r := range results {
		if r.Kind != kind || multi[r.Run] {
			continue
		}
		if runs[r.Run] == nil {
			runs[r.Run] = byTask{}
			names = append(names, r.Run)
		}
		runs[r.Run][r.TaskID] = r
	}
	m = make([][]float64, len(names))
	for i, a := range names {
		m[i] = make([]float64, len(names))
		for j, b := range names {
			both, either := 0, 0
			for id, ra := range runs[a] {
				rb, ok := runs[b][id]
				if !ok {
					continue
				}
				if !ra.Solved || !rb.Solved {
					either++
				}
				if !ra.Solved && !rb.Solved {
					both++
				}
			}
			m[i][j] = math.NaN()
			if either > 0 {
				m[i][j] = float64(both) / float64(either)
			}
		}
	}
	return names, m
}

// WriteReport gibt die Auswertung als Markdown aus.
func WriteReport(w io.Writer, results []Result, ref string) {
	sum := Summarize(results, ref)
	kind := ""
	for _, s := range sum {
		if s.Kind != kind {
			kind = s.Kind
			fmt.Fprintf(w, "\n## %s\n\n", kind)
			fmt.Fprintln(w, "| Lauf | gelöst | Quote | Fehler | Tokens/gelöst (95 %) | Median s | Δ zur Referenz (95 %) | nur Ref / nur Lauf | p (McNemar) |")
			fmt.Fprintln(w, "|---|---|---|---|---|---|---|---|---|")
		}
		cmp := "Referenz"
		if s.Ref != "" {
			cmp = fmt.Sprintf("%+.1f pp [%+.1f; %+.1f]", 100*s.DiffPoint, 100*s.DiffCI[0], 100*s.DiffCI[1])
		}
		pair, p := "–", "–"
		if s.Ref != "" {
			pair, p = fmt.Sprintf("%d / %d", s.OnlyRef, s.OnlyThis), fmt.Sprintf("%.3f", s.PValue)
		}
		fmt.Fprintf(w, "| %s | %d/%d | %.0f %% | %d | %s [%s; %s] | %.1f | %s | %s | %s |\n",
			s.Run, s.Solved, s.N, 100*float64(s.Solved)/float64(s.N), s.Errors,
			num(s.TokensPerSolve), num(s.TokensCI[0]), num(s.TokensCI[1]), float64(s.MedianMs)/1000, cmp, pair, p)
	}
	kinds := map[string]bool{}
	for _, s := range sum {
		kinds[s.Kind] = true
	}
	for _, k := range slices.Sorted(maps.Keys(kinds)) {
		names, m := FailureOverlap(results, k)
		if len(names) < 2 {
			continue
		}
		fmt.Fprintf(w, "\n### Fehlerüberlappung der Einzelmodelle, %s (gemeinsame Fehlschläge / Fehlschläge von mindestens einem)\n\n", k)
		fmt.Fprintln(w, "| | "+strings.Join(names, " | ")+" |")
		fmt.Fprintln(w, "|---"+strings.Repeat("|---", len(names))+"|")
		for i, n := range names {
			cells := make([]string, len(names))
			for j := range names {
				cells[j] = "–"
				if i != j && !math.IsNaN(m[i][j]) {
					cells[j] = fmt.Sprintf("%.2f", m[i][j])
				}
			}
			fmt.Fprintf(w, "| %s | %s |\n", n, strings.Join(cells, " | "))
		}
	}
	if ps := PartAnalysis(results); len(ps) > 0 {
		fmt.Fprintln(w, "\n### Mehrteilige Aufgaben: Teile und Abdeckung")
		fmt.Fprintln(w, "\nTeile richtig je Art. Abdeckung: Aufgaben, die dieser Lauf löst und kein Einzelmodell (direkt, eine Stichprobe) vollständig.")
		fmt.Fprintln(w, "\n| Lauf | Aufgaben gelöst | Teile richtig | Code | Zahl | Text | Abdeckung |")
		fmt.Fprintln(w, "|---|---|---|---|---|---|---|")
		for _, s := range ps {
			cell := func(k string) string {
				if s.ByKind[k][1] == 0 {
					return "–"
				}
				return fmt.Sprintf("%d/%d", s.ByKind[k][0], s.ByKind[k][1])
			}
			cov := "–"
			if s.Coverage >= 0 {
				cov = fmt.Sprintf("%d", s.Coverage)
			}
			fmt.Fprintf(w, "| %s | %d/%d | %d/%d | %s | %s | %s | %s |\n", s.Run, s.Solved, s.N, s.PartsRight, s.Parts,
				cell(PartCode), cell(PartNumber), cell(PartText), cov)
		}
	}
	if st := StepAnalysis(results); len(st) > 0 {
		fmt.Fprintln(w, "\n### Zwischenschritte (Prüfer und Synthese)")
		fmt.Fprintln(w, "\nPrüfer: Urteil über den geprüften Text gegen dessen tatsächliche Richtigkeit. Überarbeitung: nach „issues“ aus falsch richtig gemacht bzw. aus richtig falsch. Synthese: Zusammenführung mehrerer Teillösungen.")
		fmt.Fprintln(w, "\n| Lauf | Art | geprüft | berechtigt | Fehlalarm | übersehen | repariert / verschlechtert | Synthesen | richtige Teillösung verloren / gerettet |")
		fmt.Fprintln(w, "|---|---|---|---|---|---|---|---|---|")
		slices.SortStableFunc(st, func(a, b StepStats) int { return strings.Compare(a.Kind, b.Kind) })
		for _, s := range st {
			fmt.Fprintf(w, "| %s | %s | %d | %d | %d | %d | %d / %d | %d | %d / %d |\n",
				s.Run, s.Kind, s.Checked, s.Justified, s.FalseAlarm, s.Missed, s.Repaired, s.Broken, s.Synth, s.Lost, s.Rescued)
		}
	}
}

func num(f float64) string {
	if math.IsInf(f, 0) || math.IsNaN(f) {
		return "–"
	}
	return fmt.Sprintf("%.0f", f)
}

// StepStats wertet die Zwischenschritte eines Laufs für eine Aufgabenart aus
// (nur Ergebnisse mit StepSolved, also Schwarm-Läufe mit Trace).
type StepStats struct {
	Run, Kind string
	N         int // Aufgaben mit bewerteten Zwischenschritten
	// Prüfer: Urteil über den geprüften Text (letzter bewerteter Schritt vor
	// dem ersten Prüfer) gegen dessen tatsächliche Richtigkeit.
	Checked                       int
	Justified, FalseAlarm, Missed int // issues+falsch, issues+richtig, ok+falsch
	// Überarbeitung nach issues: geprüfter Text falsch → Endantwort richtig und umgekehrt.
	Repaired, Broken int
	// Synthese der Teillösungen (erste Synthese nach den Spezialisten).
	Synth         int
	Lost, Rescued int // mind. eine Teillösung richtig → Synthese falsch; keine richtig → Synthese richtig
}

func StepAnalysis(results []Result) []StepStats {
	idx := map[string]int{}
	var out []StepStats
	for _, r := range results {
		if len(r.StepSolved) != len(r.Steps) || len(r.Steps) == 0 {
			continue
		}
		k := r.Run + "|" + r.Kind
		i, ok := idx[k]
		if !ok {
			i = len(out)
			idx[k] = i
			out = append(out, StepStats{Run: r.Run, Kind: r.Kind})
		}
		s := &out[i]
		s.N++
		vi := slices.IndexFunc(r.Steps, func(st orchestrator.Step) bool { return st.Role == protocol.RoleVerifier })
		if vi > 0 && (r.Verdict == "ok" || r.Verdict == "issues") {
			cand := -1
			for j := vi - 1; j >= 0 && cand < 0; j-- {
				if r.StepSolved[j] != nil {
					cand = j
				}
			}
			if cand >= 0 {
				right := *r.StepSolved[cand]
				s.Checked++
				switch {
				case r.Verdict == "issues" && !right:
					s.Justified++
				case r.Verdict == "issues":
					s.FalseAlarm++
				case !right:
					s.Missed++
				}
				if r.Verdict == "issues" && !right && r.Solved {
					s.Repaired++
				}
				if r.Verdict == "issues" && right && !r.Solved {
					s.Broken++
				}
			}
		}
		si := slices.IndexFunc(r.Steps, func(st orchestrator.Step) bool { return st.Role == protocol.RoleSynthesizer })
		if si > 0 && r.StepSolved[si] != nil && (vi < 0 || si < vi) {
			anyRight, parts := false, 0
			for j := 0; j < si; j++ {
				if r.Steps[j].Role == protocol.RoleSpecialist && r.StepSolved[j] != nil {
					parts++
					anyRight = anyRight || *r.StepSolved[j]
				}
			}
			if parts > 1 {
				s.Synth++
				if anyRight && !*r.StepSolved[si] {
					s.Lost++
				}
				if !anyRight && *r.StepSolved[si] {
					s.Rescued++
				}
			}
		}
	}
	return out
}

// PartStats: mehrteilige Aufgaben eines Laufs (Hypothese 2 im Messplan).
type PartStats struct {
	Run               string
	N, Solved         int
	Parts, PartsRight int
	ByKind            map[string][2]int // Art → richtig, gesamt
	// Coverage: Aufgaben, die dieser Lauf löst und kein Einzelmodell
	// (direkt, eine Stichprobe) vollständig; −1 für Einzelmodelle selbst.
	Coverage int
}

// single: Ergebnisse eines Einzelmodells (eine Stichprobe, ohne Schwarm).
func single(r Result) bool { return r.Samples <= 1 && len(r.Steps) == 0 && r.SwarmTask == "" }

func PartAnalysis(results []Result) []PartStats {
	singles := map[string]bool{}  // Läufe, die als Einzelmodell gelten
	bySingle := map[string]bool{} // Aufgaben, die ein Einzelmodell löst
	notSingle := map[string]bool{}
	for _, r := range results {
		if r.Kind != KindMulti {
			continue
		}
		if single(r) {
			singles[r.Run] = true
		} else {
			notSingle[r.Run] = true
		}
	}
	for run := range notSingle {
		delete(singles, run) // ein Lauf mit Schwarm-Ergebnissen ist kein Einzelmodell
	}
	for _, r := range results {
		if r.Kind == KindMulti && singles[r.Run] && r.Solved {
			bySingle[r.TaskID] = true
		}
	}
	idx := map[string]int{}
	var out []PartStats
	for _, r := range results {
		if r.Kind != KindMulti {
			continue
		}
		i, ok := idx[r.Run]
		if !ok {
			i = len(out)
			idx[r.Run] = i
			cov := 0
			if singles[r.Run] {
				cov = -1
			}
			out = append(out, PartStats{Run: r.Run, ByKind: map[string][2]int{}, Coverage: cov})
		}
		s := &out[i]
		s.N++
		if r.Solved {
			s.Solved++
			if s.Coverage >= 0 && !bySingle[r.TaskID] {
				s.Coverage++
			}
		}
		for j, ok := range r.Parts {
			k := ""
			if j < len(r.PartKinds) {
				k = r.PartKinds[j]
			}
			c := s.ByKind[k]
			c[1]++
			s.Parts++
			if ok {
				c[0]++
				s.PartsRight++
			}
			s.ByKind[k] = c
		}
	}
	return out
}
