// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Rainer Sestig und Mitwirkende.

package bench

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/rainer-sharp/Ai-Swarms.net/internal/orchestrator"
)

var addTask = Task{
	ID: "code/add", Kind: KindCode, Prompt: "Complete add.",
	CodePrompt: "from typing import List\n\n\ndef add(a: int, b: int) -> int:\n    \"\"\"\n    >>> add(2, 3)\n    5\n    \"\"\"\n",
	EntryPoint: "add",
	Tests:      "def check(candidate):\n    assert candidate(2, 3) == 5\n    assert candidate(-1, 1) == 0\n",
}

var mathTask = Task{ID: "math/1", Kind: KindMath, Prompt: "2+2?", Answer: "1,200"}

func TestExtractNumber(t *testing.T) {
	for in, want := range map[string]float64{
		"Schritt 1: 3 + 4 = 7\nAnswer: 1,200":      1200,
		"... so the result is **Answer: $18.**":    18,
		"Antwort = -3.5":                           -3.5,
		"Es sind 12 Äpfel und 30 Birnen, also 42.": 42,
	} {
		if got, ok := ExtractNumber(in); !ok || math.Abs(got-want) > 1e-9 {
			t.Errorf("%q: %v %v, erwartet %v", in, got, ok, want)
		}
	}
	if !MathCorrect(mathTask, "Answer: 1200") || MathCorrect(mathTask, "Answer: 120") {
		t.Fatal("mathcorrect")
	}
}

func TestExtractCode(t *testing.T) {
	full := ExtractCode(addTask, "Hier:\n```python\ndef add(a, b):\n    return a + b\n```\nFertig.")
	if !strings.Contains(full, "from typing import List") || strings.Count(full, "def add") != 1 {
		t.Fatalf("vollständige funktion: %q", full)
	}
	body := ExtractCode(addTask, "```python\n    return a + b\n```")
	if !strings.Contains(body, `"""`) || !strings.HasSuffix(strings.TrimSpace(body), "return a + b") {
		t.Fatalf("nur rumpf: %q", body)
	}
}

func needBwrap(t *testing.T) {
	if _, err := exec.LookPath("bwrap"); err != nil {
		t.Skip("bwrap fehlt")
	}
	if err := exec.Command("bwrap", sandboxArgs(t.TempDir(), "true")...).Run(); err != nil {
		t.Skipf("bwrap nicht nutzbar: %v", err)
	}
}

func TestRunCodeSandbox(t *testing.T) {
	needBwrap(t)
	ctx := context.Background()
	cases := []struct {
		answer  string
		visible bool
		want    CodeResult
	}{
		{"```python\ndef add(a, b):\n    return a + b\n```", false, CodePassed},
		{"```python\ndef add(a, b):\n    return a - b\n```", false, CodeFailed},
		{"```python\ndef add(a, b):\n    return 5\n```", true, CodePassed},  // sichtbare Beispiele bestanden …
		{"```python\ndef add(a, b):\n    return 5\n```", false, CodeFailed}, // … verdeckte Tests nicht
		{"```python\ndef add(a, b):\n    while True: pass\n```", false, CodeFailed},
		{"```python\nimport socket\ndef add(a, b):\n    socket.create_connection(('1.1.1.1', 53), timeout=2)\n    return a + b\n```", false, CodeFailed},
	}
	for i, c := range cases {
		got, err := RunCode(ctx, addTask, ExtractCode(addTask, c.answer), c.visible)
		if err != nil || got != c.want {
			t.Errorf("fall %d: %v %v, erwartet %v", i, got, err, c.want)
		}
	}
}

func TestMcNemar(t *testing.T) {
	if p := mcNemar(0, 0); p != 1 {
		t.Fatal(p)
	}
	// 1 gegen 9 diskordante Paare: p = 2 * (1 + 10) / 1024
	if p := mcNemar(1, 9); math.Abs(p-22.0/1024) > 1e-9 {
		t.Fatal(p)
	}
}

func writeTasks(t *testing.T, tasks ...Task) (string, string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "tasks.jsonl")
	f, _ := os.Create(p)
	for _, task := range tasks {
		_ = json.NewEncoder(f).Encode(task)
	}
	f.Close()
	_, sum, err := LoadTasks(p)
	if err != nil {
		t.Fatal(err)
	}
	return p, sum
}

