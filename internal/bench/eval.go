// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Rainer Sestig und Mitwirkende.

package bench

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

//go:embed harness.py
var harness []byte

// codeTimeout begrenzt einen Testlauf in der Sandbox.
const codeTimeout = 20 * time.Second

var codeBlockRe = regexp.MustCompile("(?s)```(?:python|py)?[ \t]*\n(.*?)```")

// ExtractCode holt den Python-Code aus einer Antwort. Fehlt die Funktion
// selbst (nur Rumpf geliefert), wird der Funktionskopf der Aufgabe ergänzt;
// Importe der Aufgabe werden immer vorangestellt.
func ExtractCode(t Task, answer string) string {
	code := answer
	if m := codeBlockRe.FindAllStringSubmatch(answer, -1); len(m) > 0 {
		// der längste Block ist in der Regel die Lösung
		code = m[0][1]
		for _, b := range m[1:] {
			if len(b[1]) > len(code) {
				code = b[1]
			}
		}
	}
	if !strings.Contains(code, "def "+t.EntryPoint) {
		code = t.CodePrompt + code
	}
	var imports []string
	for _, l := range strings.Split(t.CodePrompt, "\n") {
		if strings.HasPrefix(l, "import ") || strings.HasPrefix(l, "from ") {
			imports = append(imports, l)
		}
	}
	return strings.Join(imports, "\n") + "\n\n" + code
}

// CodeResult: Ergebnis der Sandbox-Prüfung.
type CodeResult int

const (
	CodeFailed CodeResult = iota
	CodePassed
	CodeNoExamples // nur für sichtbare Tests: Aufgabe hat keine Beispiele
)

// RunCode prüft eine Lösung in der Sandbox. visible: nur die Beispiele aus
// dem Docstring (für Self-Consistency), sonst die verdeckten Tests.
func RunCode(ctx context.Context, t Task, code string, visible bool) (CodeResult, error) {
	dir, err := os.MkdirTemp("", "swarmainet-bench-")
	if err != nil {
		return CodeFailed, err
	}
	defer os.RemoveAll(dir)
	tj, _ := json.Marshal(t)
	for name, data := range map[string][]byte{"harness.py": harness, "solution.py": []byte(code), "task.json": tj} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			return CodeFailed, err
		}
	}
	mode := "test"
	if visible {
		mode = "visible"
	}
	ctx, cancel := context.WithTimeout(ctx, codeTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "bwrap", sandboxArgs(dir, "python3", "-I", "harness.py", mode)...)
	err = cmd.Run()
	var ee *exec.ExitError
	switch {
	case err == nil:
		return CodePassed, nil
	case ctx.Err() != nil:
		return CodeFailed, nil // Zeitlimit: als nicht bestanden werten
	case errors.As(err, &ee) && ee.ExitCode() == 3:
		return CodeNoExamples, nil
	case errors.As(err, &ee):
		return CodeFailed, nil
	}
	return CodeFailed, fmt.Errorf("sandbox: %w", err)
}

// sandboxArgs: nur /usr lesbar, eigenes /tmp, kein Netz, keine Umgebung,
// 1 GiB Adressraum und 10 s CPU.
func sandboxArgs(dir string, cmd ...string) []string {
	args := []string{
		"--ro-bind", "/usr", "/usr",
		"--symlink", "usr/lib", "/lib", "--symlink", "usr/lib64", "/lib64", "--symlink", "usr/bin", "/bin",
		"--proc", "/proc", "--dev", "/dev", "--tmpfs", "/tmp",
		"--bind", dir, "/work", "--chdir", "/work",
		"--unshare-all", "--die-with-parent", "--new-session",
		"--clearenv", "--setenv", "PATH", "/usr/bin",
		"prlimit", "--as=1073741824", "--cpu=10",
	}
	return append(args, cmd...)
}

var (
	answerLineRe = regexp.MustCompile(`(?i)(?:answer|antwort)\s*[:=]\s*\**\s*\$?\s*(-?[\d,]*\.?\d+)`)
	numberRe     = regexp.MustCompile(`-?\d[\d,]*\.?\d*`)
)

// ExtractNumber liest das Ergebnis einer Mathe-Antwort: die letzte Zeile
// „Answer: …“, sonst die letzte Zahl im Text.
func ExtractNumber(answer string) (float64, bool) {
	var s string
	if m := answerLineRe.FindAllStringSubmatch(answer, -1); len(m) > 0 {
		s = m[len(m)-1][1]
	} else if m := numberRe.FindAllString(answer, -1); len(m) > 0 {
		s = m[len(m)-1]
	} else {
		return 0, false
	}
	s = strings.TrimRight(strings.ReplaceAll(s, ",", ""), ".")
	v, err := strconv.ParseFloat(s, 64)
	return v, err == nil
}

// MathCorrect vergleicht mit der erwarteten Zahl.
func MathCorrect(t Task, answer string) bool {
	want, err := strconv.ParseFloat(strings.ReplaceAll(t.Answer, ",", ""), 64)
	if err != nil {
		return false
	}
	got, ok := ExtractNumber(answer)
	return ok && math.Abs(got-want) < 1e-6
}
