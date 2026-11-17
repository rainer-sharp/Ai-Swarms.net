// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Rainer Sestig. Kommerzielle Lizenz: siehe COMMERCIAL.md.

package membership

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/rainer-sharp/Ai-Swarms.net/internal/identity"
)

// MaxInviteTTL ist die längste Laufzeit einer Einladung (§ 4).
const MaxInviteTTL = 24 * time.Hour

var (
	ErrInviteUsed    = errors.New("einladung bereits benutzt")
	ErrInviteExpired = errors.New("einladung abgelaufen")
	ErrInviteInvalid = errors.New("einladung ungültig")
)

// Dateien eines Mitgliedsnetzes im Datenverzeichnis (§ 10).
func RootKeyFile(dir, network string) string {
	return filepath.Join(dir, "root-"+network+".key")
}
func NodeKeyFile(dir, network string) string {
	return filepath.Join(dir, "identity-"+network+".key")
}
func CertFile(dir, network string) string {
	return filepath.Join(dir, "member-"+network+".cert")
}
func RegistryFile(dir, network string) string {
	return filepath.Join(dir, "issuer-"+network+".json")
}
func CRLFile(dir, network string) string {
	return filepath.Join(dir, "crl-"+network+".json")
}

// CRLInbox ist das Muster für Listen, die ein laufender Knoten übernimmt: die
// eigene aus `member revoke` und eingespielte der Wurzel.
func CRLInbox(dir, network string) string {
	return filepath.Join(dir, "crl-"+network+"*.json")
}

// CRLImportFile ist der Ablageort einer eingespielten Liste, je Aussteller eine.
func CRLImportFile(dir, network, issuer string) string {
	h := sha256.Sum256([]byte(issuer))
	return filepath.Join(dir, "crl-"+network+"-"+hex.EncodeToString(h[:4])+".json")
}

// CRLStoreFile hält alle bekannten Sperrlisten über Neustarts.
func CRLStoreFile(dir, network string) string {
	return filepath.Join(dir, "crlstore-"+network+".json")
}

// Bundle ist die Zertifikatsdatei eines Knotens: Kette und Wurzelschlüssel.
// Der Wurzelschlüssel darin beglaubigt nichts; wer eine Datei übernimmt,
// vergleicht ihn mit dem aus sicherer Quelle.
type Bundle struct {
	V     int    `json:"v"`
	Root  string `json:"root"`
	Chain []Cert `json:"chain"`
}

// ReadBundle liest eine Zertifikatsdatei.
func ReadBundle(path string) (Bundle, error) {
	var b Bundle
	data, err := os.ReadFile(path)
	if err != nil {
		return b, err
	}
	if err := json.Unmarshal(data, &b); err != nil {
		return b, fmt.Errorf("%s: %w", path, err)
	}
	if b.V != version || len(b.Chain) == 0 {
		return b, fmt.Errorf("%s: keine zertifikatsdatei", path)
	}
	return b, nil
}

// Leaf ist das Zertifikat des Knotens selbst.
func (b Bundle) Leaf() Cert { return b.Chain[len(b.Chain)-1] }

// Network baut aus dem Wurzelschlüssel der Datei die Prüfung für ihr Netz.
func (b Bundle) Network() (*Network, error) {
	root, err := DecodeRoot(b.Root)
	if err != nil {
		return nil, err
	}
	return NewNetwork(b.Leaf().NetworkID, root)
}

// Write speichert die Datei; path wird atomar ersetzt.
func (b Bundle) Write(path string) error { return writeJSON(path, b) }

func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path) // CreateTemp legt mit Rechten 0600 an
}

// Invite ist eine offene oder benutzte Einladung. Vom Token liegt nur der Hash
// im Buch, damit eine gestohlene Sicherung keine Plätze öffnet.
type Invite struct {
	Hash      string    `json:"hash"`
	Role      string    `json:"role"`
	Name      string    `json:"name,omitempty"`
	ExpiresAt time.Time `json:"expires_at"`
	UsedBy    string    `json:"used_by,omitempty"`
}

// Registry ist das Buch eines Ausstellers: was er ausgestellt und gesperrt hat
// und die Folgenummer seiner Sperrliste. Es liegt neben seinem Schlüssel.
type Registry struct {
	Issued  []Cert       `json:"issued"`
	Seq     uint64       `json:"seq"`
	Revoked []Revocation `json:"revoked"`
	Invites []Invite     `json:"invites,omitempty"`

	path string
}

// OpenRegistry liest das Buch; fehlt die Datei, beginnt es leer.
func OpenRegistry(path string) (*Registry, error) {
	r := &Registry{path: path}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return r, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, r); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return r, nil
}

