// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Rainer Sestig. Kommerzielle Lizenz: siehe COMMERCIAL.md.

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
)

// Task ist eine Aufgabe aus dem Aufgabensatz (JSONL, eine Aufgabe pro Zeile).
type Task struct {
	ID   string `json:"id"`
	Kind string `json:"kind"` // code | math
	// Prompt ist die vollständige Nutzernachricht an das Modell.
	Prompt string `json:"prompt"`
	// Code: Funktionskopf mit Docstring, Name der Funktion, verdeckte Tests.
	CodePrompt string `json:"code_prompt,omitempty"`
	EntryPoint string `json:"entry_point,omitempty"`
	Tests      string `json:"tests,omitempty"`
	// Mathematik: erwartete Zahl.
	Answer string `json:"answer,omitempty"`
}

const (
	KindCode = "code"
	KindMath = "math"
)

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
		case t.Kind != KindCode && t.Kind != KindMath:
			return nil, "", fmt.Errorf("%s:%d: kind %q unbekannt", path, n, t.Kind)
		}
		tasks = append(tasks, t)
	}
	return tasks, hex.EncodeToString(sum[:]), sc.Err()
}
