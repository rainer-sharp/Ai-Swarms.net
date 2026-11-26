// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Rainer Sestig. Kommerzielle Lizenz: siehe COMMERCIAL.md.

package jobs

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rainer-sharp/Ai-Swarms.net/internal/protocol"
)

func TestStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jobs.json")
	s := Open(path)
	now := time.Now()
	s.now = func() time.Time { return now }
	canceled := false
	s.Add(Job{ID: "eigen", Status: protocol.JobRunning, Created: now}, func() { canceled = true })
	s.Add(Job{ID: "entfernt", Coordinator: "peer", Status: protocol.JobRunning, Created: now.Add(time.Second)}, nil)
	s.Add(Job{ID: "fremd", Owner: "peer-a", Status: protocol.JobRunning, Created: now}, nil)
	s.Add(Job{ID: "fertig", Status: protocol.JobRunning, Created: now.Add(2 * time.Second)}, nil)
	s.Update("", "fertig", func(j *Job) { j.Status, j.Content = protocol.JobDone, "antwort" })

	if got := s.List(""); len(got) != 3 || got[0].ID != "fertig" || got[2].ID != "eigen" {
		t.Fatalf("liste: %+v", got)
	}
	if s.RunningFor("") != 1 || s.RunningFor("peer-a") != 1 || s.RunningFor("peer-b") != 0 {
		t.Fatal("laufende für andere")
	}
	// Abgeschlossene Aufträge ändern sich nicht mehr.
	if j, _ := s.Update("", "fertig", func(j *Job) { j.Status = protocol.JobFailed }); j.Status != protocol.JobDone || j.Finished.IsZero() {
		t.Fatalf("abgeschlossen geändert: %+v", j)
	}
	if j, _ := s.Cancel("", "eigen"); !canceled || j.Status != protocol.JobCanceled {
		t.Fatalf("abbruch: %v %+v", canceled, j)
	}
	if _, ok := s.Get("", "fremd"); ok {
		t.Fatal("fremder auftrag unter eigenem schlüssel")
	}
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("datei: %v %v", fi.Mode(), err)
	}

	// Neustart: Was hier lief, ist verloren; entfernt koordinierte laufen weiter.
	s.Add(Job{ID: "lief", Status: protocol.JobRunning, Created: now}, nil)
	r := Open(path)
	for id, want := range map[string]string{"lief": protocol.JobFailed, "entfernt": protocol.JobRunning, "fertig": protocol.JobDone} {
		if j, ok := r.Get("", id); !ok || j.Status != want {
			t.Errorf("%s nach neustart: %+v", id, j)
		}
	}
	if j, _ := r.Get("peer-a", "fremd"); j.Status != protocol.JobFailed {
		t.Errorf("fremder auftrag nach neustart: %+v", j)
	}

	// Nach Retention fallen abgeschlossene weg.
	r.now = func() time.Time { return now.Add(Retention + time.Hour) }
	r.Remove("", "entfernt")
	if _, ok := r.Get("", "fertig"); ok {
		t.Fatal("abgelaufener auftrag noch da")
	}
}

func TestStoreLimit(t *testing.T) {
	s := Open("")
	now := time.Now()
	for i := range MaxJobs + 5 {
		id := NewID()
		s.Add(Job{ID: id, Status: protocol.JobRunning, Created: now}, nil)
		s.now = func() time.Time { return now.Add(time.Duration(i) * time.Second) }
		s.Update("", id, func(j *Job) { j.Status = protocol.JobDone })
	}
	if n := len(s.List("")); n != MaxJobs {
		t.Fatalf("%d aufträge, erwartet %d", n, MaxJobs)
	}
}
