// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Rainer Sestig. Kommerzielle Lizenz: siehe COMMERCIAL.md.

package p2p

import (
	"testing"
	"time"
)

// TestEpochs: Nach einer neueren Epoche lehnt der Anbieter ältere ab.
func TestEpochs(t *testing.T) {
	var e epochs
	now := time.Now()
	for _, c := range []struct {
		id    string
		epoch int
		ok    bool
	}{
		{"a", 1, true}, {"a", 1, true}, {"a", 2, true}, {"a", 1, false}, {"a", 2, true}, {"b", 1, true},
	} {
		if got := e.see(c.id, c.epoch, now); got != c.ok {
			t.Fatalf("%s epoche %d: %v, erwartet %v", c.id, c.epoch, got, c.ok)
		}
	}
	// Nach epochTTL ist das Wissen weg.
	if !e.see("a", 1, now.Add(epochTTL+time.Minute)) {
		t.Fatal("veraltete epoche nach ttl noch gesperrt")
	}
}