func (r *Registry) save() error { return writeJSON(r.path, r) }

// IsRevoked: Ist der Peer in diesem Buch gesperrt?
func (r *Registry) IsRevoked(subject string) bool {
	return slices.ContainsFunc(r.Revoked, func(x Revocation) bool { return x.Subject == subject })
}

// AddIssued trägt ein ausgestelltes Zertifikat ein.
func (r *Registry) AddIssued(c Cert) error {
	r.Issued = append(r.Issued, c)
	return r.save()
}

// Resolve findet den Peer zu einer Peer-ID oder einem Namen im Buch. Eine
// Peer-ID muss nicht im Buch stehen, denn die Wurzel sperrt auch Knoten, die
// ein Aussteller aufgenommen hat. Ein Name, der auf mehrere Peers passt, ist ein
// Fehler: Es soll niemand versehentlich den falschen sperren.
func (r *Registry) Resolve(target string) (string, error) {
	if _, err := peer.Decode(target); err == nil {
		return target, nil
	}
	var subjects []string
	for _, c := range r.Issued {
		if c.Name == target && !slices.Contains(subjects, c.Subject) {
			subjects = append(subjects, c.Subject)
		}
	}
	switch len(subjects) {
	case 0:
		return "", fmt.Errorf("%q steht nicht im buch dieses ausstellers", target)
	case 1:
		return subjects[0], nil
	}
	return "", fmt.Errorf("name %q passt auf %d peers; bitte die peer-id angeben", target, len(subjects))
}

// Revoke sperrt einen Peer und signiert die neue Sperrliste. Das Buch wird
// gespeichert, bevor die Liste herausgeht, damit keine Folgenummer doppelt
// vorkommt.
func (r *Registry) Revoke(iss *Issuer, subject, reason string, now time.Time) (CRL, error) {
	if r.IsRevoked(subject) {
		return CRL{}, fmt.Errorf("%s ist schon gesperrt", subject)
	}
	e := Revocation{Subject: subject, RevokedAt: now.UTC().Truncate(time.Second), Reason: reason}
	for _, c := range r.Issued {
		if c.Subject == subject {
			e.Serial = c.Serial // die letzte Ausstellung; gesperrt wird ohnehin die Peer-ID
		}
	}
	r.Revoked = append(r.Revoked, e)
	r.Seq++
	if err := r.save(); err != nil {
		return CRL{}, err
	}
	// Das Buch behält alles, damit nie neu ausgestellt wird. In die Liste gehört
	// nur, was noch gelten könnte: Kein Zertifikat läuft länger als IssuerTTL.
	active := slices.DeleteFunc(slices.Clone(r.Revoked), func(x Revocation) bool {
		return now.After(x.RevokedAt.Add(IssuerTTL))
	})
	return iss.SignCRL(r.Seq, active, now)
}

// WriteCRL speichert eine Sperrliste zum Weitergeben.
func WriteCRL(path string, l CRL) error { return writeJSON(path, l) }

