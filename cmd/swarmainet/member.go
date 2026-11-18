// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Rainer Sestig. Kommerzielle Lizenz: siehe COMMERCIAL.md.

package main

import (
	"cmp"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/rainer-sharp/Ai-Swarms.net/internal/config"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/identity"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/invite"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/membership"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/p2p"
)

// memberFlags sind die gemeinsamen Optionen von network und member.
type memberFlags struct {
	*flag.FlagSet
	config, dir *string
}

func newMemberFlags(name, home string) memberFlags {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	return memberFlags{fs,
		fs.String("config", filepath.Join(home, ".swarmainet", "config.yaml"), "Konfigurationsdatei"),
		fs.String("dir", "", "Verzeichnis der Schlüssel (Standard: node.data_dir)"),
	}
}

func (f memberFlags) dataDir() (string, error) {
	if *f.dir != "" {
		return *f.dir, nil
	}
	cfg, err := config.Load(*f.config)
	if err != nil {
		return "", fmt.Errorf("konfiguration: %w (oder -dir angeben)", err)
	}
	return cfg.Node.DataDir, nil
}

// issuer lädt den Aussteller. Mit -root-key braucht es kein Datenverzeichnis,
// das Gerät mit dem Wurzelschlüssel muss kein Knoten sein.
func (f memberFlags) issuer(network, rootKey string, now time.Time) (*membership.Admin, error) {
	dir := *f.dir
	if dir == "" && rootKey == "" {
		var err error
		if dir, err = f.dataDir(); err != nil {
			return nil, err
		}
	}
	return membership.OpenAdmin(dir, network, rootKey, now)
}

