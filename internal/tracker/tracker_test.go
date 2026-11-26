// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Rainer Sestig und Mitwirkende.

package tracker

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
)

func newKey(t *testing.T) (crypto.PrivKey, peer.ID) {
	t.Helper()
	priv, _, err := crypto.GenerateEd25519Key(nil)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := peer.IDFromPrivateKey(priv)
	return priv, id
}

func TestRecordSignature(t *testing.T) {
	priv, _ := newKey(t)
	r, err := NewRecord(priv, "swarmainet", []string{"/ip4/93.184.215.14/tcp/4711"}, 2*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := r.Verify("swarmainet", now); err != nil {
		t.Fatal(err)
	}
	if r.ExpiresAt.After(now.Add(MaxTTL + time.Second)) {
		t.Fatal("ttl nicht begrenzt")
	}
	if r.Verify("anderes-netz", now) == nil {
		t.Fatal("anderes netz akzeptiert")
	}
	if r.Verify("swarmainet", r.ExpiresAt) == nil {
		t.Fatal("abgelaufen akzeptiert")
	}
	r.Addrs = append(r.Addrs, "/ip4/6.6.6.6/tcp/1")
	if r.Verify("swarmainet", now) == nil {
		t.Fatal("veränderte adressen akzeptiert")
	}
}

func TestSealer(t *testing.T) {
	priv, id := newKey(t)
	a, _ := NewSealer(bytes.Repeat([]byte{1}, 32))
	b, _ := NewSealer(bytes.Repeat([]byte{2}, 32))
	if a.Net() == b.Net() || len(a.Net()) != 32 || a.ID(id) == b.ID(id) || strings.Contains(a.ID(id), id.String()) {
		t.Fatal("kennungen nicht getrennt oder nicht verschleiert")
	}
	r, _ := NewRecord(priv, "p-home", []string{"/ip4/192.168.1.2/tcp/4711"}, MaxTTL)
	sealed, err := a.Seal(r)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed, []byte("192.168")) || bytes.Contains(sealed, []byte(id.String())) {
		t.Fatal("klartext im verschlüsselten eintrag")
	}
	got, err := a.Open(sealed)
	if err != nil || got.PeerID != r.PeerID || got.Verify("p-home", time.Now()) != nil {
		t.Fatalf("%+v %v", got, err)
	}
	if _, err := b.Open(sealed); err == nil {
		t.Fatal("fremder schlüssel entschlüsselt")
	}
}

func TestServer(t *testing.T) {
	srv := httptest.NewServer(NewServer())
	defer srv.Close()
	ctx := context.Background()
	priv, id := newKey(t)
	rec, _ := NewRecord(priv, "swarmainet", []string{"/ip4/93.184.215.14/tcp/4711"}, MaxTTL)
	raw, _ := json.Marshal(rec)

	if err := Publish(ctx, srv.URL, Entry{Net: "swarmainet", ID: id.String(), Record: raw}); err != nil {
		t.Fatal(err)
	}
	// Fremde ID, falsches Netz, kaputter Eintrag: abgewiesen
	_, other := newKey(t)
	for _, e := range []Entry{
		{Net: "swarmainet", ID: other.String(), Record: raw},
		{Net: "anderes", ID: id.String(), Record: raw},
		{Net: "swarmainet", ID: id.String(), Record: json.RawMessage(`{"v":1}`)},
		{Net: "swarmainet", ID: "../x", Record: raw},
		{Net: "x", ID: "y", Record: json.RawMessage(`"` + strings.Repeat("A", MaxRecord) + `"`)},
	} {
		if Publish(ctx, srv.URL, e) == nil {
			t.Errorf("angenommen: %s %s", e.Net, e.ID)
		}
	}
	// Private Einträge sind undurchsichtig und werden ohne Prüfung gespeichert.
	if err := Publish(ctx, srv.URL, Entry{Net: strings.Repeat("ab", 16), ID: strings.Repeat("cd", 16), Record: json.RawMessage(`"geheim"`)}); err != nil {
		t.Fatal(err)
	}
	recs, ip, err := Fetch(ctx, srv.URL, "swarmainet")
	if err != nil || len(recs) != 1 || ip != "127.0.0.1" {
		t.Fatalf("%d einträge, ip %q, %v", len(recs), ip, err)
	}
	var back Record
	if json.Unmarshal(recs[0], &back) != nil || back.Verify("swarmainet", time.Now()) != nil {
		t.Fatal("eintrag verändert")
	}

	// Ratenlimit je IP
	limited := false
	for range reqPerMinute + 1 {
		resp, err := http.Get(srv.URL + "?net=swarmainet")
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		limited = limited || resp.StatusCode == http.StatusTooManyRequests
	}
	if !limited {
		t.Fatal("kein ratenlimit")
	}
}

func TestCheckURL(t *testing.T) {
	for u, ok := range map[string]bool{
		"https://example.org/swarmainet": true,
		"http://127.0.0.1:7720":          true,
		"http://localhost:7720":          true,
		"http://example.org":             false,
		"ftp://example.org":              false,
	} {
		if (CheckURL(u) == nil) != ok {
			t.Errorf("%s: erwartet %v", u, ok)
		}
	}
}

func TestServerProxyIPAndLimit(t *testing.T) {
	s := NewServer()
	s.TrustProxy = true
	r := httptest.NewRequest(http.MethodGet, "/?net=x", nil)
	r.Header.Set("X-Forwarded-For", "1.2.3.4, 5.6.7.8")
	if ip := s.clientIP(r); ip != "5.6.7.8" {
		t.Fatalf("ip %q: der vom eigenen proxy angehängte letzte eintrag zählt", ip)
	}

	s = NewServer()
	s.total = maxEntries
	w := httptest.NewRecorder()
	body := `{"net":"` + strings.Repeat("ab", 16) + `","id":"` + strings.Repeat("cd", 16) + `","record":"x"}`
	s.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body)))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("gesamtgrenze nicht durchgesetzt: %d", w.Code)
	}
}
