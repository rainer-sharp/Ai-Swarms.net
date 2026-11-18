// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Rainer Sestig. Kommerzielle Lizenz: siehe COMMERCIAL.md.

// Package tracker implementiert den HTTPS-Adressbriefkasten nach
// spec/p2p-layer.md § 2.3: Einträge, Server und Client.
package tracker

import (
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	"golang.org/x/crypto/chacha20poly1305"
)

const (
	sigDomain  = "swarmainet/tracker/v1\n"
	kdfInfo    = "swarmainet/tracker/v1"
	MaxTTL     = time.Hour        // länger gespeichert wird nichts
	Refresh    = 20 * time.Minute // so oft erneuern Knoten ihren Eintrag
	MaxRecord  = 2 << 10          // Bytes je Eintrag
	MaxRecords = 64               // Einträge je Antwort
)

// Record ist der Eintrag eines Knotens, im offenen Netz im Klartext.
type Record struct {
	V         int       `json:"v"`
	PeerID    string    `json:"peer_id"`
	Addrs     []string  `json:"addrs"`
	ExpiresAt time.Time `json:"expires_at"`
	Nonce     string    `json:"nonce"`
	Signature string    `json:"signature"`
}

func (r *Record) signedBytes(net string) []byte {
	return []byte(sigDomain + net + "\n" + r.PeerID + "\n" + r.ExpiresAt.UTC().Format(time.RFC3339Nano) + "\n" +
		r.Nonce + "\n" + strings.Join(r.Addrs, "\n"))
}

// NewRecord erzeugt einen signierten Eintrag mit Gültigkeit ttl (höchstens MaxTTL).
func NewRecord(priv crypto.PrivKey, net string, addrs []string, ttl time.Duration) (*Record, error) {
	id, err := peer.IDFromPrivateKey(priv)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, 16)
	_, _ = rand.Read(nonce)
	r := &Record{V: 1, PeerID: id.String(), Addrs: addrs, ExpiresAt: time.Now().Add(min(ttl, MaxTTL)).UTC(), Nonce: hex.EncodeToString(nonce)}
	sig, err := priv.Sign(r.signedBytes(net))
	if err != nil {
		return nil, err
	}
	r.Signature = base64.StdEncoding.EncodeToString(sig)
	return r, nil
}

// Verify prüft Version, Ablaufzeit (nicht abgelaufen, nicht weiter als MaxTTL
// in der Zukunft) und Signatur gegen den Schlüssel der Peer-ID.
func (r *Record) Verify(net string, now time.Time) error {
	if r.V != 1 {
		return fmt.Errorf("version %d", r.V)
	}
	if !now.Before(r.ExpiresAt) || r.ExpiresAt.After(now.Add(MaxTTL+time.Minute)) {
		return errors.New("ablaufzeit ungültig")
	}
	id, err := peer.Decode(r.PeerID)
	if err != nil {
		return err
	}
	pub, err := id.ExtractPublicKey()
	if err != nil {
		return err
	}
	sig, err := base64.StdEncoding.DecodeString(r.Signature)
	if err != nil {
		return err
	}
	if ok, err := pub.Verify(r.signedBytes(net), sig); err != nil || !ok {
		return errors.New("signatur ungültig")
	}
	return nil
}

// Entry ist die Leitungsform: POST-Rumpf und Element der GET-Antwort.
// Record ist im offenen Netz ein Record-Objekt, im privaten ein
// base64-String mit verschlüsseltem Record.
type Entry struct {
	Net    string          `json:"net"`
	ID     string          `json:"id"`
	Record json.RawMessage `json:"record"`
}

// --- privates Netz: der Tracker erfährt weder Netz noch Mitglieder ---

// Sealer leitet aus dem Netzschlüssel (PSK) die Kennungen und den
// Schlüssel für die Verschlüsselung der Einträge ab.
type Sealer struct {
	key [32]byte
	net string
}

func NewSealer(psk []byte) (*Sealer, error) {
	k, err := hkdf.Key(sha256.New, psk, nil, kdfInfo, 32)
	if err != nil {
		return nil, err
	}
	s := &Sealer{}
	copy(s.key[:], k)
	s.net = s.mac("net")
	return s, nil
}

func (s *Sealer) mac(msg string) string {
	h := hmac.New(sha256.New, s.key[:])
	h.Write([]byte(msg))
	return hex.EncodeToString(h.Sum(nil)[:16])
}

// Net ist die Netz-Kennung beim Tracker.
func (s *Sealer) Net() string { return s.net }

// ID ist die Kennung eines Mitglieds beim Tracker.
func (s *Sealer) ID(p peer.ID) string { return s.mac(p.String()) }

// Seal verschlüsselt einen Eintrag: base64(nonce ‖ XChaCha20-Poly1305).
func (s *Sealer) Seal(r *Record) (json.RawMessage, error) {
	plain, err := json.Marshal(r)
	if err != nil {
		return nil, err
	}
	aead, _ := chacha20poly1305.NewX(s.key[:])
	nonce := make([]byte, aead.NonceSize(), aead.NonceSize()+len(plain)+aead.Overhead())
	_, _ = rand.Read(nonce)
	return json.Marshal(base64.StdEncoding.EncodeToString(aead.Seal(nonce, nonce, plain, nil)))
}

// Open entschlüsselt einen Eintrag; fremde oder manipulierte Daten scheitern.
func (s *Sealer) Open(raw json.RawMessage) (*Record, error) {
	var b64 string
	if err := json.Unmarshal(raw, &b64); err != nil {
		return nil, err
	}
	data, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, err
	}
	aead, _ := chacha20poly1305.NewX(s.key[:])
	if len(data) < aead.NonceSize() {
		return nil, errors.New("eintrag zu kurz")
	}
	plain, err := aead.Open(nil, data[:aead.NonceSize()], data[aead.NonceSize():], nil)
	if err != nil {
		return nil, errors.New("eintrag nicht entschlüsselbar")
	}
	var r Record
	if err := json.Unmarshal(plain, &r); err != nil {
		return nil, err
	}
	return &r, nil
}