// cmdNetwork: swarmainet network create
func cmdNetwork(args []string, home string, out io.Writer) error {
	if len(args) == 0 || args[0] != "create" {
		return errors.New("aufruf: swarmainet network create [-dir VERZEICHNIS]")
	}
	f := newMemberFlags("network create", home)
	if err := f.Parse(args[1:]); err != nil {
		return err
	}
	dir, err := f.dataDir()
	if err != nil {
		return err
	}
	root, err := identity.New()
	if err != nil {
		return err
	}
	pub, err := membership.RootKey(root)
	if err != nil {
		return err
	}
	network := membership.NetworkID(pub)
	path := membership.RootKeyFile(dir, network)
	if err := root.Create(path); err != nil {
		return err
	}
	fmt.Fprintf(out, `Mitgliedsnetz angelegt.
  Kennung:          %s
  Wurzelschlüssel:  %s  (öffentlich, gehört in jede Einladung)
  Geheimer Schlüssel: %s

Wer die Schlüsseldatei hat, beherrscht das Netz. Sichern Sie sie und lagern Sie sie offline.
Sie wird nur gebraucht, um Aussteller zu ernennen oder zu sperren.

Nächster Schritt auf dem ersten Knoten:
  swarmainet member request -net %s
`, network, membership.EncodeRoot(pub), path, network)
	return nil
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// cmdMember: swarmainet member request|issue|install|revoke|list
func cmdMember(args []string, home string, now time.Time, out io.Writer) error {
	if len(args) == 0 {
		return errors.New("aufruf: swarmainet member invite|request|issue|install|revoke|import-crl|list")
	}
	switch args[0] {
	case "request":
		return memberRequest(args[1:], home, out)
	case "invite":
		return memberInvite(args[1:], home, now, out)
	case "issue":
		return memberIssue(args[1:], home, now, out)
	case "install":
		return memberInstall(args[1:], home, now, out)
	case "revoke":
		return memberRevoke(args[1:], home, now, out)
	case "import-crl":
		return memberImportCRL(args[1:], home, now, out)
	case "list":
		return memberList(args[1:], home, now, out)
	}
	return fmt.Errorf("unbekannter befehl member %s", args[0])
}

// memberRequest legt die Identität dieses Knotens für das Netz an und nennt
// die Peer-ID. Der private Schlüssel bleibt auf dem Gerät.
func memberRequest(args []string, home string, out io.Writer) error {
	f := newMemberFlags("member request", home)
	network := f.String("net", "", "Netz-Kennung")
	if err := f.Parse(args); err != nil {
		return err
	}
	if !strings.HasPrefix(*network, "m-") {
		return errors.New("aufruf: swarmainet member request -net m-…")
	}
	dir, err := f.dataDir()
	if err != nil {
		return err
	}
	id, err := identity.LoadOrCreate(membership.NodeKeyFile(dir, *network))
	if err != nil {
		return err
	}
	fmt.Fprintf(out, `Peer-ID dieses Knotens im Netz %s:
  %s

Beim Aussteller:
  swarmainet member issue -net %s [-role member|helper|issuer] [-name NAME] %s
`, *network, id.ID, *network, id.ID)
	return nil
}

func memberIssue(args []string, home string, now time.Time, out io.Writer) error {
	f := newMemberFlags("member issue", home)
	network := f.String("net", "", "Netz-Kennung")
	rootKey := f.String("root-key", "", "Datei mit dem Wurzelschlüssel, etwa auf einem Wechseldatenträger")
	role := f.String("role", membership.RoleMember, "member, helper oder issuer")
	name := f.String("name", "", "Bezeichnung des Geräts, keine Personennamen")
	ttl := f.Duration("ttl", 0, "Laufzeit (Standard: 720h, Aussteller 8760h)")
	output := f.String("out", "", "Zertifikatsdatei (Standard: <kennung>-<peer>.cert im aktuellen Verzeichnis)")
	if err := f.Parse(args); err != nil {
		return err
	}
	subject := f.Arg(0)
	if subject == "" {
		return errors.New("aufruf: swarmainet member issue -net m-… [-role R] [-name N] [-ttl D] [-out DATEI] <peer-id>")
	}
	ctx, err := f.issuer(*network, *rootKey, now)
	if err != nil {
		return err
	}
	if ctx.Registry.IsRevoked(subject) {
		return fmt.Errorf("%s ist gesperrt; ein gesperrter knoten braucht eine neue identität", subject)
	}
	d := *ttl
	if d == 0 {
		d = membership.NodeTTL
		if *role == membership.RoleIssuer {
			d = membership.IssuerTTL
		}
	}
	chain, err := ctx.Iss.Issue(subject, *role, *name, d, now)
	if err != nil {
		return err
	}
	path := *output
	if path == "" {
		path = ctx.Network + "-" + subject[max(0, len(subject)-6):] + ".cert"
	}
	if fileExists(path) {
		return fmt.Errorf("%s existiert bereits; mit -out eine andere datei wählen", path)
	}
	bundle := membership.Bundle{V: 1, Root: ctx.Root, Chain: chain}
	if err := bundle.Write(path); err != nil {
		return err
	}
	leaf := bundle.Leaf()
	if err := ctx.Registry.AddIssued(leaf); err != nil {
		return err
	}
	fmt.Fprintf(out, `Zertifikat ausgestellt: %s
  Rolle %s, gültig bis %s

Datei auf den Knoten kopieren (sie ist nicht geheim, aber nicht für die Öffentlichkeit) und dort ausführen:
  swarmainet member install -root %s %s
`, path, leaf.Role, leaf.ExpiresAt.Format(time.DateOnly), ctx.Root, filepath.Base(path))
	return nil
}

// memberInstall übernimmt ein Zertifikat, wenn es zum angegebenen
// Wurzelschlüssel und zur Identität dieses Knotens passt.
func memberInstall(args []string, home string, now time.Time, out io.Writer) error {
	f := newMemberFlags("member install", home)
	root := f.String("root", "", "Wurzelschlüssel aus sicherer Quelle")
	if err := f.Parse(args); err != nil {
		return err
	}
	if *root == "" || f.Arg(0) == "" {
		return errors.New("aufruf: swarmainet member install -root WURZELSCHLÜSSEL DATEI")
	}
	dir, err := f.dataDir()
	if err != nil {
		return err
	}
	b, err := membership.ReadBundle(f.Arg(0))
	if err != nil {
		return err
	}
	if b.Root != *root {
		return errors.New("das zertifikat gehört zu einem anderen wurzelschlüssel als dem angegebenen")
	}
	n, err := b.Network()
	if err != nil {
		return err
	}
	network := n.ID()
	key, err := identity.Load(membership.NodeKeyFile(dir, network))
	if err != nil {
		return fmt.Errorf("knotenschlüssel: %w (erst swarmainet member request -net %s)", err, network)
	}
	leaf, err := n.Admit(b.Chain, key.ID.String(), now)
	if err != nil {
		return fmt.Errorf("zertifikat nicht übernommen: %w", err)
	}
	path := membership.CertFile(dir, network)
	if err := b.Write(path); err != nil {
		return err
	}
	fmt.Fprintf(out, "Zertifikat übernommen: %s\n  Netz %s, Rolle %s, gültig bis %s\n", path, network, leaf.Role, leaf.ExpiresAt.Format(time.DateOnly))
	if *f.dir != "" {
		return nil // eigenes Verzeichnis: keine Konfiguration anfassen
	}
	return addMemberNetwork(*f.config, network, *root, nil, out)
}

// memberInvite erzeugt einen Einladungslink. Der Aussteller muss laufen, damit
// der Eingeladene ihn erreicht.
func memberInvite(args []string, home string, now time.Time, out io.Writer) error {
	f := newMemberFlags("member invite", home)
	network := f.String("net", "", "Netz-Kennung")
	role := f.String("role", membership.RoleMember, "member oder helper")
	name := f.String("name", "", "Bezeichnung des Geräts, keine Personennamen")
	ttl := f.Duration("ttl", membership.MaxInviteTTL, "Gültigkeit, höchstens 24h")
	var extra multiFlag
	f.Var(&extra, "peer", "zusätzliche Adresse, etwa über einen Helfer (mehrfach)")
	showQR := f.Bool("qr", false, "Link zusätzlich als QR-Code anzeigen")
	if err := f.Parse(args); err != nil {
		return err
	}
	if !strings.HasPrefix(*network, "m-") {
		return errors.New("aufruf: swarmainet member invite -net m-… [-role R] [-name N] [-ttl D] [-peer ADR] [-qr]")
	}
	ctx, err := f.issuer(*network, "", now)
	if err != nil {
		return err
	}
	dir, err := f.dataDir()
	if err != nil {
		return err
	}
	// Einlösen muss der laufende Knoten; seine Peer-ID steht im Link.
	key, err := identity.Load(membership.NodeKeyFile(dir, *network))
	if err != nil {
		return fmt.Errorf("knotenschlüssel: %w", err)
	}
	inv := invite.Invite{Network: *network, Root: ctx.Root, Issuer: key.ID.String()}
	if cfg, err := config.Load(*f.config); err == nil {
		if st, err := networkStatus(cfg, *network); err == nil {
			inv.Peers = usableAddrs(st, false)
		}
	}
	inv.Peers = append(inv.Peers, extra...)
	if len(inv.Peers) == 0 {
		return errors.New("keine adresse: der knoten muss laufen, oder mit -peer die adresse des ausstellers angeben")
	}
	token, err := ctx.Registry.NewInvite(*role, *name, *ttl, now)
	if err != nil {
		return err
	}
	inv.Token = token
	if err := inv.Validate(); err != nil {
		return err
	}
	fmt.Fprintln(out, inv)
	if *showQR {
		if err := printQR(os.Stderr, inv.String()); err != nil {
			return err
		}
	}
	fmt.Fprintf(os.Stderr, "\nDie Einladung öffnet einen Platz als %s, gilt einmal und höchstens %s. Vertraulich weitergeben.\n"+
		"Dieser Knoten muss laufen, wenn sie eingelöst wird.\n", *role, min(*ttl, membership.MaxInviteTTL))
	return nil
}

// joinMember löst eine Einladung ein: Identität lokal anlegen, beim Aussteller
// einlösen, die Kette gegen den Wurzelschlüssel aus dem Link prüfen.
func joinMember(path string, inv invite.Invite, out io.Writer) error {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		if err := cmdInit(path); err != nil {
			return err
		}
		fmt.Fprintln(out)
	}
	cfg, err := config.Load(path)
	if err != nil {
		return err
	}
	root, err := membership.DecodeRoot(inv.Root)
	if err != nil {
		return err
	}
	n, err := membership.NewNetwork(inv.Network, root)
	if err != nil {
		return err
	}
	dir := cfg.Node.DataDir
	id, err := identity.LoadOrCreate(membership.NodeKeyFile(dir, inv.Network))
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	chain, err := p2p.RedeemInvite(ctx, id, inv.Peers, inv.Issuer, inv.Token)
	if err != nil {
		return err
	}
	leaf, err := n.Admit(chain, id.ID.String(), time.Now())
	if err != nil {
		return fmt.Errorf("ausgestelltes zertifikat ungültig: %w", err)
	}
	if err := (membership.Bundle{V: 1, Root: inv.Root, Chain: chain}).Write(membership.CertFile(dir, inv.Network)); err != nil {
		return err
	}
	fmt.Fprintf(out, "Aufgenommen in %s als %s, gültig bis %s.\n", inv.Network, leaf.Role, leaf.ExpiresAt.Format(time.DateOnly))
	return addMemberNetwork(path, inv.Network, inv.Root, inv.Peers, out)
}

