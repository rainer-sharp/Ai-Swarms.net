// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Rainer Sestig. Kommerzielle Lizenz: siehe COMMERCIAL.md.

package membership

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/rainer-sharp/Ai-Swarms.net/internal/identity"
)

var t0 = time.Date(2026, 11, 8, 10, 0, 0, 0, time.UTC)

type fixture struct {
	root, issuerKey, node *identity.Identity
	rootIss, iss          *Issuer
	net                   *Network
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	f := fixture{}
	f.root, _ = identity.New()
	f.issuerKey, _ = identity.New()
	f.node, _ = identity.New()
	var err error
	if f.rootIss, err = NewRootIssuer(f.root); err != nil {
		t.Fatal(err)
	}
	ic, err := f.rootIss.Issue(f.issuerKey.ID.String(), RoleIssuer, "", IssuerTTL, t0)
	if err != nil {
		t.Fatal(err)
	}
	if f.iss, err = NewIssuer(f.issuerKey, ic[0]); err != nil {
		t.Fatal(err)
	}
	pub, _ := RootKey(f.root)
	if f.net, err = NewNetwork(NetworkID(pub), pub); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestValidChains(t *testing.T) {
	f := newFixture(t)
	node := f.node.ID.String()
	for _, c := range []struct {
		name   string
		iss    *Issuer
		role   string
		stufen int
	}{
		{"wurzel an mitglied", f.rootIss, RoleMember, 1},
		{"wurzel an helfer", f.rootIss, RoleHelper, 1},
		{"wurzel an aussteller", f.rootIss, RoleIssuer, 1},
		{"aussteller an mitglied", f.iss, RoleMember, 2},
		{"aussteller an helfer", f.iss, RoleHelper, 2},
	} {
		t.Run(c.name, func(t *testing.T) {
			chain, err := c.iss.Issue(node, c.role, "gerät", NodeTTL, t0)
			if err != nil || len(chain) != c.stufen {
				t.Fatalf("%v, %d stufen", err, len(chain))
			}
			got, err := f.net.Admit(chain, node, t0.Add(time.Hour))
			if err != nil || got.Role != c.role || got.Subject != node {
				t.Fatalf("%+v, %v", got, err)
			}
			// Übertragung als JSON ändert nichts an der Gültigkeit.
			b, _ := json.Marshal(chain)
			var back []Cert
			if err := json.Unmarshal(b, &back); err != nil {
				t.Fatal(err)
			}
			if _, err := f.net.Admit(back, node, t0.Add(time.Hour)); err != nil {
				t.Fatalf("nach json: %v", err)
			}
		})
	}
}

func TestRejectedChains(t *testing.T) {
	f := newFixture(t)
	node := f.node.ID.String()
	other, _ := identity.New()
	otherRoot, _ := identity.New()
	otherIss, _ := NewRootIssuer(otherRoot)
	now := t0.Add(time.Hour)
	valid := func() []Cert {
		c, err := f.iss.Issue(node, RoleMember, "", NodeTTL, t0)
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	// resign signiert ein verändertes Zertifikat mit dem Schlüssel key neu.
	resign := func(c Cert, key *identity.Identity) Cert {
		c.Sig, _ = sign(key, c.signingBytes())
		return c
	}

	// Ein Mitglied gibt sich als Aussteller aus.
	memberChain, _ := f.rootIss.Issue(other.ID.String(), RoleMember, "", NodeTTL, t0)
	forgedByMember := valid()[1]
	forgedByMember.Issuer = other.ID.String()
	forgedByMember = resign(forgedByMember, other)
	// Ein Aussteller ernennt einen Aussteller.
	subIssuer := valid()[1]
	subIssuer.Role = RoleIssuer
	subIssuer = resign(subIssuer, f.issuerKey)
	// Ein Aussteller stellt länger aus, als er selbst gilt.
	tooLong := valid()[1]
	tooLong.ExpiresAt = t0.Add(10 * IssuerTTL)
	tooLong = resign(tooLong, f.issuerKey)
	// Zertifikat, das von seinem eigenen Inhaber signiert ist.
	selfSigned := valid()[1]
	selfSigned.Issuer = IssuerRoot
	selfSigned = resign(selfSigned, f.node)
	tampered := valid()
	tampered[1].Role = RoleIssuer
	renamed := valid()
	renamed[1].ExpiresAt = renamed[1].ExpiresAt.Add(time.Hour)
	foreign, _ := otherIss.Issue(node, RoleMember, "", NodeTTL, t0)
	badVersion := valid()[1]
	badVersion.V = 2
	badVersion = resign(badVersion, f.issuerKey)
	noIssuerCert := valid()[1:]
	issuerChain := valid()

	for _, c := range []struct {
		name    string
		chain   []Cert
		subject string
		now     time.Time
		want    error
	}{
		{"leer", nil, node, now, ErrChain},
		{"drei stufen", []Cert{issuerChain[0], issuerChain[0], issuerChain[1]}, node, now, ErrChain},
		{"anderer peer", valid(), other.ID.String(), now, ErrSubject},
		{"anderes netz", foreign, node, now, ErrNetwork},
		{"inhalt verändert", tampered, node, now, ErrChain},
		{"laufzeit verlängert", renamed, node, now, ErrChain},
		{"mitglied als aussteller", []Cert{memberChain[0], forgedByMember}, node, now, ErrChain},
		{"aussteller ernennt aussteller", []Cert{issuerChain[0], subIssuer}, node, now, ErrChain},
		{"länger als der aussteller", []Cert{issuerChain[0], tooLong}, node, now, ErrChain},
		{"selbst signiert", []Cert{selfSigned}, node, now, ErrChain},
		{"ohne aussteller-zertifikat", noIssuerCert, node, now, ErrChain},
		{"falsche version", []Cert{issuerChain[0], badVersion}, node, now, ErrChain},
		{"abgelaufen", valid(), node, t0.Add(NodeTTL + ClockSkew + time.Second), ErrExpired},
		{"noch nicht gültig", valid(), node, t0.Add(-ClockSkew - time.Second), ErrNotYet},
	} {
		t.Run(c.name, func(t *testing.T) {
			if _, err := f.net.Admit(c.chain, c.subject, c.now); !errors.Is(err, c.want) {
				t.Fatalf("fehler %v, erwartet %v", err, c.want)
			}
		})
	}
	// Innerhalb der Toleranz der Uhren gilt das Zertifikat noch.
	for _, at := range []time.Time{t0.Add(NodeTTL + ClockSkew), t0.Add(-ClockSkew)} {
		if _, err := f.net.Admit(valid(), node, at); err != nil {
			t.Fatalf("toleranz %v: %v", at, err)
		}
	}
}

func TestIssueRules(t *testing.T) {
	f := newFixture(t)
	node := f.node.ID.String()
	if _, err := f.iss.Issue(node, RoleIssuer, "", NodeTTL, t0); !errors.Is(err, ErrChain) {
		t.Fatalf("aussteller ernennt aussteller: %v", err)
	}
	if _, err := f.rootIss.Issue(node, "admin", "", NodeTTL, t0); err == nil {
		t.Fatal("unbekannte rolle ausgestellt")
	}
	if _, err := f.rootIss.Issue("keine-peer-id", RoleMember, "", NodeTTL, t0); err == nil {
		t.Fatal("ungültige peer-id ausgestellt")
	}
	c, err := f.iss.Issue(node, RoleMember, "", 10*IssuerTTL, t0)
	if err != nil || !c[1].ExpiresAt.Equal(c[0].ExpiresAt) {
		t.Fatalf("laufzeit nicht begrenzt: %+v %v", c, err)
	}
	if _, err := NewIssuer(f.node, c[0]); !errors.Is(err, ErrChain) {
		t.Fatalf("fremdes aussteller-zertifikat: %v", err)
	}
}

func TestNetworkIDMustMatchRoot(t *testing.T) {
	f := newFixture(t)
	pub, _ := RootKey(f.root)
	other, _ := identity.New()
	otherPub, _ := RootKey(other)
	if _, err := NewNetwork(NetworkID(otherPub), pub); !errors.Is(err, ErrNetwork) {
		t.Fatalf("fehler %v", err)
	}
	back, err := DecodeRoot(EncodeRoot(pub))
	if err != nil || string(back) != string(pub) {
		t.Fatalf("textform: %v", err)
	}
	if _, err := DecodeRoot("kurz"); err == nil {
		t.Fatal("zu kurzer schlüssel akzeptiert")
	}
}

func TestRevocation(t *testing.T) {
	now := t0.Add(time.Hour)
	node := func(f fixture) string { return f.node.ID.String() }

	t.Run("nach peer-id, auch ein zweites zertifikat", func(t *testing.T) {
		f := newFixture(t)
		first, _ := f.iss.Issue(node(f), RoleMember, "", NodeTTL, t0)
		second, _ := f.rootIss.Issue(node(f), RoleMember, "", NodeTTL, t0)
		l, _ := f.iss.SignCRL(1, []Revocation{{Subject: node(f), RevokedAt: now}}, now)
		if err := f.net.AddCRL(l, now); err != nil {
			t.Fatal(err)
		}
		for _, chain := range [][]Cert{first, second} {
			if _, err := f.net.Admit(chain, node(f), now); !errors.Is(err, ErrRevoked) {
				t.Fatalf("fehler %v", err)
			}
		}
	})

	t.Run("nach seriennummer", func(t *testing.T) {
		f := newFixture(t)
		chain, _ := f.iss.Issue(node(f), RoleMember, "", NodeTTL, t0)
		l, _ := f.iss.SignCRL(1, []Revocation{{Serial: chain[1].Serial, Subject: "anderer", RevokedAt: now}}, now)
		_ = f.net.AddCRL(l, now)
		if _, err := f.net.Admit(chain, node(f), now); !errors.Is(err, ErrRevoked) {
			t.Fatalf("fehler %v", err)
		}
	})

	t.Run("andere mitglieder bleiben zugelassen", func(t *testing.T) {
		f := newFixture(t)
		other, _ := identity.New()
		chain, _ := f.iss.Issue(other.ID.String(), RoleMember, "", NodeTTL, t0)
		l, _ := f.iss.SignCRL(1, []Revocation{{Subject: node(f), RevokedAt: now}}, now)
		_ = f.net.AddCRL(l, now)
		if _, err := f.net.Admit(chain, other.ID.String(), now); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("wurzel sperrt aussteller und damit seine zertifikate", func(t *testing.T) {
		f := newFixture(t)
		chain, _ := f.iss.Issue(node(f), RoleMember, "", NodeTTL, t0)
		l, _ := f.rootIss.SignCRL(1, []Revocation{{Subject: f.issuerKey.ID.String(), RevokedAt: now}}, now)
		_ = f.net.AddCRL(l, now)
		if _, err := f.net.Admit(chain, node(f), now); !errors.Is(err, ErrRevoked) {
			t.Fatalf("fehler %v", err)
		}
	})

	t.Run("aussteller kann keinen aussteller sperren", func(t *testing.T) {
		f := newFixture(t)
		k2, _ := identity.New()
		ic2, _ := f.rootIss.Issue(k2.ID.String(), RoleIssuer, "", IssuerTTL, t0)
		iss2, _ := NewIssuer(k2, ic2[0])
		chain, _ := iss2.Issue(node(f), RoleMember, "", NodeTTL, t0)
		l, _ := f.iss.SignCRL(1, []Revocation{{Subject: k2.ID.String(), RevokedAt: now}}, now)
		if err := f.net.AddCRL(l, now); err != nil {
			t.Fatal(err)
		}
		if _, err := f.net.Admit(chain, node(f), now); err != nil {
			t.Fatalf("zweiter aussteller lahmgelegt: %v", err)
		}
	})

	t.Run("sperren eines widerrufenen ausstellers enden mit ihm", func(t *testing.T) {
		f := newFixture(t)
		k2, _ := identity.New()
		ic2, _ := f.rootIss.Issue(k2.ID.String(), RoleIssuer, "", IssuerTTL, t0)
		iss2, _ := NewIssuer(k2, ic2[0])
		chain, _ := iss2.Issue(node(f), RoleMember, "", NodeTTL, t0)
		l, _ := f.iss.SignCRL(1, []Revocation{{Subject: node(f), RevokedAt: now}}, now)
		_ = f.net.AddCRL(l, now)
		if _, err := f.net.Admit(chain, node(f), now); !errors.Is(err, ErrRevoked) {
			t.Fatalf("vor dem widerruf: %v", err)
		}
		rl, _ := f.rootIss.SignCRL(1, []Revocation{{Subject: f.issuerKey.ID.String(), RevokedAt: now}}, now)
		_ = f.net.AddCRL(rl, now)
		if _, err := f.net.Admit(chain, node(f), now); err != nil {
			t.Fatalf("nach dem widerruf: %v", err)
		}
	})
}

func TestCRLChecks(t *testing.T) {
	f := newFixture(t)
	now := t0.Add(time.Hour)
	stranger, _ := identity.New()
	otherRoot, _ := identity.New()
	otherIss, _ := NewRootIssuer(otherRoot)

	l1, _ := f.iss.SignCRL(5, []Revocation{{Subject: "a", RevokedAt: now}}, now)
	if err := f.net.AddCRL(l1, now); err != nil {
		t.Fatal(err)
	}
	if got := f.net.Seqs()[f.issuerKey.ID.String()]; got != 5 {
		t.Fatalf("folgenummer %d", got)
	}
	// Rückspielen: gleiche und niedrigere Folgenummer.
	for _, seq := range []uint64{5, 4} {
		old, _ := f.iss.SignCRL(seq, nil, now)
		if err := f.net.AddCRL(old, now); !errors.Is(err, ErrStaleCRL) {
			t.Fatalf("folgenummer %d: %v", seq, err)
		}
	}

	tampered := l1
	tampered.Seq, tampered.Revoked = 6, nil
	if err := f.net.AddCRL(tampered, now); err == nil {
		t.Fatal("veränderte liste akzeptiert")
	}
	foreign, _ := otherIss.SignCRL(1, nil, now)
	if err := f.net.AddCRL(foreign, now); !errors.Is(err, ErrNetwork) {
		t.Fatalf("fremdes netz: %v", err)
	}
	// Ein Fremder ohne Aussteller-Zertifikat, auch mit erfundener Kennung.
	forged := CRL{V: version, NetworkID: f.net.ID(), Issuer: stranger.ID.String(), Seq: 1, IssuedAt: now}
	forged.Sig, _ = sign(stranger, forged.signingBytes())
	if err := f.net.AddCRL(forged, now); !errors.Is(err, ErrChain) {
		t.Fatalf("ohne zertifikat: %v", err)
	}
	// Ein Mitglied legt sein eigenes Zertifikat bei.
	mc, _ := f.rootIss.Issue(stranger.ID.String(), RoleMember, "", NodeTTL, t0)
	forged.IssuerCert = &mc[0]
	if err := f.net.AddCRL(forged, now); !errors.Is(err, ErrChain) {
		t.Fatalf("mitglied als aussteller: %v", err)
	}
	// Ein Fremder gibt sich als Wurzel aus.
	asRoot := CRL{V: version, NetworkID: f.net.ID(), Issuer: IssuerRoot, Seq: 9, IssuedAt: now}
	asRoot.Sig, _ = sign(stranger, asRoot.signingBytes())
	if err := f.net.AddCRL(asRoot, now); err == nil {
		t.Fatal("gefälschte wurzel-liste akzeptiert")
	}
	// Liste übersteht JSON, auch ohne Einträge.
	empty, _ := f.rootIss.SignCRL(1, nil, now)
	b, _ := json.Marshal(empty)
	var back CRL
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if err := f.net.AddCRL(back, now); err != nil {
		t.Fatalf("leere liste nach json: %v", err)
	}
}

// Testvektoren für andere Implementierungen: feste Schlüssel, feste Bytes.
type vectors struct {
	RootSeed   string `json:"root_seed_hex"`
	RootPub    string `json:"root_pub"`
	NetworkID  string `json:"network_id"`
	Node       string `json:"node_peer_id"`
	CertSigned string `json:"cert_signed_bytes"`
	CertSig    string `json:"cert_sig"`
	CRLSigned  string `json:"crl_signed_bytes"`
	CRLSig     string `json:"crl_sig"`
	CertJSON   string `json:"cert_json"`
}

func fixedKey(t *testing.T, fill byte) *identity.Identity {
	t.Helper()
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = fill
	}
	priv, err := crypto.UnmarshalEd25519PrivateKey(ed25519.NewKeyFromSeed(seed))
	if err != nil {
		t.Fatal(err)
	}
	id, err := peer.IDFromPrivateKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	return &identity.Identity{Priv: priv, ID: id}
}

func TestVectors(t *testing.T) {
	root, node := fixedKey(t, 0x01), fixedKey(t, 0x02)
	pub, _ := RootKey(root)
	id := NetworkID(pub)
	c := Cert{V: 1, NetworkID: id, Serial: "AAAAAAAAAAAAAAAAAAAAAA", Subject: node.ID.String(), Role: RoleMember,
		Name: "gerät <1> & co", IssuedAt: t0, ExpiresAt: t0.Add(NodeTTL), Issuer: IssuerRoot}
	c.Sig, _ = sign(root, c.signingBytes())
	l := CRL{V: 1, NetworkID: id, Issuer: IssuerRoot, Seq: 1, IssuedAt: t0,
		Revoked: []Revocation{{Serial: c.Serial, Subject: c.Subject, RevokedAt: t0, Reason: "gerät verloren"}}}
	l.Sig, _ = sign(root, l.signingBytes())
	certJSON, _ := json.Marshal(c)
	got := vectors{
		RootSeed: "0101010101010101010101010101010101010101010101010101010101010101",
		RootPub:  EncodeRoot(pub), NetworkID: id, Node: node.ID.String(),
		CertSigned: string(c.signingBytes()), CertSig: c.Sig,
		CRLSigned: string(l.signingBytes()), CRLSig: l.Sig, CertJSON: string(certJSON),
	}
	const path = "testdata/vectors.json"
	if os.Getenv("MEMBERSHIP_WRITE_VECTORS") != "" {
		b, _ := json.MarshalIndent(got, "", "  ")
		if err := os.WriteFile(path, append(b, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var want vectors
	if err := json.Unmarshal(data, &want); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("format geändert:\n%+v\n%+v", got, want)
	}
	// Das Zertifikat aus den Vektoren muss gegen die Wurzel gültig sein.
	raw, _ := base64.RawURLEncoding.DecodeString(want.RootPub)
	n, err := NewNetwork(want.NetworkID, raw)
	if err != nil {
		t.Fatal(err)
	}
	var parsed Cert
	if err := json.Unmarshal([]byte(want.CertJSON), &parsed); err != nil {
		t.Fatal(err)
	}
	if _, err := n.Admit([]Cert{parsed}, want.Node, t0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
}

func TestInviteTokens(t *testing.T) {
	f := newFixture(t)
	dir := t.TempDir()
	pub, _ := RootKey(f.root)
	if err := f.root.Create(RootKeyFile(dir, NetworkID(pub))); err != nil {
		t.Fatal(err)
	}
	open := func() *Admin {
		a, err := OpenAdmin(dir, NetworkID(pub), "", t0)
		if err != nil {
			t.Fatal(err)
		}
		return a
	}
	a := open()
	token, err := a.Registry.NewInvite(RoleHelper, "server", 0, t0)
	if err != nil {
		t.Fatal(err)
	}
	if raw, _ := os.ReadFile(RegistryFile(dir, a.Network)); string(raw) == "" || json.Valid(raw) == false || contains(raw, token) {
		t.Fatal("token steht im klartext im buch")
	}
	if _, err := a.Registry.NewInvite(RoleIssuer, "", 0, t0); err == nil {
		t.Fatal("einladung für einen aussteller")
	}
	first, second := f.node.ID.String(), f.issuerKey.ID.String()
	if _, err := open().Redeem("falsch", first, t0); !errors.Is(err, ErrInviteInvalid) {
		t.Fatalf("falsches token: %v", err)
	}
	if _, err := open().Redeem(token, first, t0.Add(MaxInviteTTL+time.Second)); !errors.Is(err, ErrInviteExpired) {
		t.Fatalf("abgelaufen: %v", err)
	}
	chain, err := open().Redeem(token, first, t0.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if got, err := f.net.Admit(chain, first, t0.Add(time.Hour)); err != nil || got.Role != RoleHelper || got.Name != "server" {
		t.Fatalf("%+v %v", got, err)
	}
	// Einmalig, auch nach einem Neustart: Der Verbrauch steht im Buch.
	if _, err := open().Redeem(token, second, t0.Add(time.Hour)); !errors.Is(err, ErrInviteUsed) {
		t.Fatalf("zweite identität: %v", err)
	}
	if _, err := open().Redeem(token, first, t0.Add(time.Hour)); err != nil {
		t.Fatalf("dieselbe identität nach verlorener antwort: %v", err)
	}
	// Ein gesperrter Knoten kommt mit einer Einladung nicht zurück.
	a = open()
	other, _ := identity.New()
	t2, _ := a.Registry.NewInvite(RoleMember, "", 0, t0)
	if _, err := a.Registry.Revoke(a.Iss, other.ID.String(), "", t0); err != nil {
		t.Fatal(err)
	}
	if _, err := open().Redeem(t2, other.ID.String(), t0); !errors.Is(err, ErrRevoked) {
		t.Fatalf("gesperrt: %v", err)
	}
}

func contains(b []byte, s string) bool { return bytes.Contains(b, []byte(s)) }

func TestLongTTLIsCappedAndOldEntriesLeaveTheList(t *testing.T) {
	f := newFixture(t)
	c, err := f.rootIss.Issue(f.node.ID.String(), RoleMember, "", 100*IssuerTTL, t0)
	if err != nil || !c[0].ExpiresAt.Equal(t0.Add(IssuerTTL)) {
		t.Fatalf("laufzeit %+v %v", c, err)
	}
	r, err := OpenRegistry(RegistryFile(t.TempDir(), "m-x"))
	if err != nil {
		t.Fatal(err)
	}
	old, _ := identity.New()
	fresh, _ := identity.New()
	if _, err := r.Revoke(f.rootIss, old.ID.String(), "", t0); err != nil {
		t.Fatal(err)
	}
	l, err := r.Revoke(f.rootIss, fresh.ID.String(), "", t0.Add(IssuerTTL+time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Revoked) != 1 || l.Revoked[0].Subject != fresh.ID.String() || !r.IsRevoked(old.ID.String()) {
		t.Fatalf("liste %+v, buch %+v", l.Revoked, r.Revoked)
	}
	// Die Liste bleibt prüfbar, auch ohne den entfernten Eintrag.
	if err := f.net.AddCRL(l, t0.Add(IssuerTTL+time.Hour)); err != nil {
		t.Fatal(err)
	}
}

func TestCRLStoreRoundtrip(t *testing.T) {
	f := newFixture(t)
	path := filepath.Join(t.TempDir(), "store.json")
	if err := f.net.LoadCRLs(path, t0); err != nil {
		t.Fatalf("fehlende datei: %v", err)
	}
	l, _ := f.iss.SignCRL(3, []Revocation{{Subject: f.node.ID.String(), RevokedAt: t0}}, t0)
	if err := f.net.AddCRL(l, t0); err != nil {
		t.Fatal(err)
	}
	if err := f.net.SaveCRLs(path); err != nil {
		t.Fatal(err)
	}
	pub, _ := RootKey(f.root)
	n2, _ := NewNetwork(NetworkID(pub), pub)
	if err := n2.LoadCRLs(path, t0); err != nil {
		t.Fatal(err)
	}
	chain, _ := f.iss.Issue(f.node.ID.String(), RoleMember, "", NodeTTL, t0)
	if _, err := n2.Admit(chain, f.node.ID.String(), t0); !errors.Is(err, ErrRevoked) {
		t.Fatalf("nach dem laden: %v", err)
	}
	// Eine manipulierte Datei fällt auf, statt still weniger zu sperren.
	raw, _ := os.ReadFile(path)
	if err := os.WriteFile(path, bytes.Replace(raw, []byte(`"seq": 3`), []byte(`"seq": 2`), 1), 0o600); err != nil {
		t.Fatal(err)
	}
	n3, _ := NewNetwork(NetworkID(pub), pub)
	if err := n3.LoadCRLs(path, t0); err == nil {
		t.Fatal("manipulierte datei akzeptiert")
	}
}
