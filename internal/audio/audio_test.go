// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Rainer Sestig und Mitwirkende.

package audio

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/rainer-sharp/Ai-Swarms.net/internal/protocol"
)

func TestPlan(t *testing.T) {
	m := time.Minute
	if p := Plan(5*m, 10*m, m); len(p) != 1 || p[0] != [2]time.Duration{0, 5 * m} {
		t.Fatalf("kurz: %v", p)
	}
	// 25 min: 0–10, 9–19, 18–25
	p := Plan(25*m, 10*m, m)
	want := [][2]time.Duration{{0, 10 * m}, {9 * m, 10 * m}, {18 * m, 7 * m}}
	if len(p) != len(want) {
		t.Fatalf("%v", p)
	}
	for i := range want {
		if p[i] != want[i] {
			t.Fatalf("%v, erwartet %v", p, want)
		}
	}
}

// TestMerge: Die doppelt erkannte Überlappung erscheint einmal; Abweichungen
// in Groß- und Kleinschreibung und Satzzeichen stören nicht; ohne Fundstelle
// wird der geschätzte Anteil weggelassen.
func TestMerge(t *testing.T) {
	a := "Es war einmal ein Haus am See. Das Fenster war geschlossen und der Haken hing am Kreuz"
	b := "fenster war geschlossen, und der Haken hing am Kreuz. Dann kam der Kommissar herein."
	got := Merge([]string{a, b}, []float64{0.6})
	want := "Es war einmal ein Haus am See. Das Fenster war geschlossen und der Haken hing am Kreuz. Dann kam der Kommissar herein."
	if got != want {
		t.Fatalf("\n%q\n%q", got, want)
	}
	// Keine gemeinsame Folge: 2 von 4 Wörtern (Anteil 0,5) fallen beim zweiten weg.
	if got := Merge([]string{"eins zwei", "drei vier fünf sechs"}, []float64{0.5}); got != "eins zwei fünf sechs" {
		t.Fatalf("%q", got)
	}
	if got := Merge([]string{"nur ein stück"}, nil); got != "nur ein stück" {
		t.Fatalf("%q", got)
	}
}

// TestSplit erzeugt mit ffmpeg eine Aufnahme von 25 min und prüft die Stücke.
func TestSplit(t *testing.T) {
	if !Available() {
		t.Skip("ffmpeg fehlt")
	}
	path := filepath.Join(t.TempDir(), "ton.mp3")
	if out, err := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "sine=frequency=440:duration=1500",
		"-ac", "2", "-b:a", "64k", path).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	chunks, total, err := Split(context.Background(), path, ChunkLength, Overlap)
	if err != nil {
		t.Fatal(err)
	}
	if total < 1499*time.Second || total > 1501*time.Second || len(chunks) != 3 {
		t.Fatalf("länge %v, %d stücke", total, len(chunks))
	}
	for i, c := range chunks {
		if c.Index != i || len(c.Data) == 0 || len(c.Data) > protocol.MaxAudioBytes {
			t.Fatalf("stück %d: %d bytes", i, len(c.Data))
		}
	}
	if chunks[1].Start != 9*time.Minute || chunks[2].Len != 7*time.Minute {
		t.Fatalf("%v %v", chunks[1].Start, chunks[2].Len)
	}
}