// addMemberNetwork trägt das Mitgliedsnetz in die Konfiguration ein, nach
// denselben Regeln wie join: Es ersetzt nur das unveränderte offene Netz der
// Grundkonfiguration oder dasselbe Netz, sonst kommt es als weiteres Netz dazu.
func addMemberNetwork(path, network, root string, peers []string, out io.Writer) error {
	cfg, err := config.Load(path)
	if err != nil {
		return err
	}
	mem := &config.Membership{Root: root}
	merge := func(have []string) []string {
		have = slices.Clone(have)
		for _, p := range peers {
			if !slices.Contains(have, p) {
				have = append(have, p)
			}
		}
		return have
	}
	var set map[string]any
	where := "node"
	if replacesFirstNetwork(cfg, network) {
		set = map[string]any{"network_id": network, "membership": mem, "bootstrap": merge(cfg.Node.Bootstrap), "dns_seeds": []string{}}
	} else {
		nets := slices.Clone(cfg.Node.Networks)
		i := slices.IndexFunc(nets, func(n config.Network) bool { return n.NetworkID == network })
		if i < 0 {
			nets = append(nets, config.Network{NetworkID: network, Listen: nextListen(cfg, false)})
			i = len(nets) - 1
		}
		nets[i].Membership, nets[i].Bootstrap = mem, merge(nets[i].Bootstrap)
		set = map[string]any{"networks": nets}
		where = fmt.Sprintf("node.networks[%d], listen %s", i, strings.Join(nets[i].Listen, " "))
	}
	if err := config.UpdateNode(path, set); err != nil {
		return err
	}
	fmt.Fprintf(out, "Netz %s eingetragen in %s (%s).\nKnoten (neu) starten: swarmainet run\n", network, path, where)
	return nil
}

