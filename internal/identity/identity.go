// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Rainer Sestig. Kommerzielle Lizenz: siehe COMMERCIAL.md.

// Package identity verwaltet das lokale Schlüsselpaar eines Knotens.
// Die Peer-ID wird aus dem öffentlichen Schlüssel abgeleitet und bleibt
// bei IP-Wechsel gleich.
package identity

import (
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
)

type Identity struct {
	Priv crypto.PrivKey
	ID   peer.ID
}

// Load liest einen vorhandenen Schlüssel und legt nie einen neuen an, etwa für
// den Wurzelschlüssel eines Netzes, den kein Tippfehler ersetzen darf.
func Load(path string) (*Identity, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	priv, err := crypto.UnmarshalPrivateKey(data)
	if err != nil {
		return nil, fmt.Errorf("schlüssel %s: %w", path, err)
	}
	return fromPriv(priv)
}

// Create speichert den Schlüssel in einer neuen Datei mit Rechten 0600 und
// überschreibt keine vorhandene.
func (i *Identity) Create(path string) error {
	raw, err := crypto.MarshalPrivateKey(i.Priv)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(raw)
	return err
}

// LoadOrCreate liest den Schlüssel aus path oder erzeugt einen neuen
// Ed25519-Schlüssel und speichert ihn mit Rechten 0600.
func LoadOrCreate(path string) (*Identity, error) {
	data, err := os.ReadFile(path)
	if err == nil {
		priv, err := crypto.UnmarshalPrivateKey(data)
		if err != nil {
			return nil, fmt.Errorf("schlüssel %s: %w", path, err)
		}
		return fromPriv(priv)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	priv, _, err := crypto.GenerateEd25519Key(rand.Reader)
	if err != nil {
		return nil, err
	}
	raw, err := crypto.MarshalPrivateKey(priv)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		return nil, err
	}
	return fromPriv(priv)
}

// New erzeugt eine flüchtige Identität (für Tests).
func New() (*Identity, error) {
	priv, _, err := crypto.GenerateEd25519Key(rand.Reader)
	if err != nil {
		return nil, err
	}
	return fromPriv(priv)
}

func fromPriv(priv crypto.PrivKey) (*Identity, error) {
	id, err := peer.IDFromPrivateKey(priv)
	if err != nil {
		return nil, err
	}
	return &Identity{Priv: priv, ID: id}, nil
}

func (i *Identity) Sign(data []byte) ([]byte, error) {
	return i.Priv.Sign(data)
}

// Verify prüft eine Signatur gegen den in der Peer-ID eingebetteten Schlüssel.
func Verify(id string, data, sig []byte) error {
	pid, err := peer.Decode(id)
	if err != nil {
		return fmt.Errorf("ungültige peer-id: %w", err)
	}
	pub, err := pid.ExtractPublicKey()
	if err != nil {
		return fmt.Errorf("kein schlüssel in peer-id: %w", err)
	}
	ok, err := pub.Verify(data, sig)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("signatur ungültig")
	}
	return nil
}
