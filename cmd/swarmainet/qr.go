// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Rainer Sestig und Mitwirkende.

package main

import (
	"fmt"
	"io"
	"strings"

	"rsc.io/qr"
)

// qrQuiet ist der helle Rand um den Code in Modulen. Die Norm verlangt 4;
// Kamera-Apps lesen auch 2, und der Code passt so in schmalere Fenster.
const qrQuiet = 2

// Farben fest schwarz auf weiß, damit der Code in hellen und dunklen
// Terminals gleich aussieht und nicht invertiert gelesen werden muss.
const (
	qrColor = "\x1b[30;107m"
	qrReset = "\x1b[0m"
)

// renderQR zeichnet den Code mit Halbblöcken: Jede Textzeile trägt zwei
// Modulzeilen, oben im Vordergrund ▀, unten ▄. Black ist außerhalb des
// Codes falsch, das ergibt den hellen Rand.
func renderQR(c *qr.Code) string {
	black := c.Black
	var b strings.Builder
	for y := -qrQuiet; y < c.Size+qrQuiet; y += 2 {
		b.WriteString(qrColor)
		for x := -qrQuiet; x < c.Size+qrQuiet; x++ {
			switch top, bottom := black(x, y), black(x, y+1); {
			case top && bottom:
				b.WriteString("█")
			case top:
				b.WriteString("▀")
			case bottom:
				b.WriteString("▄")
			default:
				b.WriteString(" ")
			}
		}
		b.WriteString(qrReset + "\n")
	}
	return b.String()
}

// printQR gibt text als QR-Code aus, etwa damit ein Telefon den Link abliest.
func printQR(w io.Writer, text string) error {
	c, err := qr.Encode(text, qr.L) // geringste Redundanz: lange Links bleiben lesbar
	if err != nil {
		return fmt.Errorf("qr-code: %w", err)
	}
	fmt.Fprintf(w, "\nQR-Code (%d Zeichen breit, Fenster bei Bedarf vergrößern):\n", c.Size+2*qrQuiet)
	_, err = io.WriteString(w, renderQR(c))
	return err
}
