// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Rainer Sestig und Mitwirkende.

// Package membership prüft Zertifikate und Sperrlisten der Mitgliedsnetze
// (spec/membership.md). Es kennt keine Verbindungen und keine Dateien;
// Zulassung und Verteilung liegen in der P2P-Schicht.
package membership

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/rainer-sharp/Ai-Swarms.net/internal/identity"
)

// Rollen nach § 3.
const (
	RoleIssuer = "issuer"
	RoleMember = "member"
	RoleHelper = "helper"
)

const (
	// IssuerRoot steht als Aussteller, wenn die Wurzel selbst signiert hat.
	IssuerRoot = "root"

	// Vorgaben für die Laufzeit (§ 3).
	IssuerTTL = 365 * 24 * time.Hour
	NodeTTL   = 30 * 24 * time.Hour

	// ClockSkew ist die erlaubte Abweichung der Uhren.
	ClockSkew = 5 * time.Minute

	version = 1
	maxName = 64
)

var (
	ErrExpired  = errors.New("zertifikat abgelaufen")
	ErrNotYet   = errors.New("zertifikat noch nicht gültig")
	ErrRevoked  = errors.New("zertifikat widerrufen")
	ErrSubject  = errors.New("zertifikat gehört zu einem anderen peer")
	ErrNetwork  = errors.New("zertifikat oder sperrliste eines anderen netzes")
	ErrChain    = errors.New("zertifikatskette ungültig")
	ErrStaleCRL = errors.New("sperrliste nicht neuer als die gespeicherte")
)

// Cert ist ein Zertifikat. Die Reihenfolge der Felder ist Teil des Formats,
// weil sie die signierten Bytes bestimmt.
type Cert struct {
	V         int       `json:"v"`
	NetworkID string    `json:"network_id"`
	Serial    string    `json:"serial"`
	Subject   string    `json:"subject"`
	Role      string    `json:"role"`
	Name      string    `json:"name,omitempty"`
	IssuedAt  time.Time `json:"issued_at"`
	ExpiresAt time.Time `json:"expires_at"`
	Issuer    string    `json:"issuer"`
	Sig       string    `json:"sig,omitempty"`
}

func (c Cert) signingBytes() []byte {
	c.Sig = ""
	c.IssuedAt, c.ExpiresAt = c.IssuedAt.UTC(), c.ExpiresAt.UTC()
	return signedForm("swarmainet/member-cert/v1\n", c)
}

// Revocation ist ein Eintrag einer Sperrliste.
type Revocation struct {
	Serial    string    `json:"serial,omitempty"`
	Subject   string    `json:"subject"`
	RevokedAt time.Time `json:"revoked_at"`
	Reason    string    `json:"reason,omitempty"`
}

// CRL ist die Sperrliste eines Ausstellers. IssuerCert ist nicht signiert: Es
// weist den Aussteller nach und wird für sich gegen die Wurzel geprüft.
type CRL struct {
	V          int          `json:"v"`
	NetworkID  string       `json:"network_id"`
	Issuer     string       `json:"issuer"`
	Seq        uint64       `json:"seq"`
	IssuedAt   time.Time    `json:"issued_at"`
	Revoked    []Revocation `json:"revoked"`
	Sig        string       `json:"sig,omitempty"`
	IssuerCert *Cert        `json:"issuer_cert,omitempty"`
}

func (l CRL) signingBytes() []byte {
	l.Sig, l.IssuerCert = "", nil
	l.IssuedAt = l.IssuedAt.UTC()
	if l.Revoked == nil {
		l.Revoked = []Revocation{}
	}
	l.Revoked = slices.Clone(l.Revoked)
	for i := range l.Revoked {
		l.Revoked[i].RevokedAt = l.Revoked[i].RevokedAt.UTC()
	}
	return signedForm("swarmainet/member-crl/v1\n", l)
}

// signedForm: Präfix und JSON ohne Leerraum, ohne die HTML-Maskierung von Go,
// die andere Implementierungen nicht nachbilden würden (§ 3).
func signedForm(prefix string, v any) []byte {
	var b bytes.Buffer
	b.WriteString(prefix)
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
	return bytes.TrimSuffix(b.Bytes(), []byte("\n"))
}

// NetworkID leitet die Kennung aus dem Wurzelschlüssel ab (§ 2).
func NetworkID(rootPub []byte) string {
	h := sha256.Sum256(append([]byte("swarmainet/member-network/v1\n"), rootPub...))
	return "m-" + hex.EncodeToString(h[:8])
}

