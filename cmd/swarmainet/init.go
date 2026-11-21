// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Rainer Sestig. Kommerzielle Lizenz: siehe COMMERCIAL.md.

package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/rainer-sharp/Ai-Swarms.net/internal/config"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/identity"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/invite"
)

// privateListen: Private Netze laufen in libp2p nur über TCP.
var privateListen = []string{"/ip4/0.0.0.0/tcp/4711", "/ip6/::/tcp/4711"}

// createConfig legt Konfiguration und Peer-Schlüssel an. Ohne open richtet sie
// ein eigenes privates Netz mit frischem Netzschlüssel ein (docs/concept.md:
// erst die eigenen Geräte, dann Gruppe, dann alle); mit open das offene Netz.
func createConfig(path string, open bool) (config.Config, *identity.Identity, error) {
	if _, err := os.Stat(path); err == nil {
		return config.Config{}, nil, fmt.Errorf("%s existiert bereits", path)
	}
	cfg := config.Default()
	cfg.Node.DataDir = filepath.Dir(path)
	if !open {
		suffix := make([]byte, 4)
		if _, err := rand.Read(suffix); err != nil {
			return config.Config{}, nil, err
		}
		id := "p-" + hex.EncodeToString(suffix)
		keyPath := filepath.Join(cfg.Node.DataDir, id+".key")
		if err := os.MkdirAll(cfg.Node.DataDir, 0o700); err != nil {
			return config.Config{}, nil, err
		}
		if err := writeNewPSK(keyPath); err != nil {
			return config.Config{}, nil, err
		}
		cfg.Node.NetworkID, cfg.Node.NetworkKeyFile = id, keyPath
		cfg.Node.Listen = append([]string(nil), privateListen...)
	}
	if err := config.Save(path, cfg); err != nil {
		return config.Config{}, nil, err
	}
	id, err := identity.LoadOrCreate(filepath.Join(cfg.Node.DataDir, "identity.key"))
	if err != nil {
		return config.Config{}, nil, err
	}
	return cfg, id, nil
}

// writeNewPSK schreibt einen zufälligen Netzschlüssel; eine vorhandene Datei
// wird nie überschrieben.
func writeNewPSK(path string) error {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return fmt.Errorf("%s existiert bereits", path)
	}
	if err != nil {
		return err
	}
	if _, err := f.Write(invite.EncodePSK(key)); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func cmdInit(path string, open bool, out io.Writer) error {
	cfg, id, err := createConfig(path, open)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Konfiguration: %s\nPeer-ID:       %s\n", path, id.ID)
	if open {
		fmt.Fprintln(out, "Netz:          offenes Netz (swarmainet). Der Knoten ist dort mit Peer-ID und Adresse")
		fmt.Fprintln(out, "               sichtbar; fremde Aufträge nimmt er erst mit accept_public_tasks: true an.")
	} else {
		fmt.Fprintf(out, "Netz:          eigenes privates Netz %s\nSchlüssel:     %s (nicht weitergeben)\n",
			cfg.Node.NetworkID, cfg.Node.NetworkKeyFile)
		fmt.Fprintln(out, "\nWeitere eigene Geräte: hier `swarmainet invite -with-key`, dort `swarmainet join '<link>'`.")
		fmt.Fprintln(out, "Lieber einem vorhandenen Netz beitreten: hier `swarmainet join '<link>'`, solange der Knoten noch nicht lief.")
		fmt.Fprintln(out, "Offenes Netz stattdessen: Konfiguration löschen und `swarmainet init -open`.")
	}
	fmt.Fprintln(out, "\nNächste Schritte: unter backends[].models die Modelle eintragen,")
	fmt.Fprintln(out, "optional node.provide: true setzen und dann `swarmainet run` starten.")
	return nil
}

// initForJoin legt für join eine Grundkonfiguration an, deren erstes Netz die
// Einladung ersetzt. Ein eigenes privates Netz entsteht dabei nicht.
func initForJoin(path string, out io.Writer) error {
	_, id, err := createConfig(path, true)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Konfiguration angelegt: %s\nPeer-ID: %s\n\n", path, id.ID)
	return nil
}

// freshPrivate: das von init angelegte private Netz, das noch nie lief (kein
// Peer-Cache) und keine Einstiegsadressen hat. join ersetzt es, statt ein
// zweites Netz daneben anzulegen; ein benutztes Netz bleibt unangetastet.
func freshPrivate(cfg config.Config) bool {
	n := cfg.Node
	if !strings.HasPrefix(n.NetworkID, "p-") || n.NetworkKeyFile != filepath.Join(n.DataDir, n.NetworkID+".key") ||
		len(n.Bootstrap)+len(n.StaticPeers)+len(n.Trackers)+len(n.Networks) > 0 {
		return false
	}
	_, err := os.Stat(filepath.Join(n.DataDir, "peers-"+n.NetworkID+".json"))
	return errors.Is(err, os.ErrNotExist)
}
