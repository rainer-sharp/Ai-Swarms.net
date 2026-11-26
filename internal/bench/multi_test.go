// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Rainer Sestig und Mitwirkende.

package bench

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestPartValueAndNumbers(t *testing.T) {
	answer := "Here you go.\n\nAnswer 1: 41\n**Answer 2:** 1,814,400 seconds\n- Answer 3: `Tungsten`\n> Antwort 4: 22,1 l\nAnswer 1: 42"
	for n, want := range map[int]string{1: "42", 2: "1,814,400 seconds", 3: "Tungsten", 4: "22,1 l"} {
		if got, ok := partValue(answer, n); !ok || got != want {
			t.Errorf("teil %d: %q %v, erwartet %q", n, got, ok, want)
		}
	}
	if _, ok := partValue(answer, 5); ok {
		t.Error("teil 5 gefunden")
	}
	for in, want := range map[string]float64{"1,814,400": 1814400, "22,1 l": 22.1, "39.78 €": 39.78, "-3": -3, "1,234.5": 1234.5, "0.3125.": 0.3125} {
		if got, ok := parseNumber(in); !ok || got != want {
			t.Errorf("%q: %v %v, erwartet %v", in, got, ok, want)
		}
	}
	if !numberCorrect(Part{Answer: "28.27"}, "28.27") || numberCorrect(Part{Answer: "28.27"}, "28.274") ||
		!numberCorrect(Part{Answer: "28.27", Tolerance: 0.01}, "28.274") {
		t.Error("toleranz")
	}
}

// TestDraftTaskSet: Der Entwurf lädt, und eine vollständige Antwort auf
// mix/04 wird je Teil richtig bewertet, eine teilweise falsche nicht.
func TestDraftTaskSet(t *testing.T) {
	tasks, _, err := LoadTasks(filepath.Join("..", "..", "bench", "tasks-mix-draft.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 10 {
		t.Fatalf("%d aufgaben", len(tasks))
	}
	i := slices.IndexFunc(tasks, func(x Task) bool { return x.ID == "mix/04" })
	task := tasks[i]
	needBwrap(t)
	good := "The total is 5.4.\n\n```python\ndef total_cost(csv_text: str) -> float:\n    total = 0.0\n" +
		"    for line in csv_text.strip().splitlines()[1:]:\n        _, qty, price = line.split(',')\n        total += int(qty) * float(price)\n" +
		"    return total\n```\n\nAnswer 1: 5.4\nAnswer 3: **melon**\n"
	parts, err := GradeParts(context.Background(), task, good)
	if err != nil || !slices.Equal(parts, []bool{true, true, true}) {
		t.Fatalf("richtige antwort: %v %v", parts, err)
	}
	bad := strings.Replace(strings.Replace(good, "Answer 3: **melon**", "Answer 3: apple", 1), "int(qty) * float(price)", "int(qty)", 1)
	if parts, _ = GradeParts(context.Background(), task, bad); !slices.Equal(parts, []bool{true, false, false}) {
		t.Fatalf("falsche antwort: %v", parts)
	}
	var res Result
	if solved(context.Background(), task, good, &res) != true || len(res.PartKinds) != 3 || res.PartKinds[1] != PartCode {
		t.Fatalf("solved: %+v", res)
	}
}

func TestLoadTasksMultiValidation(t *testing.T) {
	for name, line := range map[string]string{
		"ohne teile":     `{"id":"m","kind":"multi","prompt":"p"}`,
		"doppeltes n":    `{"id":"m","kind":"multi","prompt":"p","parts":[{"n":1,"kind":"number","answer":"1"},{"n":1,"kind":"number","answer":"2"}]}`,
		"code ohne test": `{"id":"m","kind":"multi","prompt":"p","parts":[{"n":1,"kind":"code","entry_point":"f"}]}`,
		"keine zahl":     `{"id":"m","kind":"multi","prompt":"p","parts":[{"n":1,"kind":"number","answer":"viele"}]}`,
		"muster":         `{"id":"m","kind":"multi","prompt":"p","parts":[{"n":1,"kind":"text","pattern":"("}]}`,
		"art":            `{"id":"m","kind":"multi","prompt":"p","parts":[{"n":1,"kind":"bild"}]}`,
	} {
		path := filepath.Join(t.TempDir(), "t.jsonl")
		if err := os.WriteFile(path, []byte(line+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, _, err := LoadTasks(path); err == nil {
			t.Errorf("%s: angenommen", name)
		}
	}
}

// TestPartAnalysis: Teile je Art und Abdeckung gegenüber den Einzelmodellen.
func TestPartAnalysis(t *testing.T) {
	kinds := []string{PartCode, PartNumber, PartText}
	r := func(run, task string, swarm bool, parts ...bool) Result {
		res := Result{Run: run, TaskID: task, Kind: KindMulti, Samples: 1, Parts: parts, PartKinds: kinds}
		res.Solved = !slices.Contains(parts, false)
		if swarm {
			res.SwarmTask = "t-" + task
		}
		return res
	}
	results := []Result{
		r("team", "a", true, true, true, true), r("team", "b", true, true, true, true), r("team", "c", true, false, true, true),
		r("coder", "a", false, true, false, true), r("coder", "b", false, true, true, true), r("coder", "c", false, true, false, false),
		r("general", "a", false, false, true, true), r("general", "b", false, false, true, true), r("general", "c", false, false, true, true),
		{Run: "team", TaskID: "x", Kind: KindCode, Solved: true}, // andere Art: nicht mitgezählt
	}
	got := PartAnalysis(results)
	if len(got) != 3 {
		t.Fatalf("%+v", got)
	}
	team, coder := got[0], got[1]
	if team.Solved != 2 || team.PartsRight != 8 || team.ByKind[PartCode] != [2]int{2, 3} || team.Coverage != 1 {
		t.Fatalf("team: %+v", team) // a löst kein Einzelmodell, b löst coder
	}
	if coder.Coverage != -1 || coder.Solved != 1 {
		t.Fatalf("coder: %+v", coder)
	}
}

// TestAnswerLinesInCode: Zeilen „Answer <n>:“ im Code-Block (so schreiben es
// kleine Modelle oft) machen den Code nicht ungültig; Python-Namen wie
// answer1 bleiben.
func TestAnswerLinesInCode(t *testing.T) {
	answer := "```python\ndef f():\n    answer1 = 5\n    return answer1\n\n# Output\nAnswer 1: [('a', 3)]\n**Answer 2:** 960\n```"
	code := multiCode(answer)
	if strings.Contains(code, "Answer 1") || strings.Contains(code, "960") || !strings.Contains(code, "answer1 = 5") {
		t.Fatalf("code:\n%s", code)
	}
	if v, ok := partValue(answer, 2); !ok || v != "960" {
		t.Fatalf("wert aus dem block: %q %v", v, ok)
	}
}
