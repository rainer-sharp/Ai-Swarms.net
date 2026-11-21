// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Rainer Sestig. Kommerzielle Lizenz: siehe COMMERCIAL.md.

package bench

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"slices"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/rainer-sharp/Ai-Swarms.net/internal/backend"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/orchestrator"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/protocol"
)

// Plan beschreibt einen Benchmark-Lauf (YAML).
type Plan struct {
	Tasks string `yaml:"tasks"`
	// TasksSHA256 legt den Aufgabensatz vor dem Lauf fest (Commit-Reveal).
	TasksSHA256 string `yaml:"tasks_sha256"`
	Out         string `yaml:"out"`
	MaxTokens   int    `yaml:"max_tokens"`
	Runs        []Run  `yaml:"runs"`
}

// Run ist eine Versuchsbedingung: ein Einzelmodell direkt am Backend
// (mode: direct) oder ein Schwarm-Muster über das Gateway (mode: swarm).
type Run struct {
	Name string `yaml:"name"`
	Mode string `yaml:"mode"` // direct | swarm
	// direct
	URL     string `yaml:"url"`
	Model   string `yaml:"model"`
	APIKey  string `yaml:"api_key"`
	Samples int    `yaml:"samples"` // >1: Self-Consistency
	// swarm
	Gateway string   `yaml:"gateway"`
	Pattern string   `yaml:"pattern"`
	Agents  []string `yaml:"agents"`
	// Kinds begrenzt den Lauf auf Aufgabenarten (leer: alle).
	Kinds []string `yaml:"kinds"`
}

// Result ist eine Zeile in der Ergebnisdatei.
type Result struct {
	Run              string              `json:"run"`
	TaskID           string              `json:"task_id"`
	Kind             string              `json:"kind"`
	Solved           bool                `json:"solved"`
	Samples          int                 `json:"samples"`
	PromptTokens     int                 `json:"prompt_tokens"`
	CompletionTokens int                 `json:"completion_tokens"`
	DurationMs       int64               `json:"duration_ms"`
	Steps            []orchestrator.Step `json:"steps,omitempty"`
	// StepSolved bewertet die Zwischentexte (Steps[i].Output) wie eine
	// Endantwort; nil für Prüfer, fehlgeschlagene Schritte und ohne Text.
	StepSolved []*bool `json:"step_solved,omitempty"`
	// SwarmTask: Auftrags-ID beim Knoten, für die Rückmeldung ans Kompetenzprofil.
	SwarmTask   string    `json:"swarm_task,omitempty"`
	Verdict     string    `json:"verdict,omitempty"`
	Issues      []string  `json:"issues,omitempty"`
	Limitations []string  `json:"limitations,omitempty"`
	Error       string    `json:"error,omitempty"`
	Answer      string    `json:"answer"`
	TasksSHA256 string    `json:"tasks_sha256"`
	At          time.Time `json:"at"`
}

// RunOptions steuern einen Aufruf von Execute.
type RunOptions struct {
	Limit       int      // höchstens so viele Aufgaben je Art (0: alle)
	Only        []string // nur diese Läufe (leer: alle)
	RetryErrors bool     // Aufgaben mit Fehler erneut ausführen
}

// LoadPlan liest und prüft einen Plan.
func LoadPlan(path string) (Plan, error) {
	var p Plan
	data, err := os.ReadFile(path)
	if err != nil {
		return p, err
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&p); err != nil {
		return p, fmt.Errorf("%s: %w", path, err)
	}
	if p.MaxTokens == 0 {
		p.MaxTokens = 4096
	}
	seen := map[string]bool{}
	for i, r := range p.Runs {
		switch {
		case r.Name == "" || seen[r.Name]:
			return p, fmt.Errorf("runs[%d]: name fehlt oder doppelt", i)
		case r.Mode == "direct" && (r.URL == "" || r.Model == ""):
			return p, fmt.Errorf("runs[%d] %s: direct braucht url und model", i, r.Name)
		case r.Mode == "swarm" && (r.Gateway == "" || r.Pattern == ""):
			return p, fmt.Errorf("runs[%d] %s: swarm braucht gateway und pattern", i, r.Name)
		case r.Mode == "swarm" && r.Samples > 1:
			return p, fmt.Errorf("runs[%d] %s: samples nur bei direct", i, r.Name)
		case r.Mode != "direct" && r.Mode != "swarm":
			return p, fmt.Errorf("runs[%d] %s: mode %q unbekannt", i, r.Name, r.Mode)
		}
		seen[r.Name] = true
	}
	return p, nil
}

