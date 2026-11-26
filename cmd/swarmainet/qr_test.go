// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Rainer Sestig und Mitwirkende.

package main

import (
	"bytes"
	"strings"
	"testing"

	"rsc.io/qr"
)

// TestRenderQR liest die Halbblöcke zurück und vergleicht sie Modul für Modul.
func TestRenderQR(t *testing.T) {
	// So lang wie eine Einladung in ein Mitgliedsnetz mit drei Adressen.
	link := "swarmainet://join?v=2&net=m-0123456789abcdef&root=" + strings.Repeat("A", 52) +
		"&issuer=12D3KooW" + strings.Repeat("x", 44) + "&token=" + strings.Repeat("t", 43)
	for range 3 {
		link += "&peer=%2Fip4%2F203.0.113.7%2Ftcp%2F4711%2Fp2p%2F12D3KooW" + strings.Repeat("y", 44)
	}
	for _, text := range []string{"x", link} {
		c, err := qr.Encode(text, qr.L)
		if err != nil {
			t.Fatalf("%d zeichen: %v", len(text), err)
		}
		lines := strings.Split(strings.TrimSuffix(renderQR(c), "\n"), "\n")
		if want := (c.Size + 2*qrQuiet + 1) / 2; len(lines) != want {
			t.Fatalf("%d zeilen, erwartet %d", len(lines), want)
		}
		for i, line := range lines {
			if !strings.HasPrefix(line, qrColor) || !strings.HasSuffix(line, qrReset) {
				t.Fatalf("zeile %d ohne farben", i)
			}
			cells := []rune(strings.TrimSuffix(strings.TrimPrefix(line, qrColor), qrReset))
			if len(cells) != c.Size+2*qrQuiet {
				t.Fatalf("zeile %d: %d zeichen, erwartet %d", i, len(cells), c.Size+2*qrQuiet)
			}
			y := 2*i - qrQuiet
			for j, r := range cells {
				x := j - qrQuiet
				top, bottom := strings.ContainsRune("█▀", r), strings.ContainsRune("█▄", r)
				if top != c.Black(x, y) || bottom != c.Black(x, y+1) {
					t.Fatalf("modul (%d,%d): zeichen %q passt nicht", x, y, r)
				}
			}
		}
	}
}

func TestPrintQRTooLong(t *testing.T) {
	var out bytes.Buffer
	if err := printQR(&out, strings.Repeat("z", 3000)); err == nil {
		t.Fatal("zu langer text ohne fehler")
	}
}