// RootKey liefert den öffentlichen Wurzelschlüssel einer Identität.
func RootKey(id *identity.Identity) ([]byte, error) {
	if id.Priv.Type() != crypto.Ed25519 {
		return nil, errors.New("wurzelschlüssel muss ed25519 sein")
	}
	return id.Priv.GetPublic().Raw()
}

// EncodeRoot und DecodeRoot sind die Textform des Wurzelschlüssels für Links
// und Konfiguration.
func EncodeRoot(rootPub []byte) string { return base64.RawURLEncoding.EncodeToString(rootPub) }

func DecodeRoot(s string) ([]byte, error) {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("wurzelschlüssel: %w", err)
	}
	if _, err := crypto.UnmarshalEd25519PublicKey(b); err != nil {
		return nil, fmt.Errorf("wurzelschlüssel: %w", err)
	}
	return b, nil
}

func sign(id *identity.Identity, data []byte) (string, error) {
	sig, err := id.Sign(data)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(sig), nil
}

func verifySig(pub crypto.PubKey, data []byte, sig string) error {
	raw, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil {
		return errors.New("signatur nicht lesbar")
	}
	ok, err := pub.Verify(data, raw)
	if err != nil || !ok {
		return errors.New("signatur ungültig")
	}
	return nil
}

// issuerKey liest den Schlüssel aus der Peer-ID eines Ausstellers.
func issuerKey(id string) (crypto.PubKey, error) {
	p, err := peer.Decode(id)
	if err != nil {
		return nil, fmt.Errorf("ungültige peer-id: %w", err)
	}
	return p.ExtractPublicKey()
}

// Issuer stellt Zertifikate aus: die Wurzel oder ein Knoten mit
// Aussteller-Zertifikat.
type Issuer struct {
	key     *identity.Identity
	network string
	cert    *Cert // nil bei der Wurzel
}

// NewRootIssuer stellt mit dem Wurzelschlüssel aus.
func NewRootIssuer(root *identity.Identity) (*Issuer, error) {
	pub, err := RootKey(root)
	if err != nil {
		return nil, err
	}
	return &Issuer{key: root, network: NetworkID(pub)}, nil
}

// NewIssuer stellt mit einem von der Wurzel ernannten Schlüssel aus.
func NewIssuer(key *identity.Identity, cert Cert) (*Issuer, error) {
	if cert.Role != RoleIssuer || cert.Issuer != IssuerRoot || cert.Subject != key.ID.String() {
		return nil, fmt.Errorf("%w: kein aussteller-zertifikat für diesen schlüssel", ErrChain)
	}
	return &Issuer{key: key, network: cert.NetworkID, cert: &cert}, nil
}

// NetworkID ist die Kennung des Netzes, für das ausgestellt wird.
func (i *Issuer) NetworkID() string { return i.network }

// Issue stellt ein Zertifikat aus und liefert die vollständige Kette. Ein
// Zertifikat gilt nie länger als das des Ausstellers.
func (i *Issuer) Issue(subject, role, name string, ttl time.Duration, now time.Time) ([]Cert, error) {
	if _, err := peer.Decode(subject); err != nil {
		return nil, fmt.Errorf("ungültige peer-id: %w", err)
	}
	switch role {
	case RoleMember, RoleHelper:
	case RoleIssuer:
		if i.cert != nil {
			return nil, fmt.Errorf("%w: aussteller ernennt nur die wurzel", ErrChain)
		}
	default:
		return nil, fmt.Errorf("unbekannte rolle %q", role)
	}
	if len(name) > maxName {
		return nil, fmt.Errorf("name länger als %d zeichen", maxName)
	}
	// Die Obergrenze erlaubt es, alte Einträge aus Sperrlisten zu entfernen.
	ttl = min(ttl, IssuerTTL)
	serial := make([]byte, 16)
	if _, err := rand.Read(serial); err != nil {
		return nil, err
	}
	now = now.UTC().Truncate(time.Second)
	c := Cert{
		V: version, NetworkID: i.network, Serial: base64.RawURLEncoding.EncodeToString(serial),
		Subject: subject, Role: role, Name: name, IssuedAt: now, ExpiresAt: now.Add(ttl), Issuer: IssuerRoot,
	}
	if i.cert != nil {
		c.Issuer = i.key.ID.String()
		if c.ExpiresAt.After(i.cert.ExpiresAt) {
			c.ExpiresAt = i.cert.ExpiresAt.UTC()
		}
	}
	if !c.ExpiresAt.After(c.IssuedAt) {
		return nil, ErrExpired
	}
	sig, err := sign(i.key, c.signingBytes())
	if err != nil {
		return nil, err
	}
	c.Sig = sig
	if i.cert != nil {
		return []Cert{*i.cert, c}, nil
	}
	return []Cert{c}, nil
}

