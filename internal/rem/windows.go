// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Rainer Sestig. Kommerzielle Lizenz: siehe COMMERCIAL.md.

package rem

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/rainer-sharp/Ai-Swarms.net/internal/config"
)

var dayNames = map[string]time.Weekday{
	"sun": time.Sunday, "mon": time.Monday, "tue": time.Tuesday, "wed": time.Wednesday,
	"thu": time.Thursday, "fri": time.Friday, "sat": time.Saturday,
}

// minutes parst "HH:MM" in Minuten seit Mitternacht; "24:00" ist erlaubt.
func minutes(s string) (int, error) {
	h, m, ok := strings.Cut(s, ":")
	if !ok {
		return 0, fmt.Errorf("uhrzeit %q: erwartet HH:MM", s)
	}
	hh, err1 := strconv.Atoi(h)
	mm, err2 := strconv.Atoi(m)
	if err1 != nil || err2 != nil || hh < 0 || mm < 0 || mm > 59 || hh*60+mm > 24*60 {
		return 0, fmt.Errorf("uhrzeit %q ungültig", s)
	}
	return hh*60 + mm, nil
}

// ValidateWindows prüft Tagesnamen und Uhrzeiten.
func ValidateWindows(ws []config.TimeWindow) error {
	for _, w := range ws {
		for _, d := range w.Days {
			if _, ok := dayNames[strings.ToLower(d)]; !ok {
				return fmt.Errorf("tag %q unbekannt", d)
			}
		}
		if _, err := minutes(w.From); err != nil {
			return err
		}
		if _, err := minutes(w.To); err != nil {
			return err
		}
	}
	return nil
}

// InWindow meldet, ob t in einem der Fenster liegt. Ohne Fenster ist immer frei.
// Ein Fenster mit von > bis reicht über Mitternacht und gehört zum Starttag.
func InWindow(t time.Time, ws []config.TimeWindow) bool {
	if len(ws) == 0 {
		return true
	}
	now := t.Hour()*60 + t.Minute()
	today := t.Weekday()
	yesterday := (today + 6) % 7
	for _, w := range ws {
		von, err1 := minutes(w.From)
		bis, err2 := minutes(w.To)
		if err1 != nil || err2 != nil {
			continue
		}
		for _, d := range w.Days {
			wd, ok := dayNames[strings.ToLower(d)]
			if !ok {
				continue
			}
			if von <= bis {
				if wd == today && now >= von && now < bis {
					return true
				}
				continue
			}
			// über Mitternacht
			if (wd == today && now >= von) || (wd == yesterday && now < bis) {
				return true
			}
		}
	}
	return false
}
