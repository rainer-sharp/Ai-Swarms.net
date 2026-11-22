// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Rainer Sestig. Kommerzielle Lizenz: siehe COMMERCIAL.md.

// Package audio teilt lange Aufnahmen in überlappende Stücke, die je in einen
// Teilauftrag passen (protocol.MaxAudioBytes), und fügt die Abschriften wieder
// zusammen. Zum Teilen braucht es ffmpeg und ffprobe.
package audio

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"os/exec"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/rainer-sharp/Ai-Swarms.net/internal/protocol"
)

// Vorgaben: 10 min Stücke mit 1 min Überlappung. Als Mono-MP3 mit 32 kbit/s
// sind das gut 2,3 MB, unter protocol.MaxAudioBytes.
const (
	ChunkLength = 10 * time.Minute
	Overlap     = time.Minute
	bitrate     = "32k"
)

// ErrNoFFmpeg: Zum Teilen fehlen ffmpeg oder ffprobe.
var ErrNoFFmpeg = errors.New("zum teilen langer aufnahmen werden ffmpeg und ffprobe gebraucht")

// ErrUnreadable: ffprobe erkennt die Datei nicht als Aufnahme.
var ErrUnreadable = errors.New("aufnahme nicht lesbar")

// Chunk ist ein Stück einer Aufnahme.
type Chunk struct {
	Index int
	Start time.Duration
	Len   time.Duration
	Data  []byte // Mono-MP3, 16 kHz
}

// Available: ffmpeg und ffprobe sind installiert.
func Available() bool {
	_, e1 := exec.LookPath("ffmpeg")
	_, e2 := exec.LookPath("ffprobe")
	return e1 == nil && e2 == nil
}

// Duration liest die Länge einer Aufnahme mit ffprobe.
func Duration(ctx context.Context, path string) (time.Duration, error) {
	if !Available() {
		return 0, ErrNoFFmpeg
	}
	out, err := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-show_entries", "format=duration",
		"-of", "default=nw=1:nk=1", path).Output()
	if err != nil {
		return 0, fmt.Errorf("ffprobe: %w (%v)", ErrUnreadable, err)
	}
	s, err := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
	if err != nil || s <= 0 || math.IsInf(s, 0) || math.IsNaN(s) {
		return 0, fmt.Errorf("ffprobe: %w, länge unbekannt (%q)", ErrUnreadable, strings.TrimSpace(string(out)))
	}
	return time.Duration(s * float64(time.Second)), nil
}

// Plan liefert Beginn und Länge der Stücke für eine Aufnahme der Länge total.
func Plan(total, chunk, overlap time.Duration) [][2]time.Duration {
	if total <= chunk {
		return [][2]time.Duration{{0, total}}
	}
	var out [][2]time.Duration
	for start := time.Duration(0); ; start += chunk - overlap {
		l := min(chunk, total-start)
		out = append(out, [2]time.Duration{start, l})
		if start+l >= total {
			return out
		}
	}
}

// Split schneidet die Aufnahme in überlappende Stücke (Mono, 16 kHz, MP3).
func Split(ctx context.Context, path string, chunk, overlap time.Duration) ([]Chunk, time.Duration, error) {
	total, err := Duration(ctx, path)
	if err != nil {
		return nil, 0, err
	}
	var out []Chunk
	for i, p := range Plan(total, chunk, overlap) {
		var buf, stderr bytes.Buffer
		cmd := exec.CommandContext(ctx, "ffmpeg", "-nostdin", "-v", "error",
			"-ss", fmt.Sprintf("%.3f", p[0].Seconds()), "-i", path, "-t", fmt.Sprintf("%.3f", p[1].Seconds()),
			"-vn", "-ac", "1", "-ar", "16000", "-b:a", bitrate, "-f", "mp3", "pipe:1")
		cmd.Stdout, cmd.Stderr = &buf, &stderr
		if err := cmd.Run(); err != nil {
			return nil, 0, fmt.Errorf("ffmpeg, stück %d: %w: %s", i, err, strings.TrimSpace(stderr.String()))
		}
		if buf.Len() > protocol.MaxAudioBytes {
			return nil, 0, fmt.Errorf("stück %d mit %d bytes größer als ein teilauftrag erlaubt", i, buf.Len())
		}
		out = append(out, Chunk{Index: i, Start: p[0], Len: p[1], Data: buf.Bytes()})
	}
	return out, total, nil
}

// minMatch: So viele gleiche Wörter in Folge gelten als sichere Fundstelle der
// Überlappung.
const minMatch = 4

// norm vereinheitlicht ein Wort für den Vergleich: klein, ohne Satzzeichen.
func norm(w string) string {
	return strings.ToLower(strings.TrimFunc(w, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }))
}

// Merge fügt die Abschriften aufeinanderfolgender Stücke zusammen. In der
// Überlappung sucht es die längste gemeinsame Wortfolge zwischen dem Ende des
// einen und dem Anfang des nächsten Stücks und schneidet in deren Mitte.
// Findet es keine, schätzt es den doppelten Anteil aus share (Überlappung im
// Verhältnis zur Länge des nächsten Stücks) und lässt ihn beim nächsten weg.
func Merge(parts []string, share []float64) string {
	var words []string
	for i, p := range parts {
		next := strings.Fields(p)
		if i == 0 || len(words) == 0 {
			words = append(words, next...)
			continue
		}
		s := 0.1
		if i-1 < len(share) {
			s = share[i-1]
		}
		window := int(math.Ceil(float64(len(next))*s*2)) + 20
		a0 := max(0, len(words)-window)
		ia, jb, l := longestCommon(words[a0:], next[:min(window, len(next))])
		if l >= minMatch {
			cut := l / 2
			words = append(words[:a0+ia+cut], next[jb+cut:]...)
			continue
		}
		skip := int(math.Round(float64(len(next)) * s))
		words = append(words, next[min(skip, len(next)):]...)
	}
	return strings.Join(words, " ")
}

// longestCommon: längste gemeinsame zusammenhängende Wortfolge von a und b
// (normalisiert); liefert Anfang in a, Anfang in b und Länge.
func longestCommon(a, b []string) (int, int, int) {
	na, nb := make([]string, len(a)), make([]string, len(b))
	for i, w := range a {
		na[i] = norm(w)
	}
	for j, w := range b {
		nb[j] = norm(w)
	}
	prev, cur := make([]int, len(b)+1), make([]int, len(b)+1)
	bi, bj, bl := 0, 0, 0
	for i := 1; i <= len(a); i++ {
		for j := 1; j <= len(b); j++ {
			if na[i-1] != "" && na[i-1] == nb[j-1] {
				cur[j] = prev[j-1] + 1
				if cur[j] > bl {
					bi, bj, bl = i-cur[j], j-cur[j], cur[j]
				}
			} else {
				cur[j] = 0
			}
		}
		prev, cur = cur, prev
	}
	return bi, bj, bl
}
