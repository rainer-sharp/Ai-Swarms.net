// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Rainer Sestig und Mitwirkende.

package bench

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// Mehrteilige Aufgaben (kind multi, spec/benchmark.md „Mixed task set“): Die
// Antwort enthält allen Code in Python-Blöcken und je Teil, der kein Code
// ist, eine Zeile „Answer <n>: <Wert>“. Jeder Teil wird für sich bewertet.

// partValue liest den Wert der letzten Zeile „Answer <n>: …“ (auch fett,
// als Liste oder Zitat formatiert); ok=false, wenn sie fehlt.
func partValue(answer string, n int) (string, bool) {
	re := regexp.MustCompile(fmt.Sprintf(`(?im)^[\s>*_\-]*(?:answer|antwort)\s*%d\s*\**\s*[:=]\s*(.*?)\s*$`, n))
	m := re.FindAllStringSubmatch(answer, -1)
	if len(m) == 0 {
		return "", false
	}
	v := strings.Trim(m[len(m)-1][1], "*_` ")
	return v, true
}

var firstNumberRe = regexp.MustCompile(`-?\d[\d,.]*`)

// parseNumber liest die erste Zahl eines Werts. Kommas gelten als
// Tausendertrennung, wenn sie Dreiergruppen trennen oder ein Punkt folgt,
// sonst als Dezimalkomma („22,1“).
func parseNumber(s string) (float64, bool) {
	m := firstNumberRe.FindString(s)
	if m == "" {
		return 0, false
	}
	m = strings.TrimRight(m, ".,")
	switch {
	case strings.Contains(m, ",") && strings.Contains(m, "."):
		m = strings.ReplaceAll(m, ",", "")
	case regexp.MustCompile(`^-?\d{1,3}(,\d{3})+$`).MatchString(m):
		m = strings.ReplaceAll(m, ",", "")
	default:
		m = strings.ReplaceAll(m, ",", ".")
	}
	v, err := strconv.ParseFloat(m, 64)
	return v, err == nil
}

// numberCorrect: relative Abweichung höchstens 1e-6, mit Tolerance absolut.
func numberCorrect(p Part, value string) bool {
	want, err := strconv.ParseFloat(p.Answer, 64)
	if err != nil {
		return false
	}
	got, ok := parseNumber(value)
	if !ok {
		return false
	}
	tol := p.Tolerance
	if tol == 0 {
		tol = 1e-6 * math.Max(1, math.Abs(want))
	}
	return math.Abs(got-want) <= tol
}

// answerLineInCodeRe: Zeilen „Answer <n>: …“, die kleine Modelle oft in den
// Code-Block schreiben; sie sind kein Python und gehören nicht zum Code.
var answerLineInCodeRe = regexp.MustCompile(`(?im)^[ \t#>*_\-]*(?:answer|antwort)[ \t]+\d+[ \t]*\**[ \t]*:.*$`)

// multiCode: alle Python-Blöcke der Antwort hintereinander, ohne Zeilen
// „Answer <n>: …“; ohne Block die ganze Antwort.
func multiCode(answer string) string {
	m := codeBlockRe.FindAllStringSubmatch(answer, -1)
	if len(m) == 0 {
		return answerLineInCodeRe.ReplaceAllString(answer, "")
	}
	blocks := make([]string, len(m))
	for i, b := range m {
		blocks[i] = answerLineInCodeRe.ReplaceAllString(b[1], "")
	}
	return strings.Join(blocks, "\n\n")
}

// GradeParts bewertet jeden Teil einer mehrteiligen Aufgabe.
func GradeParts(ctx context.Context, t Task, answer string) ([]bool, error) {
	out := make([]bool, len(t.Parts))
	code := multiCode(answer)
	var firstErr error
	for i, p := range t.Parts {
		switch p.Kind {
		case PartCode:
			r, err := RunCode(ctx, Task{ID: t.ID, Kind: KindCode, EntryPoint: p.EntryPoint, Tests: p.Tests}, code, false)
			if err != nil && firstErr == nil {
				firstErr = err
			}
			out[i] = r == CodePassed
		case PartNumber:
			v, ok := partValue(answer, p.N)
			out[i] = ok && numberCorrect(p, v)
		case PartText:
			v, ok := partValue(answer, p.N)
			out[i] = ok && regexp.MustCompile(p.Pattern).MatchString(v)
		}
	}
	return out, firstErr
}

// partKinds: Art jedes Teils, für den Bericht.
func partKinds(t Task) []string {
	out := make([]string, len(t.Parts))
	for i, p := range t.Parts {
		out[i] = p.Kind
	}
	return out
}
