// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Rainer Sestig und Mitwirkende.

// Package invite kodiert Einladungslinks nach spec/p2p-layer.md § 2.2:
//
//	swarmainet://join?v=1&net=<network_id>&peer=<multiaddr>[&peer=…][&tracker=<url>][&psk=<base64url>]
//
// und in ein Mitgliedsnetz nach spec/membership.md § 4:
//
//	swarmainet://join?v=2&net=<network_id>&root=<wurzelschlüssel>&issuer=<peer-id>&peer=<multiaddr>[&peer=…]&token=<base64url>
package invite

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/pnet"

	"github.com/rainer-sharp/Ai-Swarms.net/internal/config"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/membership"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/tracker"
)

const scheme = "swarmainet"

type Invite struct {
	Network  string
	Peers    []string // Multiadressen mit /p2p/<peer-id>
	Trackers []string
	PSK      []byte // 32 Byte; nur bei Einladungen mit Schlüssel
	// Mitgliedsnetz (spec/membership.md § 4): Wurzelschlüssel und Einmal-Token.
	Root  string
	Token string
	// Issuer ist die Peer-ID des Ausstellers. Nur er bekommt das Token; die
	// übrigen Adressen sind Einstieg, etwa ein Helfer, der es sonst abfangen könnte.
	Issuer string
}

func (i Invite) Validate() error {
	if !config.NetworkIDRe.MatchString(i.Network) {
		return fmt.Errorf("netz-kennung %q ungültig (erlaubt: a-z, 0-9, -; höchstens 32 zeichen)", i.Network)
	}
	if len(i.Peers) == 0 {
		return errors.New("einladung ohne adresse")
	}
	for _, p := range i.Peers {
		if _, err := peer.AddrInfoFromString(p); err != nil {
			return fmt.Errorf("adresse %q: %w", p, err)
		}
	}
	for _, t := range i.Trackers {
		if err := tracker.CheckURL(t); err != nil {
			return err
		}
	}
	if i.PSK != nil && len(i.PSK) != 32 {
		return errors.New("netzschlüssel muss 32 byte lang sein")
	}
	if (i.Root == "") != (i.Token == "") || (i.Token == "") != (i.Issuer == "") {
		return errors.New("einladung in ein mitgliedsnetz braucht wurzelschlüssel, token und aussteller")
	}
	if i.Root != "" {
		reachable := false
		for _, p := range i.Peers {
			ai, err := peer.AddrInfoFromString(p)
			reachable = reachable || err == nil && ai.ID.String() == i.Issuer
		}
		if !reachable {
			return errors.New("einladung ohne adresse des ausstellers")
		}
		if i.PSK != nil {
			return errors.New("mitgliedsnetz und netzschlüssel schließen einander aus")
		}
		root, err := membership.DecodeRoot(i.Root)
		if err != nil {
			return err
		}
		if want := membership.NetworkID(root); want != i.Network {
			return fmt.Errorf("netz-kennung %q passt nicht zum wurzelschlüssel (%s)", i.Network, want)
		}
	}
	return nil
}

func (i Invite) String() string {
	q := url.Values{}
	q.Set("v", "1")
	if i.Token != "" {
		q.Set("v", "2")
		q.Set("root", i.Root)
		q.Set("token", i.Token)
		q.Set("issuer", i.Issuer)
	}
	q.Set("net", i.Network)
	for _, p := range i.Peers {
		q.Add("peer", p)
	}
	for _, t := range i.Trackers {
		q.Add("tracker", t)
	}
	if i.PSK != nil {
		q.Set("psk", base64.RawURLEncoding.EncodeToString(i.PSK))
	}
	return scheme + "://join?" + q.Encode()
}

// Parse liest einen Einladungslink. Unbekannte Parameter werden ignoriert.
func Parse(link string) (Invite, error) {
	u, err := url.Parse(link)
	if err != nil {
		return Invite{}, err
	}
	if u.Scheme != scheme || u.Host != "join" {
		return Invite{}, fmt.Errorf("kein einladungslink (erwartet %s://join?…)", scheme)
	}
	q := u.Query()
	v := q.Get("v")
	if v != "1" && v != "2" {
		return Invite{}, fmt.Errorf("einladungsversion %q nicht unterstützt", v)
	}
	i := Invite{Network: q.Get("net"), Peers: q["peer"], Trackers: q["tracker"]}
	if v == "2" {
		i.Root, i.Token, i.Issuer = q.Get("root"), q.Get("token"), q.Get("issuer")
		if i.Token == "" {
			return Invite{}, errors.New("einladung ohne token")
		}
	}
	if s := q.Get("psk"); s != "" {
		if i.PSK, err = base64.RawURLEncoding.DecodeString(s); err != nil {
			return Invite{}, fmt.Errorf("netzschlüssel: %w", err)
		}
	}
	return i, i.Validate()
}

// ReadPSK liest eine Schlüsseldatei im libp2p-Format (/key/swarm/psk/1.0.0/).
func ReadPSK(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return pnet.DecodeV1PSK(bytes.NewReader(data))
}

// EncodePSK erzeugt den Inhalt einer Schlüsseldatei.
func EncodePSK(key []byte) []byte {
	return []byte("/key/swarm/psk/1.0.0/\n/base16/\n" + hex.EncodeToString(key) + "\n")
}