// Execute arbeitet den Plan strikt nacheinander ab und hängt jedes Ergebnis
// sofort an die Ergebnisdatei an. Bereits vorhandene Ergebnisse werden
// übersprungen, ein abgebrochener Lauf lässt sich also fortsetzen.
func Execute(ctx context.Context, p Plan, opt RunOptions) error {
	tasks, sum, err := LoadTasks(p.Tasks)
	if err != nil {
		return err
	}
	if p.TasksSHA256 == "" || !strings.EqualFold(p.TasksSHA256, sum) {
		return fmt.Errorf("aufgabensatz %s hat sha256 %s, im plan steht %q – lauf verweigert", p.Tasks, sum, p.TasksSHA256)
	}
	tasks = limitTasks(tasks, opt.Limit)
	done, err := doneSet(p.Out, opt.RetryErrors)
	if err != nil {
		return err
	}
	out, err := os.OpenFile(p.Out, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer out.Close()
	enc := json.NewEncoder(out)

	for _, run := range p.Runs {
		if len(opt.Only) > 0 && !slices.Contains(opt.Only, run.Name) {
			continue
		}
		var todo []Task
		for _, t := range tasks {
			if !done[run.Name+"|"+t.ID] && (len(run.Kinds) == 0 || slices.Contains(run.Kinds, t.Kind)) {
				todo = append(todo, t)
			}
		}
		if len(todo) == 0 {
			continue
		}
		slog.Info("bench: lauf", "run", run.Name, "aufgaben", len(todo))
		if run.Mode == "direct" {
			warmUp(ctx, run)
		}
		for i, t := range todo {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			res := runTask(ctx, p, run, t)
			res.TasksSHA256, res.At = sum, time.Now().UTC().Truncate(time.Second)
			if err := enc.Encode(res); err != nil {
				return err
			}
			slog.Info("bench: aufgabe", "run", run.Name, "nr", fmt.Sprintf("%d/%d", i+1, len(todo)), "task", t.ID,
				"gelöst", res.Solved, "tokens", res.PromptTokens+res.CompletionTokens, "ms", res.DurationMs, "err", res.Error)
		}
	}
	return nil
}

func limitTasks(tasks []Task, limit int) []Task {
	if limit <= 0 {
		return tasks
	}
	n := map[string]int{}
	var out []Task
	for _, t := range tasks {
		if n[t.Kind] < limit {
			n[t.Kind]++
			out = append(out, t)
		}
	}
	return out
}

func doneSet(path string, retryErrors bool) (map[string]bool, error) {
	done := map[string]bool{}
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return done, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	for sc.Scan() {
		var r Result
		if json.Unmarshal(sc.Bytes(), &r) == nil && (!retryErrors || r.Error == "") {
			done[r.Run+"|"+r.TaskID] = true
		}
	}
	return done, sc.Err()
}

// warmUp lädt das Modell vorab, damit die erste Aufgabe nicht die Ladezeit misst.
func warmUp(ctx context.Context, run Run) {
	c := backend.New(run.URL, run.APIKey)
	// 256 Tokens: Reasoning-Modelle denken vor der Antwort nach.
	_, err := c.Chat(ctx, backend.ChatRequest{Model: run.Model, MaxTokens: 256,
		Messages: []protocol.ChatMessage{{Role: "user", Content: "Antworte nur mit OK."}}})
	if err != nil {
		slog.Warn("bench: aufwärmen fehlgeschlagen", "run", run.Name, "err", err)
	}
}

func runTask(ctx context.Context, p Plan, run Run, t Task) Result {
	res := Result{Run: run.Name, TaskID: t.ID, Kind: t.Kind, Samples: 1}
	start := time.Now()
	switch {
	case run.Mode == "swarm":
		swarmTask(ctx, p, run, t, &res)
	case run.Samples > 1:
		selfConsistency(ctx, p, run, t, &res)
	default:
		answer, pt, ct, err := directChat(ctx, p, run, t)
		res.PromptTokens, res.CompletionTokens, res.Answer = pt, ct, answer
		if err != nil {
			res.Error = err.Error()
		}
	}
	res.DurationMs = time.Since(start).Milliseconds()
	if res.Answer != "" {
		res.Solved = solved(ctx, t, res.Answer, &res)
	}
	if run.Mode == "swarm" && res.SwarmTask != "" {
		sendFeedback(ctx, run, res)
	}
	return res
}

// sendFeedback meldet die Bewertung als objektives Signal an das
// Kompetenzprofil des Knotens (spec/cooperation.md § 2.2): je bewertetem
// Zwischenschritt einzeln, sonst das Endergebnis. Fehler halten den Lauf nicht auf.
func sendFeedback(ctx context.Context, run Run, res Result) {
	type fb struct {
		TaskID    string `json:"task_id"`
		Good      bool   `json:"good"`
		Objective bool   `json:"objective"`
		Step      *int   `json:"step,omitempty"`
	}
	var all []fb
	for i, ok := range res.StepSolved {
		if ok != nil {
			all = append(all, fb{TaskID: res.SwarmTask, Good: *ok, Objective: true, Step: &i})
		}
	}
	if len(all) == 0 {
		all = append(all, fb{TaskID: res.SwarmTask, Good: res.Solved, Objective: true})
	}
	url := strings.TrimRight(run.Gateway, "/") + "/v1/feedback"
	for _, f := range all {
		body, _ := json.Marshal(f)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			return
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
		if err != nil {
			slog.Warn("bench: rückmeldung ans profil fehlgeschlagen", "task", res.TaskID, "err", err)
			return
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusConflict {
			slog.Warn("bench: rückmeldung ans profil abgelehnt", "task", res.TaskID, "status", resp.Status)
		}
	}
}

func solved(ctx context.Context, t Task, answer string, res *Result) bool {
	if t.Kind == KindMath {
		return MathCorrect(t, answer)
	}
	r, err := RunCode(ctx, t, ExtractCode(t, answer), false)
	if err != nil && res.Error == "" {
		res.Error = err.Error()
	}
	return r == CodePassed
}

func directChat(ctx context.Context, p Plan, run Run, t Task) (string, int, int, error) {
	c := backend.New(run.URL, run.APIKey)
	r, err := c.Chat(ctx, backend.ChatRequest{Model: run.Model, MaxTokens: p.MaxTokens,
		Messages: []protocol.ChatMessage{{Role: "user", Content: t.Prompt}}})
	return r.Content, r.PromptTokens, r.CompletionTokens, err
}

// selfConsistency gibt einem Einzelmodell mehrere Versuche: Mathematik per
// Mehrheitsentscheid, Code mit dem ersten Versuch, der die sichtbaren
// Beispiele besteht (sonst der erste Versuch).
func selfConsistency(ctx context.Context, p Plan, run Run, t Task, res *Result) {
	var answers []string
	res.Samples = 0
	for i := 0; i < run.Samples; i++ {
		a, pt, ct, err := directChat(ctx, p, run, t)
		res.Samples++
		res.PromptTokens += pt
		res.CompletionTokens += ct
		if err != nil {
			res.Error = err.Error()
			continue
		}
		answers = append(answers, a)
		if t.Kind == KindCode {
			if v, _ := RunCode(ctx, t, ExtractCode(t, a), true); v == CodePassed {
				res.Answer, res.Error = a, ""
				return
			}
		}
	}
	if len(answers) == 0 {
		return
	}
	res.Error = ""
	if t.Kind == KindCode {
		res.Answer = answers[0]
		return
	}
	votes := map[float64]int{}
	best, bestN := answers[0], 0
	for _, a := range answers {
		if v, ok := ExtractNumber(a); ok {
			if votes[v]++; votes[v] > bestN {
				best, bestN = a, votes[v]
			}
		}
	}
	res.Answer = best
}

// skillFor legt die Fähigkeit je Aufgabenart fest, statt den Router raten zu lassen.
func skillFor(kind string) string {
	if kind == KindCode {
		return "code.python"
	}
	return "reasoning.math"
}

func swarmTask(ctx context.Context, p Plan, run Run, t Task, res *Result) {
	body, _ := json.Marshal(map[string]any{
		"model":      "swarmainet/" + run.Pattern,
		"max_tokens": p.MaxTokens,
		"messages":   []protocol.ChatMessage{{Role: "user", Content: t.Prompt}},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(run.Gateway, "/")+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		res.Error = err.Error()
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-SwarmAiNet-Privacy", string(protocol.PrivacyTrusted))
	req.Header.Set("X-SwarmAiNet-Skill", skillFor(t.Kind))
	if len(run.Agents) > 0 {
		req.Header.Set("X-SwarmAiNet-Agents", strings.Join(run.Agents, ","))
	}
	req.Header.Set("X-SwarmAiNet-Trace", "1")
	resp, err := (&http.Client{Timeout: 30 * time.Minute}).Do(req)
	if err != nil {
		res.Error = err.Error()
		return
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	var out struct {
		Choices []struct {
			Message protocol.ChatMessage `json:"message"`
		} `json:"choices"`
		Swarm *orchestrator.Answer `json:"swarmainet"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		res.Error = fmt.Sprintf("gateway %s: %v", resp.Status, err)
		return
	}
	if out.Error != nil {
		res.Error = out.Error.Message
		return
	}
	if len(out.Choices) > 0 {
		res.Answer = out.Choices[0].Message.Content
	}
	if a := out.Swarm; a != nil {
		res.Steps, res.Verdict, res.Issues, res.Limitations = a.Steps, a.Verdict, a.Issues, a.Limitations
		res.SwarmTask = a.TaskID
		for _, s := range a.Steps {
			res.PromptTokens += s.PromptTokens
			res.CompletionTokens += s.CompletionTokens
		}
		res.StepSolved = gradeSteps(ctx, t, a.Steps)
	}
}

// gradeSteps bewertet Entwürfe, Teillösungen und Synthesen einzeln, damit der
// Bericht zeigen kann, was Prüfer und Synthese bewirken.
func gradeSteps(ctx context.Context, t Task, steps []orchestrator.Step) []*bool {
	var out []*bool
	graded := false
	for _, s := range steps {
		var v *bool
		if s.Role != protocol.RoleVerifier && s.Status != protocol.StatusFailed && strings.TrimSpace(s.Output) != "" {
			var discard Result
			ok := solved(ctx, t, s.Output, &discard)
			v, graded = &ok, true
		}
		out = append(out, v)
	}
	if !graded {
		return nil
	}
	return out
}