func memberRevoke(args []string, home string, now time.Time, out io.Writer) error {
	f := newMemberFlags("member revoke", home)
	network := f.String("net", "", "Netz-Kennung")
	rootKey := f.String("root-key", "", "Datei mit dem Wurzelschlüssel")
	reason := f.String("reason", "", "Grund, steht in der Sperrliste und ist für alle Mitglieder sichtbar")
	if err := f.Parse(args); err != nil {
		return err
	}
	target := f.Arg(0)
	if target == "" {
		return errors.New("aufruf: swarmainet member revoke -net m-… [-reason TEXT] <peer-id|name>")
	}
	ctx, err := f.issuer(*network, *rootKey, now)
	if err != nil {
		return err
	}
	subject, err := ctx.Registry.Resolve(target)
	if err != nil {
		return err
	}
	l, err := ctx.Registry.Revoke(ctx.Iss, subject, *reason, now)
	if err != nil {
		return err
	}
	path := membership.CRLFile(ctx.Dir, ctx.Network)
	if err := membership.WriteCRL(path, l); err != nil {
		return err
	}
	fmt.Fprintf(out, "Gesperrt: %s\nSperrliste Nummer %d: %s\n\n", subject, l.Seq, path)
	if *rootKey != "" {
		fmt.Fprintf(out, "Die Liste liegt beim Wurzelschlüssel. Auf einem laufenden Knoten einspielen:\n  swarmainet member import-crl -root %s %s\n", ctx.Root, filepath.Base(path))
		return nil
	}
	fmt.Fprintln(out, "Der laufende Knoten verteilt sie binnen Sekunden an alle erreichbaren Knoten und trennt den Gesperrten.\n"+
		"Läuft er nicht, geschieht das beim nächsten Start.")
	return nil
}

