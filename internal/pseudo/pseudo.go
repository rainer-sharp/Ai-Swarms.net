// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Rainer Sestig. Kommerzielle Lizenz: siehe COMMERCIAL.md.

// Package pseudo ersetzt persönliche Daten durch Platzhalter wie [PERSON_1]
// oder [EMAIL_2] und tauscht sie in Antworten zurück (Freigabeklasse reduced,
// spec/privacy-classes.md). Feste Regeln erkennen E-Mail-Adressen,
// Telefonnummern, IBAN, Kartennummern, IP-Adressen und URLs mit Zugangsdaten;
// Namen, Firmen und Orte kommen von außen (eigene Liste, Erkennung durch ein
// eigenes Modell).
package pseudo

import (
	"fmt"
	"net/netip"
	"regexp"
	"slices"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

// Kind ist die Art eines ersetzten Werts; sie steht im Platzhalter.
type Kind string

const (
	Person   Kind = "PERSON"
	Org      Kind = "ORG"
	Location Kind = "LOCATION"
	Term     Kind = "TERM" // aus der eigenen Liste (tasks.pseudonymize_terms)
	Email    Kind = "EMAIL"
	Phone    Kind = "PHONE"
	IBAN     Kind = "IBAN"
	Card     Kind = "CARD"
	IP       Kind = "IP"
	URL      Kind = "URL"
)

// Kinds, die von außen kommen dürfen (Liste, Modell).
var namedKinds = []Kind{Person, Org, Location, Term}

// ValidNamedKind: Art für einen Namen, eine Firma oder einen Ort.
func ValidNamedKind(k Kind) bool { return slices.Contains(namedKinds, k) }

// placeholderRe erkennt Platzhalter in Antworten.
var placeholderRe = regexp.MustCompile(`\[(PERSON|ORG|LOCATION|TERM|EMAIL|PHONE|IBAN|CARD|IP|URL)_(\d{1,4})\]`)

// maxPlaceholderLen: längster Platzhalter, etwa [LOCATION_9999].
const maxPlaceholderLen = len("[LOCATION_9999]")

// Regeln für Werte mit fester Form. Reihenfolge: erst die spezifischeren.
var rules = []struct {
	kind  Kind
	re    *regexp.Regexp
	check func(string) bool
}{
	// URL mit Benutzer und Passwort: schützt Zugangsdaten, nicht jede URL.
	{URL, regexp.MustCompile(`\b[a-zA-Z][a-zA-Z0-9+.-]*://[^\s/:@]+:[^\s/@]+@[^\s]+`), nil},
	{Email, regexp.MustCompile(`\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}\b`), nil},
	{IBAN, regexp.MustCompile(`\b[A-Z]{2}\d{2}(?: ?[A-Z0-9]{4}){3,7}(?: ?[A-Z0-9]{1,4})?\b`), validIBAN},
	{Card, regexp.MustCompile(`\b\d{4}(?:[ -]?\d{4}){2}[ -]?\d{1,7}\b`), luhn},
	{IP, regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b|\b(?:[0-9A-Fa-f]{1,4}:){2,7}[0-9A-Fa-f]{0,4}(?::[0-9A-Fa-f]{1,4}){0,5}\b`), validIP},
	// Telefon: international mit +, oder national mit führender 0, mindestens
	// 8 Ziffern; Datumsangaben und Versionsnummern fallen durch die Form heraus.
	{Phone, regexp.MustCompile(`(?:\+\d{1,3}[ \-/]?|\b0)\d{1,5}(?:[ \-/]?\d{2,8}){1,4}\b`), validPhone},
}

// Mapping hält die Zuordnung Original ↔ Platzhalter für eine Anfrage. Gleiche
// Werte bekommen überall denselben Platzhalter. Sicher für mehrere Goroutinen.
type Mapping struct {
	mu      sync.Mutex
	forward map[string]string // Original → Platzhalter
	reverse map[string]string // Platzhalter → Original
	named   []string          // von außen gemeldete Werte, längste zuerst
	count   map[Kind]int
}

func New() *Mapping {
	return &Mapping{forward: map[string]string{}, reverse: map[string]string{}, count: map[Kind]int{}}
}

// Add meldet einen Namen, eine Firma, einen Ort oder einen eigenen Begriff an.
// Kurze oder leere Werte werden ignoriert, damit nicht jedes Vorkommen eines
// Allerweltsworts ersetzt wird.
func (m *Mapping) Add(kind Kind, value string) {
	value = strings.TrimSpace(value)
	if utf8.RuneCountInString(value) < 2 || !ValidNamedKind(kind) || placeholderRe.MatchString(value) {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.forward[value]; ok {
		return
	}
	m.assignLocked(kind, value)
	m.named = append(m.named, value)
	slices.SortStableFunc(m.named, func(a, b string) int { return len(b) - len(a) })
}

func (m *Mapping) assignLocked(kind Kind, value string) string {
	if p, ok := m.forward[value]; ok {
		return p
	}
	m.count[kind]++
	p := fmt.Sprintf("[%s_%d]", kind, m.count[kind])
	m.forward[value], m.reverse[p] = p, value
	return p
}

// Len: Zahl der ersetzten Werte.
func (m *Mapping) Len() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.forward)
}

// Apply ersetzt alle bekannten Werte und alles, was die festen Regeln finden.
// Platzhalter, die schon im Text stehen, bleiben unverändert.
func (m *Mapping) Apply(text string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, v := range m.named {
		text = replaceWord(text, v, m.forward[v])
	}
	for _, r := range rules {
		text = r.re.ReplaceAllStringFunc(text, func(s string) string {
			if r.check != nil && !r.check(s) {
				return s
			}
			return m.assignLocked(r.kind, s)
		})
	}
	return text
}

// replaceWord ersetzt v nur als ganzes Wort (nicht „Anna“ in „Annahme“);
// Groß- und Kleinschreibung zählen.
func replaceWord(text, v, p string) string {
	var b strings.Builder
	for {
		i := strings.Index(text, v)
		if i < 0 {
			b.WriteString(text)
			return b.String()
		}
		before, _ := utf8.DecodeLastRuneInString(text[:i])
		after, _ := utf8.DecodeRuneInString(text[i+len(v):])
		b.WriteString(text[:i])
		if (i == 0 || !wordRune(before)) && (i+len(v) == len(text) || !wordRune(after)) {
			b.WriteString(p)
		} else {
			b.WriteString(v)
		}
		text = text[i+len(v):]
	}
}

func wordRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' }

// Revert tauscht Platzhalter zurück; unbekannte bleiben stehen.
func (m *Mapping) Revert(text string) string {
	if !strings.Contains(text, "[") {
		return text
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return placeholderRe.ReplaceAllStringFunc(text, func(p string) string {
		if v, ok := m.reverse[p]; ok {
			return v
		}
		return p
	})
}

// Hint: Anweisung für ein Modell, das pseudonymisierten Text bekommt.
const Hint = "Der Text enthält Platzhalter wie [PERSON_1], [ORG_1] oder [EMAIL_1] für persönliche Daten. " +
	"Übernimm sie unverändert, wo du dich auf sie beziehst, und versuche nicht, sie zu erraten."

// View nummeriert die Platzhalter für einen Teilauftrag neu, in der
// Reihenfolge ihres Auftretens: Ein fremder Agent sieht [PERSON_1], auch wenn
// es in der ganzen Anfrage [PERSON_3] ist, und erfährt so nicht, wie viele
// Angaben es außerhalb seines Teilauftrags gibt.
type View struct {
	m        *Mapping
	mu       sync.Mutex
	toGlobal map[string]string // lokaler → globaler Platzhalter
	toLocal  map[string]string
	count    map[string]int
}

func (m *Mapping) View() *View {
	return &View{m: m, toGlobal: map[string]string{}, toLocal: map[string]string{}, count: map[string]int{}}
}

// Apply ersetzt wie Mapping.Apply und nummeriert die Platzhalter neu.
func (v *View) Apply(text string) string {
	text = v.m.Apply(text)
	v.m.mu.Lock()
	defer v.m.mu.Unlock()
	v.mu.Lock()
	defer v.mu.Unlock()
	return placeholderRe.ReplaceAllStringFunc(text, func(p string) string {
		if _, known := v.m.reverse[p]; !known {
			return p
		}
		if l, ok := v.toLocal[p]; ok {
			return l
		}
		kind := placeholderRe.FindStringSubmatch(p)[1]
		v.count[kind]++
		l := fmt.Sprintf("[%s_%d]", kind, v.count[kind])
		v.toLocal[p], v.toGlobal[l] = l, p
		return l
	})
}

// Revert tauscht die Platzhalter dieses Teilauftrags zurück; unbekannte
// bleiben stehen.
func (v *View) Revert(text string) string {
	if !strings.Contains(text, "[") {
		return text
	}
	v.m.mu.Lock()
	defer v.m.mu.Unlock()
	v.mu.Lock()
	defer v.mu.Unlock()
	return placeholderRe.ReplaceAllStringFunc(text, func(p string) string {
		if g, ok := v.toGlobal[p]; ok {
			return v.m.reverse[g]
		}
		return p
	})
}

// reverter: Mapping oder View.
type reverter interface{ Revert(string) string }

// Stream tauscht Platzhalter in einem Strom von Textstücken zurück. Ein
// Platzhalter kann über zwei Stücke verteilt ankommen; deshalb hält Stream
// ein angefangenes „[“ zurück, bis klar ist, ob ein Platzhalter folgt.
type Stream struct {
	m       reverter
	pending string
}

func (m *Mapping) Stream() *Stream { return &Stream{m: m} }
func (v *View) Stream() *Stream    { return &Stream{m: v} }

// Write nimmt ein Stück an und liefert den Text, der sicher ausgegeben werden kann.
func (s *Stream) Write(chunk string) string {
	text := s.pending + chunk
	s.pending = ""
	if i := strings.LastIndex(text, "["); i >= 0 && !strings.Contains(text[i:], "]") && len(text)-i < maxPlaceholderLen {
		s.pending, text = text[i:], text[:i]
	}
	return s.m.Revert(text)
}

// Flush liefert den zurückgehaltenen Rest am Ende des Stroms.
func (s *Stream) Flush() string {
	t := s.pending
	s.pending = ""
	return s.m.Revert(t)
}

func luhn(s string) bool {
	sum, n, double := 0, 0, false
	for i := len(s) - 1; i >= 0; i-- {
		c := s[i]
		if c < '0' || c > '9' {
			continue
		}
		d := int(c - '0')
		if double {
			if d *= 2; d > 9 {
				d -= 9
			}
		}
		sum += d
		double = !double
		n++
	}
	return n >= 13 && n <= 19 && sum%10 == 0
}

func validIBAN(s string) bool {
	s = strings.ReplaceAll(s, " ", "")
	if len(s) < 15 || len(s) > 34 {
		return false
	}
	s = s[4:] + s[:4]
	rem := 0
	for _, c := range s {
		switch {
		case c >= '0' && c <= '9':
			rem = (rem*10 + int(c-'0')) % 97
		case c >= 'A' && c <= 'Z':
			rem = (rem*100 + int(c-'A'+10)) % 97
		default:
			return false
		}
	}
	return rem == 1
}

func validIP(s string) bool {
	a, err := netip.ParseAddr(s)
	// Loopback und „alle Adressen“ verraten nichts über Personen und stehen
	// oft in Konfigurationsfragen; sie bleiben lesbar.
	return err == nil && !a.IsUnspecified() && !a.IsLoopback()
}

func validPhone(s string) bool {
	digits := 0
	for _, c := range s {
		if c >= '0' && c <= '9' {
			digits++
		}
	}
	return digits >= 8 && digits <= 15
}
