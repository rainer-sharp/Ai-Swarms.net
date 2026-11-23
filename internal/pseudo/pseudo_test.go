// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Rainer Sestig. Kommerzielle Lizenz: siehe COMMERCIAL.md.

package pseudo

import (
	"strings"
	"testing"
)

// TestRules: Die festen Regeln finden Werte mit fester Form und lassen
// Ähnliches stehen (Datum, Version, ungültige Prüfsummen, Loopback).
func TestRules(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"Schreib an anna.berg@example.org bitte", "Schreib an [EMAIL_1] bitte"},
		{"Ruf an: +49 170 1234567 oder 030/12345678", "Ruf an: [PHONE_1] oder [PHONE_2]"},
		{"IBAN DE89 3704 0044 0532 0130 00 überweisen", "IBAN [IBAN_1] überweisen"},
		{"Karte 4111 1111 1111 1111 und 4111 1111 1111 1112", "Karte [CARD_1] und 4111 1111 1111 1112"},
		{"Server 192.168.101.94 und 2a03:4000:f:15bd::1, lokal 127.0.0.1", "Server [IP_1] und [IP_2], lokal 127.0.0.1"},
		{"git clone https://max:geheim@git.example.org/repo.git", "git clone [URL_1]"},
		{"Termin am 2026-11-23, Version 1.27.2, Port 4711", "Termin am 2026-11-23, Version 1.27.2, Port 4711"},
	} {
		if got := New().Apply(tc.in); got != tc.want {
			t.Errorf("\n%q\n%q", got, tc.want)
		}
	}
	// Ungültige Prüfsumme: keine IBAN (Teile darf die Telefonregel verbergen).
	if got := New().Apply("IBAN DE00 3704 0044 0532 0130 00"); strings.Contains(got, "[IBAN") {
		t.Errorf("ungültige iban erkannt: %q", got)
	}
}

// TestNamedAndRevert: Namen nur als ganze Wörter, längere zuerst, gleiche
// Werte überall gleich; Revert stellt das Original her.
func TestNamedAndRevert(t *testing.T) {
	m := New()
	m.Add(Person, "Anna")
	m.Add(Person, "Anna Berg")
	m.Add(Org, "Berg GmbH")
	m.Add(Location, "x") // zu kurz, ignoriert
	in := "Anna Berg von der Berg GmbH. Anna kommt zur Annahme. Mail: anna@berg.de"
	out := m.Apply(in)
	want := "[PERSON_2] von der [ORG_1]. [PERSON_1] kommt zur Annahme. Mail: [EMAIL_1]"
	if out != want {
		t.Fatalf("\n%q\n%q", out, want)
	}
	if again := m.Apply("Grüße an Anna Berg"); again != "Grüße an [PERSON_2]" {
		t.Fatalf("nicht gleich ersetzt: %q", again)
	}
	if back := m.Revert(out + " [PERSON_9]"); back != in+" [PERSON_9]" {
		t.Fatalf("%q", back)
	}
	if strings.Contains(m.Apply(out), "[[") {
		t.Fatal("platzhalter doppelt ersetzt")
	}
}

// TestStream: Ein Platzhalter, der über mehrere Stücke verteilt ankommt, wird
// trotzdem zurückgetauscht; gewöhnliche eckige Klammern gehen durch.
func TestStream(t *testing.T) {
	m := New()
	m.Add(Person, "Anna Berg")
	m.Apply("Anna Berg")
	s := m.Stream()
	var out strings.Builder
	for _, c := range []string{"Hallo [PER", "SON_", "1], siehe [Quelle] und a[0", "] und [", "x"} {
		out.WriteString(s.Write(c))
	}
	out.WriteString(s.Flush())
	if out.String() != "Hallo Anna Berg, siehe [Quelle] und a[0] und [x" {
		t.Fatalf("%q", out.String())
	}
}
