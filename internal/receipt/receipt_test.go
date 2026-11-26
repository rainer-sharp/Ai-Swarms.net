// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Rainer Sestig und Mitwirkende.

package receipt

import (
	"testing"
	"time"

	"github.com/rainer-sharp/Ai-Swarms.net/internal/identity"
)

func TestTwoSidedReceipt(t *testing.T) {
	coord, _ := identity.New()
	prov, _ := identity.New()
	r := Receipt{TaskID: "t", SubtaskID: "s", Coordinator: coord.ID.String(), Provider: prov.ID.String(),
		Status: "ok", FinishedAt: time.Now(), DurationMs: 1200}
	if err := r.SignAsCoordinator(coord); err != nil {
		t.Fatal(err)
	}
	if r.Verify() == nil {
		t.Fatal("einseitig signierte quittung gilt als gültig")
	}
	if err := r.SignAsProvider(prov); err != nil {
		t.Fatal(err)
	}
	if err := r.Verify(); err != nil {
		t.Fatal(err)
	}
	tampered := r
	tampered.DurationMs = 99999
	if tampered.Verify() == nil {
		t.Fatal("manipulierte quittung akzeptiert")
	}

	path := t.TempDir() + "/receipts.jsonl"
	s, _ := OpenStore(path)
	if err := s.Add(r); err != nil {
		t.Fatal(err)
	}
	_ = s.Add(r) // doppelt
	s2, err := OpenStore(path)
	if err != nil || s2.Count() != 1 {
		t.Fatalf("store nach neustart: %v count=%d", err, s2.Count())
	}
	if sum := s2.Summary(prov.ID.String()); sum[coord.ID.String()].Given != 1 {
		t.Fatalf("summary: %+v", sum)
	}
}
