// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Rainer Sestig. Kommerzielle Lizenz: siehe COMMERCIAL.md.

package main

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	ma "github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"

	"github.com/rainer-sharp/Ai-Swarms.net/internal/config"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/invite"
)

// multiFlag sammelt eine mehrfach angegebene Option.
type multiFlag []string

func (m *multiFlag) String() string     { return strings.Join(*m, ",") }
func (m *multiFlag) Set(s string) error { *m = append(*m, s); return nil }

type netInfo struct {
	NetworkID string   `json:"network_id"`
	Addrs     []string `json:"addrs"`
	Private   bool     `json:"private_network"`
}

// networkStatus fragt den laufenden Knoten nach einem seiner Netze.
func networkStatus(cfg config.Config, network string) (netInfo, error) {
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Get("http://" + cfg.Node.Gateway + "/api/status")
	if err != nil {
		return netInfo{}, fmt.Errorf("knoten läuft nicht (%s); erst `swarmainet run` starten: %w", cfg.Node.Gateway, err)
	}
	defer resp.Body.Close()
	var status struct {
		Networks []netInfo `json:"networks"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return netInfo{}, err
	}
	i := slices.IndexFunc(status.Networks, func(n netInfo) bool { return n.NetworkID == network })
	if i < 0 {
		return netInfo{}, fmt.Errorf("netz %q läuft auf diesem knoten nicht", network)
	}
	return status.Networks[i], nil
}

// usableAddrs: Adressen für eine Einladung, ohne Loopback; private nur im
// privaten Netz oder mit lan (§ 3).
func usableAddrs(st netInfo, lan bool) []string {
	var out []string
	for _, s := range st.Addrs {
		a, err := ma.NewMultiaddr(s)
		if err != nil {
			continue
		}
		first, _ := ma.SplitFunc(a, func(c ma.Component) bool { return c.Protocol().Code == ma.P_CIRCUIT })
		switch {
		case first == nil || manet.IsIPLoopback(first):
		case st.Private || lan || manet.IsPublicAddr(first):
			out = append(out, s)
		}
	}
	return out
}

// cmdInvite erzeugt einen Einladungslink aus den Adressen des laufenden
// Knotens (spec/p2p-layer.md § 2.2).
func cmdInvite(path, network string, withKey, lan, showQR bool, extra []string) error {
	cfg, err := config.Load(path)
	if err != nil {
		return err
	}
	network = cmp.Or(network, cfg.Node.NetworkID)
	st, err := networkStatus(cfg, network)
	if err != nil {
		return err
	}
	var keyPath string
	inv := invite.Invite{Network: network}
	for _, n := range cfg.AllNetworks() {
		if n.NetworkID == network {
			keyPath, inv.Trackers = n.NetworkKeyFile, n.Trackers
		}
	}
	inv.Peers = usableAddrs(st, lan)
	inv.Peers = append(inv.Peers, extra...)
	if len(inv.Peers) == 0 {
		return errors.New("keine geeignete adresse; mit -lan auch lokale adressen aufnehmen oder mit -peer die adresse eines erreichbaren knotens angeben")
	}
	switch {
	case withKey && !st.Private:
		return errors.New("-with-key gibt es nur für private netze")
	case withKey:
		if inv.PSK, err = invite.ReadPSK(keyPath); err != nil {
			return fmt.Errorf("netzschlüssel: %w", err)
		}
	}
	if err := inv.Validate(); err != nil {
		return err
	}
	fmt.Println(inv)
	if showQR {
		if err := printQR(os.Stderr, inv.String()); err != nil {
			return err
		}
	}
	switch {
	case withKey:
		fmt.Fprintln(os.Stderr, "\nAchtung: Der Link enthält den Netzschlüssel. Wer ihn hat, ist Mitglied des Netzes.\n"+
			"Nur über einen vertraulichen Kanal weitergeben und nicht dauerhaft speichern.")
		if showQR {
			fmt.Fprintln(os.Stderr, "Der QR-Code enthält ihn ebenfalls: nicht fotografieren, nur scannen und danach vom Telefon löschen.")
		}
	case st.Private:
		fmt.Fprintln(os.Stderr, "\nOhne Netzschlüssel: Der Eingeladene braucht zusätzlich die Schlüsseldatei\n"+
			"(swarmainet join -key DATEI <link>) oder einen Link mit -with-key.")
	}
	return nil
}

// replacesFirstNetwork: Das erste Netz wird ersetzt, wenn es das unveränderte
// offene Netz der Grundkonfiguration oder dasselbe Netz ist.
func replacesFirstNetwork(cfg config.Config, network string) bool {
	return cfg.Node.NetworkID == network ||
		cfg.Node.NetworkID == config.Default().Node.NetworkID && cfg.Node.NetworkKeyFile == "" &&
			len(cfg.Node.Bootstrap) == 0 && len(cfg.Node.StaticPeers) == 0 && len(cfg.Node.Networks) == 0
}

// cmdJoin trägt das Netz aus einer Einladung in die Konfiguration ein und
// legt sie bei Bedarf an. Kommentare in der Datei bleiben erhalten.
func cmdJoin(path, link, keyFile string) error {
	if link == "" {
		return errors.New("aufruf: swarmainet join [-key DATEI] <swarmainet://join?…>")
	}
	inv, err := invite.Parse(link)
	if err != nil {
		return err
	}
	if inv.Token != "" {
		return joinMember(path, inv, os.Stdout)
	}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		if err := cmdInit(path); err != nil {
			return err
		}
		fmt.Println()
	}
	cfg, err := config.Load(path)
	if err != nil {
		return err
	}
	// Das erste Netz wird ersetzt, wenn es das unveränderte offene Netz der
	// Grundkonfiguration ist oder dasselbe Netz; sonst kommt ein weiteres dazu.
	first := replacesFirstNetwork(cfg, inv.Network)
	extraIdx := slices.IndexFunc(cfg.Node.Networks, func(n config.Network) bool { return n.NetworkID == inv.Network })

	set := map[string]any{"network_id": inv.Network}
	switch {
	case inv.PSK != nil:
		kp := filepath.Join(cfg.Node.DataDir, inv.Network+".key")
		if old, err := invite.ReadPSK(kp); err == nil {
			if !slices.Equal(old, inv.PSK) {
				return fmt.Errorf("%s enthält einen anderen schlüssel; bitte prüfen und ggf. entfernen", kp)
			}
		} else if err := os.WriteFile(kp, invite.EncodePSK(inv.PSK), 0o600); err != nil {
			return err
		}
		set["network_key_file"] = kp
	case keyFile != "":
		if _, err := invite.ReadPSK(keyFile); err != nil {
			return fmt.Errorf("schlüsseldatei %s: %w", keyFile, err)
		}
		abs, err := filepath.Abs(keyFile)
		if err != nil {
			return err
		}
		set["network_key_file"] = abs
	case cfg.Node.NetworkKeyFile == "":
		fmt.Println("Hinweis: Einladung ohne Netzschlüssel – Beitritt als offenes Netz.\n" +
			"Für ein privates Netz die Schlüsseldatei mit -key DATEI angeben.")
	}
	merge := func(boot []string) []string {
		boot = slices.Clone(boot)
		for _, p := range inv.Peers {
			if !slices.Contains(boot, p) {
				boot = append(boot, p)
			}
		}
		return boot
	}
	mergeTrackers := func(have []string) []string {
		have = slices.Clone(have)
		for _, t := range inv.Trackers {
			if !slices.Contains(have, t) {
				have = append(have, t)
			}
		}
		return have
	}
	where := "node"
	if first {
		set["bootstrap"] = merge(cfg.Node.Bootstrap)
		if len(inv.Trackers) > 0 {
			set["trackers"] = mergeTrackers(cfg.Node.Trackers)
		}
	} else {
		nets := slices.Clone(cfg.Node.Networks)
		if extraIdx < 0 {
			private := set["network_key_file"] != nil
			nets = append(nets, config.Network{NetworkID: inv.Network, Listen: nextListen(cfg, private)})
			extraIdx = len(nets) - 1
		}
		x := &nets[extraIdx]
		x.Bootstrap = merge(x.Bootstrap)
		x.Trackers = mergeTrackers(x.Trackers)
		if kf, ok := set["network_key_file"].(string); ok {
			x.NetworkKeyFile = kf
		}
		set = map[string]any{"networks": nets}
		where = fmt.Sprintf("node.networks[%d], listen %s", extraIdx, strings.Join(x.Listen, " "))
	}
	if err := config.UpdateNode(path, set); err != nil {
		return err
	}
	fmt.Printf("Netz %q eingetragen in %s (%s, %d Einstiegsadressen).\n", inv.Network, path, where, len(inv.Peers))
	fmt.Println("Knoten (neu) starten: swarmainet run")
	return nil
}

// nextListen wählt für ein weiteres Netz den nächsten freien Port nach dem
// höchsten bisher konfigurierten (Standard 4711). Private Netze nutzen nur TCP.
func nextListen(cfg config.Config, private bool) []string {
	port := 4711
	for _, n := range cfg.AllNetworks() {
		for _, a := range n.Listen {
			m, err := ma.NewMultiaddr(a)
			if err != nil {
				continue
			}
			for _, code := range []int{ma.P_TCP, ma.P_UDP} {
				if v, err := m.ValueForProtocol(code); err == nil {
					if p, _ := strconv.Atoi(v); p > port {
						port = p
					}
				}
			}
		}
	}
	port++
	p := strconv.Itoa(port)
	out := []string{"/ip4/0.0.0.0/tcp/" + p, "/ip6/::/tcp/" + p}
	if !private {
		out = append(out, "/ip4/0.0.0.0/udp/"+p+"/quic-v1", "/ip6/::/udp/"+p+"/quic-v1")
	}
	return out
}
