// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Rainer Sestig und Mitwirkende.

package tracker

import (
	"bytes"
	"encoding/json"
	"math/rand/v2"
	"net"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Grenzen des Servers (spec/p2p-layer.md § 2.3).
const (
	maxPerNet     = 256
	maxNets       = 1024
	maxEntries    = 16384 // gesamt; mit MaxRecord höchstens ~32 MiB
	maxBody       = 4 << 10
	reqPerMinute  = 60 // je Absender-IP
	cleanInterval = time.Minute
)

// keyRe: Netz- und Eintragskennungen (Netz-Kennung, Peer-ID oder 32 Hex-Zeichen).
var keyRe = regexp.MustCompile(`^[A-Za-z0-9-]{1,64}$`)

type stored struct {
	record  json.RawMessage
	expires time.Time
}

// Server hält Einträge nur im Speicher; sie laufen nach höchstens MaxTTL ab.
type Server struct {
	// TrustProxy: Absender-IP aus X-Forwarded-For (hinter einem Reverse-Proxy).
	TrustProxy bool

	mu    sync.Mutex
	nets  map[string]map[string]stored
	total int // Einträge über alle Netze
	rate  map[string]int
	reset time.Time
}

func NewServer() *Server {
	return &Server{nets: make(map[string]map[string]stored), rate: make(map[string]int)}
}

func (s *Server) clientIP(r *http.Request) string {
	if s.TrustProxy {
		// Der letzte Eintrag stammt vom eigenen Proxy; alle davor kann der
		// Absender selbst setzen.
		if h := r.Header.Values("X-Forwarded-For"); len(h) > 0 {
			parts := strings.Split(h[len(h)-1], ",")
			if ip := strings.TrimSpace(parts[len(parts)-1]); ip != "" {
				return ip
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// allow zählt Anfragen je IP in festen Minutenfenstern und räumt dabei
// abgelaufene Einträge weg.
func (s *Server) allow(ip string, now time.Time) bool {
	if now.Sub(s.reset) >= cleanInterval {
		s.reset = now
		clear(s.rate)
		for net, m := range s.nets {
			for id, e := range m {
				if !now.Before(e.expires) {
					delete(m, id)
					s.total--
				}
			}
			if len(m) == 0 {
				delete(s.nets, net)
			}
		}
	}
	s.rate[ip]++
	return s.rate[ip] <= reqPerMinute
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	ip := s.clientIP(r)
	s.mu.Lock()
	ok := s.allow(ip, now)
	s.mu.Unlock()
	if !ok {
		http.Error(w, "zu viele anfragen", http.StatusTooManyRequests)
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.get(w, r, ip, now)
	case http.MethodPost:
		s.post(w, r, now)
	default:
		http.Error(w, "nur GET und POST", http.StatusMethodNotAllowed)
	}
}

func (s *Server) get(w http.ResponseWriter, r *http.Request, ip string, now time.Time) {
	net := r.URL.Query().Get("net")
	if !keyRe.MatchString(net) {
		http.Error(w, "net ungültig", http.StatusBadRequest)
		return
	}
	out := struct {
		Records []json.RawMessage `json:"records"`
		YourIP  string            `json:"your_ip"`
	}{Records: []json.RawMessage{}, YourIP: ip}
	s.mu.Lock()
	for _, e := range s.nets[net] {
		if now.Before(e.expires) {
			out.Records = append(out.Records, e.record)
		}
	}
	s.mu.Unlock()
	rand.Shuffle(len(out.Records), func(i, j int) { out.Records[i], out.Records[j] = out.Records[j], out.Records[i] })
	out.Records = out.Records[:min(len(out.Records), MaxRecords)]
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

func (s *Server) post(w http.ResponseWriter, r *http.Request, now time.Time) {
	var e Entry
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody)).Decode(&e); err != nil {
		http.Error(w, "rumpf ungültig", http.StatusBadRequest)
		return
	}
	if !keyRe.MatchString(e.Net) || !keyRe.MatchString(e.ID) || len(e.Record) == 0 || len(e.Record) > MaxRecord {
		http.Error(w, "eintrag ungültig", http.StatusBadRequest)
		return
	}
	expires := now.Add(MaxTTL)
	if rec := bytes.TrimSpace(e.Record); rec[0] == '{' {
		// Offenes Netz: Klartext, also prüfbar. Gespeichert wird nur ein
		// gültig signierter Eintrag unter seiner eigenen Peer-ID.
		var rr Record
		if err := json.Unmarshal(rec, &rr); err != nil || rr.PeerID != e.ID || rr.Verify(e.Net, now) != nil {
			http.Error(w, "signatur oder ablaufzeit ungültig", http.StatusBadRequest)
			return
		}
		expires = rr.ExpiresAt
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.nets[e.Net]
	if m == nil {
		if len(s.nets) >= maxNets {
			http.Error(w, "tracker voll", http.StatusServiceUnavailable)
			return
		}
		m = make(map[string]stored)
		s.nets[e.Net] = m
	}
	if _, exists := m[e.ID]; !exists {
		if len(m) >= maxPerNet || s.total >= maxEntries {
			http.Error(w, "tracker voll", http.StatusServiceUnavailable)
			return
		}
		s.total++
	}
	m[e.ID] = stored{record: e.Record, expires: expires}
	w.WriteHeader(http.StatusNoContent)
}