func TestExecuteDirectSwarmAndResume(t *testing.T) {
	var calls atomic.Int32
	backendSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"Answer: 1200"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5}}`))
	}))
	defer backendSrv.Close()
	var agentsHeader, traceHeader string
	var feedback []map[string]any
	gatewaySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/feedback" {
			var fb map[string]any
			_ = json.NewDecoder(r.Body).Decode(&fb)
			feedback = append(feedback, fb)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		agentsHeader, traceHeader = r.Header.Get("X-SwarmAiNet-Agents"), r.Header.Get("X-SwarmAiNet-Trace")
		// Entwurf richtig, Prüfer meldet trotzdem Mängel, Überarbeitung falsch.
		ans := orchestrator.Answer{TaskID: "s1", Verdict: "issues", Issues: []string{"rechenfehler"}, Steps: []orchestrator.Step{
			{Role: "specialist", AgentID: "a", Status: "ok", PromptTokens: 100, CompletionTokens: 20, Output: "Answer: 1200"},
			{Role: "verifier", AgentID: "b", Status: "ok", PromptTokens: 50, CompletionTokens: 10, Output: `{"verdict":"issues"}`},
		}}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices":    []map[string]any{{"message": map[string]string{"role": "assistant", "content": "Answer: 120"}}},
			"swarmainet": ans,
		})
	}))
	defer gatewaySrv.Close()

	tasksPath, sum := writeTasks(t, mathTask)
	out := filepath.Join(t.TempDir(), "results.jsonl")
	p := Plan{Tasks: tasksPath, TasksSHA256: sum, Out: out, MaxTokens: 100, Runs: []Run{
		{Name: "einzel", Mode: "direct", URL: backendSrv.URL, Model: "m"},
		{Name: "sc3", Mode: "direct", URL: backendSrv.URL, Model: "m", Samples: 3},
		{Name: "schwarm", Mode: "swarm", Gateway: gatewaySrv.URL, Pattern: "parallel_verify", Agents: []string{"a", "b"}},
	}}
	if err := Execute(context.Background(), p, RunOptions{}); err != nil {
		t.Fatal(err)
	}
	res, err := LoadResults(out)
	if err != nil || len(res) != 3 {
		t.Fatalf("%v %d", err, len(res))
	}
	byRun := map[string]Result{}
	for _, r := range res {
		byRun[r.Run] = r
	}
	if r := byRun["einzel"]; !r.Solved || r.PromptTokens != 10 || r.CompletionTokens != 5 {
		t.Fatalf("einzel %+v", r)
	}
	if r := byRun["sc3"]; !r.Solved || r.Samples != 3 || r.PromptTokens != 30 {
		t.Fatalf("sc3 %+v", r)
	}
	if r := byRun["schwarm"]; r.Solved || r.PromptTokens != 150 || r.CompletionTokens != 30 || len(r.Steps) != 2 || agentsHeader != "a,b" || traceHeader != "1" {
		t.Fatalf("schwarm %+v, agents %q trace %q", r, agentsHeader, traceHeader)
	}
	if r := byRun["schwarm"]; len(r.StepSolved) != 2 || r.StepSolved[0] == nil || !*r.StepSolved[0] || r.StepSolved[1] != nil || len(r.Issues) != 1 {
		t.Fatalf("zwischenschritte %+v", r)
	}
	// Rückmeldung ans Kompetenzprofil: nur der bewertete Entwurf, objektiv.
	if len(feedback) != 1 || feedback[0]["task_id"] != "s1" || feedback[0]["step"] != 0.0 || feedback[0]["good"] != true || feedback[0]["objective"] != true {
		t.Fatalf("rückmeldungen %v", feedback)
	}
	st := StepAnalysis(res)
	if len(st) != 1 || st[0].Checked != 1 || st[0].FalseAlarm != 1 || st[0].Broken != 1 || st[0].Justified != 0 {
		t.Fatalf("stepanalysis %+v", st)
	}

	// Fortsetzen: nichts mehr zu tun, keine weiteren Backend-Aufrufe.
	before := calls.Load()
	if err := Execute(context.Background(), p, RunOptions{}); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != before {
		t.Fatal("bereits erledigte aufgaben erneut ausgeführt")
	}

	// Falscher Hash: Lauf verweigert.
	p.TasksSHA256 = strings.Repeat("0", 64)
	if err := Execute(context.Background(), p, RunOptions{}); err == nil || !strings.Contains(err.Error(), "verweigert") {
		t.Fatalf("hash nicht geprüft: %v", err)
	}

	var sb strings.Builder
	WriteReport(&sb, res, "")
	if !strings.Contains(sb.String(), "| einzel | 1/1 | 100 % |") || !strings.Contains(sb.String(), "| schwarm | 0/1 | 0 % |") ||
		!strings.Contains(sb.String(), "| schwarm | math | 1 | 0 | 1 | 0 | 0 / 1 | 0 | 0 / 0 |") {
		t.Fatalf("bericht:\n%s", sb.String())
	}
}

func TestLoadPlanValidation(t *testing.T) {
	p := filepath.Join(t.TempDir(), "plan.yaml")
	_ = os.WriteFile(p, []byte("tasks: t.jsonl\nruns:\n  - name: x\n    mode: swarm\n    gateway: http://127.0.0.1:7711\n    pattern: single\n    samples: 3\n"), 0o644)
	if _, err := LoadPlan(p); err == nil || !strings.Contains(err.Error(), "samples nur bei direct") {
		t.Fatalf("%v", err)
	}
	_ = os.WriteFile(p, []byte("tasks: t.jsonl\ntippfehler: 1\n"), 0o644)
	if _, err := LoadPlan(p); err == nil {
		t.Fatal("unbekannter schlüssel nicht erkannt")
	}
}
