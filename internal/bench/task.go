// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Rainer Sestig und Mitwirkende.

// Package bench führt den Benchmark aus spec/benchmark.md aus: Einzelmodelle
// direkt am Backend gegen Schwarm-Muster über das Gateway, mit automatischer
// Bewertung (Code: Unit-Tests in einer Sandbox, Mathematik: exakte Zahl).
package bench

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strconv"
)

// Task ist eine Aufgabe aus dem Aufgabensatz (JSONL, eine Aufgabe pro Zeile).
type Task struct {
	ID   string `json:"id"`
	Kind string `json:"kind"` // code | math | multi
	// Prompt ist die vollständige Nutzernachricht an das Modell.
	Prompt string `json:"prompt"`
	// Code: Funktionskopf mit Docstring, Name der Funktion, verdeckte Tests.
	CodePrompt string `json:"code_prompt,omitempty"`
	EntryPoint string `json:"entry_point,omitempty"`
	Tests      string `json:"tests,omitempty"`
	// Mathematik: erwartete Zahl.
	Answer string `json:"answer,omitempty"`
	// Parts: Teile einer mehrteiligen Aufgabe (kind multi, spec/benchmark.md
	// „Mixed task set“); gelöst, wenn alle Teile stimmen.
	Parts []Part `json:"parts,omitempty"`
}

// Part ist ein Teil einer mehrteiligen Aufgabe. Code-Teile werden mit ihren
// Tests geprüft, Zahlen- und Textteile über die Zeile „Answer <n>: …“.
type Part struct {
	N          int     `json:"n"`
	Kind       string  `json:"kind"` // code | number | text
	EntryPoint string  `json:"entry_point,omitempty"`
	Tests      string  `json:"tests,omitempty"`
	Answer     string  `json:"answer,omitempty"`
	Tolerance  float64 `json:"tolerance,omitempty"` // absolut; 0: relativ 1e-6
	Pattern    string  `json:"pattern,omitempty"`   // regulärer Ausdruck für Textteile
}

const (
	KindCode  = "code"
	KindMath  = "math"
	KindMulti = "multi"

	PartCode   = "code"
	PartNumber = "number"
	PartText   = "text"
)

// checkParts prüft die Teile einer mehrteiligen Aufgabe.
func checkParts(parts []Part) error {
	if len(parts) == 0 {
		return fmt.Errorf("mehrteilige aufgabe ohne parts")
	}
	seen := map[int]bool{}
	for _, p := range parts {
		if p.N <= 0 || seen[p.N] {
			return fmt.Errorf("teil %d: n fehlt oder doppelt", p.N)
		}
		seen[p.N] = true
		switch p.Kind {
		case PartCode:
			if p.EntryPoint == "" || p.Tests == "" {
				return fmt.Errorf("teil %d: code ohne entry_point oder tests", p.N)
			}
		case PartNumber:
			if _, err := strconv.ParseFloat(p.Answer, 64); err != nil {
				return fmt.Errorf("teil %d: answer %q ist keine zahl", p.N, p.Answer)
			}
		case PartText:
			if _, err := regexp.Compile(p.Pattern); err != nil || p.Pattern == "" {
				return fmt.Errorf("teil %d: pattern fehlt oder ungültig: %v", p.N, err)
			}
		default:
			return fmt.Errorf("teil %d: kind %q unbekannt", p.N, p.Kind)
		}
	}
	return nil
}

// LoadTasks liest den Aufgabensatz und liefert ihn mit seinem SHA-256.
func LoadTasks(path string) ([]Task, string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256(data)
	f, err := os.Open(path)
	if err != nil {
		return nil, "", err
	}
	defer f.Close()
	var tasks []Task
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	for n := 1; sc.Scan(); n++ {
		if len(sc.Bytes()) == 0 {
			continue
		}
		var t Task
		if err := json.Unmarshal(sc.Bytes(), &t); err != nil {
			return nil, "", fmt.Errorf("%s:%d: %w", path, n, err)
		}
		switch {
		case t.ID == "" || t.Prompt == "":
			return nil, "", fmt.Errorf("%s:%d: id oder prompt fehlt", path, n)
		case t.Kind == KindCode && (t.EntryPoint == "" || t.Tests == ""):
			return nil, "", fmt.Errorf("%s:%d: code-aufgabe ohne entry_point oder tests", path, n)
		case t.Kind == KindMath && t.Answer == "":
			return nil, "", fmt.Errorf("%s:%d: mathe-aufgabe ohne answer", path, n)
		case t.Kind == KindMulti:
			if err := checkParts(t.Parts); err != nil {
				return nil, "", fmt.Errorf("%s:%d: %w", path, n, err)
			}
		case t.Kind != KindCode && t.Kind != KindMath:
			return nil, "", fmt.Errorf("%s:%d: kind %q unbekannt", path, n, t.Kind)
		}
		tasks = append(tasks, t)
	}
	return tasks, hex.EncodeToString(sum[:]), sc.Err()
}
