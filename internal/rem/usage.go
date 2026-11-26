// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Rainer Sestig und Mitwirkende.

package rem

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// UsageCounter zählt GPU-Sekunden für fremde Aufträge pro Tag; neustartfest.
type UsageCounter struct {
	mu   sync.Mutex
	path string
	Date string  `json:"date"`
	Secs float64 `json:"secs"`
}

func LoadUsage(path string) *UsageCounter {
	u := &UsageCounter{path: path}
	if path == "" {
		return u
	}
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, u)
	}
	return u
}

func (u *UsageCounter) roll(now time.Time) {
	if d := now.Format("2006-01-02"); d != u.Date {
		u.Date, u.Secs = d, 0
	}
}

func (u *UsageCounter) Used(now time.Time) time.Duration {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.roll(now)
	return time.Duration(u.Secs * float64(time.Second))
}

func (u *UsageCounter) Add(now time.Time, d time.Duration) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.roll(now)
	u.Secs += d.Seconds()
	if u.path == "" {
		return nil
	}
	data, _ := json.Marshal(u)
	tmp := u.path + ".tmp"
	if err := os.MkdirAll(filepath.Dir(u.path), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, u.path)
}