// SignCRL signiert eine Sperrliste dieses Ausstellers. Die Folgenummer MUSS
// höher sein als die der zuletzt verteilten Liste.
func (i *Issuer) SignCRL(seq uint64, revoked []Revocation, now time.Time) (CRL, error) {
	l := CRL{V: version, NetworkID: i.network, Issuer: IssuerRoot, Seq: seq, IssuedAt: now.UTC().Truncate(time.Second), Revoked: revoked}
	if i.cert != nil {
		l.Issuer, l.IssuerCert = i.key.ID.String(), i.cert
	}
	sig, err := sign(i.key, l.signingBytes())
	if err != nil {
		return CRL{}, err
	}
	l.Sig = sig
	return l, nil
}

// Network prüft Ketten und hält die Sperrlisten eines Netzes.
type Network struct {
	id   string
	root crypto.PubKey

	mu    sync.RWMutex
	lists map[string]CRL // je Aussteller die neueste Liste
}

// NewNetwork legt die Prüfung für ein Netz an. Stimmt die Kennung nicht zum
// Schlüssel, ist die Konfiguration oder der Einladungslink falsch.
func NewNetwork(networkID string, rootPub []byte) (*Network, error) {
	pub, err := crypto.UnmarshalEd25519PublicKey(rootPub)
	if err != nil {
		return nil, fmt.Errorf("wurzelschlüssel: %w", err)
	}
	if got := NetworkID(rootPub); got != networkID {
		return nil, fmt.Errorf("%w: kennung %s passt nicht zum wurzelschlüssel (%s)", ErrNetwork, networkID, got)
	}
	return &Network{id: networkID, root: pub, lists: make(map[string]CRL)}, nil
}

// ID ist die Netzkennung.
func (n *Network) ID() string { return n.id }

// checkCert prüft Form, Netz und Laufzeit eines einzelnen Zertifikats.
func (n *Network) checkCert(c Cert, now time.Time) error {
	if c.V != version {
		return fmt.Errorf("%w: version %d", ErrChain, c.V)
	}
	if c.NetworkID != n.id {
		return ErrNetwork
	}
	if c.Serial == "" {
		return fmt.Errorf("%w: seriennummer fehlt", ErrChain)
	}
	if _, err := peer.Decode(c.Subject); err != nil {
		return fmt.Errorf("%w: subject: %v", ErrChain, err)
	}
	if !c.ExpiresAt.After(c.IssuedAt) {
		return fmt.Errorf("%w: laufzeit", ErrChain)
	}
	if now.Add(ClockSkew).Before(c.IssuedAt) {
		return ErrNotYet
	}
	if now.Add(-ClockSkew).After(c.ExpiresAt) {
		return ErrExpired
	}
	return nil
}

// checkIssuerCert: von der Wurzel signiert und Rolle issuer.
func (n *Network) checkIssuerCert(c Cert, now time.Time) error {
	if err := n.checkCert(c, now); err != nil {
		return err
	}
	if c.Role != RoleIssuer || c.Issuer != IssuerRoot {
		return fmt.Errorf("%w: kein von der wurzel ernannter aussteller", ErrChain)
	}
	if err := verifySig(n.root, c.signingBytes(), c.Sig); err != nil {
		return fmt.Errorf("%w: %v", ErrChain, err)
	}
	return nil
}