// memberImportCRL spielt eine Sperrliste ein, etwa die der Wurzel, deren Schlüssel
// offline liegt. Der laufende Knoten übernimmt und verteilt sie.
func memberImportCRL(args []string, home string, now time.Time, out io.Writer) error {
	f := newMemberFlags("member import-crl", home)
	root := f.String("root", "", "Wurzelschlüssel aus sicherer Quelle")
	if err := f.Parse(args); err != nil {
		return err
	}
	if *root == "" || f.Arg(0) == "" {
		return errors.New("aufruf: swarmainet member import-crl -root WURZELSCHLÜSSEL DATEI")
	}
	dir, err := f.dataDir()
	if err != nil {
		return err
	}
	pub, err := membership.DecodeRoot(*root)
	if err != nil {
		return err
	}
	n, err := membership.NewNetwork(membership.NetworkID(pub), pub)
	if err != nil {
		return err
	}
	l, err := membership.ReadCRL(f.Arg(0))
	if err != nil {
		return err
	}
	if err := n.AddCRL(l, now); err != nil {
		return fmt.Errorf("sperrliste nicht übernommen: %w", err)
	}
	if err := membership.WriteCRL(membership.CRLImportFile(dir, n.ID(), l.Issuer), l); err != nil {
		return err
	}
	fmt.Fprintf(out, "Sperrliste Nummer %d eingespielt (%d einträge). Der laufende Knoten verteilt sie binnen Sekunden.\n", l.Seq, len(l.Revoked))
	return nil
}

func memberList(args []string, home string, now time.Time, out io.Writer) error {
	f := newMemberFlags("member list", home)
	network := f.String("net", "", "Netz-Kennung")
	rootKey := f.String("root-key", "", "Datei mit dem Wurzelschlüssel")
	if err := f.Parse(args); err != nil {
		return err
	}
	ctx, err := f.issuer(*network, *rootKey, now)
	if err != nil {
		return err
	}
	if len(ctx.Registry.Issued) == 0 {
		fmt.Fprintln(out, "Dieser Aussteller hat noch nichts ausgestellt.")
		return nil
	}
	w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tROLLE\tPEER\tGÜLTIG BIS\tZUSTAND")
	for _, c := range ctx.Registry.Issued {
		state := "gültig"
		switch {
		case ctx.Registry.IsRevoked(c.Subject):
			state = "gesperrt"
		case now.After(c.ExpiresAt):
			state = "abgelaufen"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", cmp.Or(c.Name, "-"), c.Role, c.Subject, c.ExpiresAt.Format(time.DateOnly), state)
	}
	return w.Flush()
}