func tokenHash(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

// NewInvite legt eine Einladung an und liefert das Token. Aussteller ernennt
// nur die Wurzel per Zertifikatsdatei, nie ein Link.
func (r *Registry) NewInvite(role, name string, ttl time.Duration, now time.Time) (string, error) {
	if role != RoleMember && role != RoleHelper {
		return "", fmt.Errorf("einladungen gibt es nur für %s und %s", RoleMember, RoleHelper)
	}
	if ttl <= 0 || ttl > MaxInviteTTL {
		ttl = MaxInviteTTL
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	r.Invites = append(r.Invites, Invite{Hash: tokenHash(token), Role: role, Name: name, ExpiresAt: now.UTC().Add(ttl).Truncate(time.Second)})
	return token, r.save()
}

// Admin ist ein Aussteller mit seinem Buch.
type Admin struct {
	Iss      *Issuer
	Network  string
	Dir      string
	Registry *Registry
	Root     string // Textform des Wurzelschlüssels
}

// OpenAdmin nimmt den Wurzelschlüssel aus rootKey oder aus dem Verzeichnis,
// sonst den Knotenschlüssel mit gültigem Aussteller-Zertifikat. Fehlt alles,
// wird nichts angelegt.
func OpenAdmin(dir, network, rootKey string, now time.Time) (*Admin, error) {
	if rootKey == "" && network != "" {
		if p := RootKeyFile(dir, network); fileExists(p) {
			rootKey = p
		}
	}
	if rootKey != "" {
		root, err := identity.Load(rootKey)
		if err != nil {
			return nil, fmt.Errorf("wurzelschlüssel: %w", err)
		}
		iss, err := NewRootIssuer(root)
		if err != nil {
			return nil, err
		}
		if network != "" && network != iss.NetworkID() {
			return nil, fmt.Errorf("der wurzelschlüssel gehört zu netz %s, nicht zu %s", iss.NetworkID(), network)
		}
		pub, err := RootKey(root)
		if err != nil {
			return nil, err
		}
		// Das Buch liegt neben dem Schlüssel, auch auf einem Wechseldatenträger.
		a := &Admin{Iss: iss, Network: iss.NetworkID(), Dir: filepath.Dir(rootKey), Root: EncodeRoot(pub)}
		a.Registry, err = OpenRegistry(RegistryFile(a.Dir, a.Network))
		return a, err
	}
	if network == "" {
		return nil, errors.New("-net oder -root-key angeben")
	}
	b, err := ReadBundle(CertFile(dir, network))
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("kein wurzelschlüssel und kein zertifikat für netz %s in %s", network, dir)
	}
	if err != nil {
		return nil, err
	}
	key, err := identity.Load(NodeKeyFile(dir, network))
	if err != nil {
		return nil, fmt.Errorf("knotenschlüssel: %w", err)
	}
	n, err := b.Network()
	if err != nil {
		return nil, err
	}
	leaf, err := n.Admit(b.Chain, key.ID.String(), now)
	if err != nil {
		return nil, fmt.Errorf("eigenes zertifikat: %w", err)
	}
	iss, err := NewIssuer(key, leaf)
	if err != nil {
		return nil, fmt.Errorf("dieser knoten darf nicht ausstellen (rolle %s): %w", leaf.Role, err)
	}
	a := &Admin{Iss: iss, Network: network, Dir: dir, Root: b.Root}
	a.Registry, err = OpenRegistry(RegistryFile(dir, network))
	return a, err
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// Redeem löst ein Token ein und stellt auf subject aus. Der Verbrauch steht im
// Buch, bevor die Kette herausgeht, damit ein Token nie zweimal wirkt.
func (a *Admin) Redeem(token, subject string, now time.Time) ([]Cert, error) {
	want := tokenHash(token)
	for i := range a.Registry.Invites {
		inv := &a.Registry.Invites[i]
		if subtle.ConstantTimeCompare([]byte(inv.Hash), []byte(want)) != 1 {
			continue
		}
		switch {
		case now.After(inv.ExpiresAt):
			return nil, ErrInviteExpired
		case inv.UsedBy != "" && inv.UsedBy != subject:
			return nil, ErrInviteUsed // dieselbe Identität darf erneut fragen, falls die Antwort verloren ging
		}
		if a.Registry.IsRevoked(subject) {
			return nil, ErrRevoked
		}
		chain, err := a.Iss.Issue(subject, inv.Role, inv.Name, NodeTTL, now)
		if err != nil {
			return nil, err
		}
		inv.UsedBy = subject
		if err := a.Registry.AddIssued(chain[len(chain)-1]); err != nil {
			return nil, err
		}
		return chain, nil
	}
	return nil, ErrInviteInvalid
}

// Renew stellt einem zugelassenen Peer neu aus, mit Rolle und Name aus seinem
// geprüften Zertifikat. Aussteller erneuert nur die Wurzel von Hand.
func (a *Admin) Renew(subject, role, name string, now time.Time) ([]Cert, error) {
	if role == RoleIssuer {
		return nil, errors.New("aussteller-zertifikate erneuert die wurzel")
	}
	if a.Registry.IsRevoked(subject) {
		return nil, ErrRevoked
	}
	chain, err := a.Iss.Issue(subject, role, name, NodeTTL, now)
	if err != nil {
		return nil, err
	}
	return chain, a.Registry.AddIssued(chain[len(chain)-1])
}

// ReadCRL liest eine einzelne Sperrliste.
func ReadCRL(path string) (CRL, error) {
	var l CRL
	data, err := os.ReadFile(path)
	if err != nil {
		return l, err
	}
	return l, json.Unmarshal(data, &l)
}

// SaveCRLs schreibt alle Listen des Netzes.
func (n *Network) SaveCRLs(path string) error { return writeJSON(path, n.CRLs()) }

// LoadCRLs liest gespeicherte Listen. Die Prüfung gilt wie beim Empfang; fehlt
// die Datei, beginnt das Netz ohne Listen.
func (n *Network) LoadCRLs(path string, now time.Time) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var lists []CRL
	if err := json.Unmarshal(data, &lists); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	for _, l := range lists {
		if err := n.AddCRL(l, now); err != nil && !errors.Is(err, ErrStaleCRL) {
			return fmt.Errorf("%s: %w", path, err)
		}
	}
	return nil
}