// Admit prüft die Kette eines Peers und liefert sein Zertifikat. subject ist
// die durch die Verbindung nachgewiesene Peer-ID der Gegenstelle.
func (n *Network) Admit(chain []Cert, subject string, now time.Time) (Cert, error) {
	if len(chain) == 0 || len(chain) > 2 {
		return Cert{}, fmt.Errorf("%w: %d stufen", ErrChain, len(chain))
	}
	leaf := chain[len(chain)-1]
	if err := n.checkCert(leaf, now); err != nil {
		return Cert{}, err
	}
	if leaf.Subject != subject {
		return Cert{}, ErrSubject
	}
	switch leaf.Role {
	case RoleIssuer, RoleMember, RoleHelper:
	default:
		return Cert{}, fmt.Errorf("%w: rolle %q", ErrChain, leaf.Role)
	}
	if len(chain) == 1 {
		if leaf.Issuer != IssuerRoot {
			return Cert{}, fmt.Errorf("%w: aussteller-zertifikat fehlt", ErrChain)
		}
		if err := verifySig(n.root, leaf.signingBytes(), leaf.Sig); err != nil {
			return Cert{}, fmt.Errorf("%w: %v", ErrChain, err)
		}
	} else {
		issuer := chain[0]
		if err := n.checkIssuerCert(issuer, now); err != nil {
			return Cert{}, err
		}
		if leaf.Role == RoleIssuer {
			return Cert{}, fmt.Errorf("%w: aussteller ernennt nur die wurzel", ErrChain)
		}
		if leaf.Issuer != issuer.Subject {
			return Cert{}, fmt.Errorf("%w: aussteller passt nicht", ErrChain)
		}
		if leaf.ExpiresAt.After(issuer.ExpiresAt) {
			return Cert{}, fmt.Errorf("%w: gilt länger als der aussteller", ErrChain)
		}
		pub, err := issuerKey(issuer.Subject)
		if err != nil {
			return Cert{}, fmt.Errorf("%w: %v", ErrChain, err)
		}
		if err := verifySig(pub, leaf.signingBytes(), leaf.Sig); err != nil {
			return Cert{}, fmt.Errorf("%w: %v", ErrChain, err)
		}
	}
	n.mu.RLock()
	defer n.mu.RUnlock()
	for _, c := range chain {
		if n.revokedLocked(c) {
			return Cert{}, ErrRevoked
		}
	}
	return leaf, nil
}

// revokedLocked: Aussteller kann nur die Wurzel sperren, sonst könnte ein
// übernommener Aussteller die Verwaltung lahmlegen. Listen eines widerrufenen
// Ausstellers gelten nicht mehr, damit seine Sperren mit ihm enden.
func (n *Network) revokedLocked(c Cert) bool {
	if c.Role == RoleIssuer {
		return matches(n.lists[IssuerRoot], c)
	}
	for issuer, l := range n.lists {
		if issuer != IssuerRoot && matches(n.lists[IssuerRoot], Cert{Subject: issuer}) {
			continue
		}
		if matches(l, c) {
			return true
		}
	}
	return false
}

func matches(l CRL, c Cert) bool {
	return slices.ContainsFunc(l.Revoked, func(r Revocation) bool {
		return r.Subject == c.Subject || r.Serial != "" && r.Serial == c.Serial
	})
}

// AddCRL prüft eine Sperrliste und übernimmt sie, wenn sie neuer ist. Listen
// mit gleicher oder niedrigerer Folgenummer ergeben ErrStaleCRL.
func (n *Network) AddCRL(l CRL, now time.Time) error {
	if l.V != version {
		return fmt.Errorf("sperrliste: version %d", l.V)
	}
	if l.NetworkID != n.id {
		return ErrNetwork
	}
	pub := n.root
	if l.Issuer != IssuerRoot {
		if l.IssuerCert == nil || l.IssuerCert.Subject != l.Issuer {
			return fmt.Errorf("%w: sperrliste ohne aussteller-zertifikat", ErrChain)
		}
		if err := n.checkIssuerCert(*l.IssuerCert, now); err != nil {
			return err
		}
		var err error
		if pub, err = issuerKey(l.Issuer); err != nil {
			return fmt.Errorf("%w: %v", ErrChain, err)
		}
	}
	if err := verifySig(pub, l.signingBytes(), l.Sig); err != nil {
		return fmt.Errorf("sperrliste: %w", err)
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if old, ok := n.lists[l.Issuer]; ok && l.Seq <= old.Seq {
		return ErrStaleCRL
	}
	n.lists[l.Issuer] = l
	return nil
}

// Seqs nennt je Aussteller die Folgenummer der gespeicherten Liste, für den
// Abgleich beim Zulassen (§ 5).
func (n *Network) Seqs() map[string]uint64 {
	n.mu.RLock()
	defer n.mu.RUnlock()
	out := make(map[string]uint64, len(n.lists))
	for issuer, l := range n.lists {
		out[issuer] = l.Seq
	}
	return out
}

// CRLs liefert die gespeicherten Listen zum Weitergeben und Sichern.
func (n *Network) CRLs() []CRL {
	n.mu.RLock()
	defer n.mu.RUnlock()
	out := make([]CRL, 0, len(n.lists))
	for _, l := range n.lists {
		out = append(out, l)
	}
	return out
}
