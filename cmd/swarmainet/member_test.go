// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Rainer Sestig und Mitwirkende.

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/rainer-sharp/Ai-Swarms.net/internal/config"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/identity"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/invite"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/membership"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/node"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/rem"
)

// memberNow ist die echte Zeit: TestJoinEndToEnd löst die Einladung bei einem
// laufenden Knoten ein, der mit der Uhr prüft.
var memberNow = time.Now().UTC().Truncate(time.Second)

func member(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	err := cmdMember(args, t.TempDir(), memberNow, &out)
	return out.String(), err
}

func mustMember(t *testing.T, args ...string) string {
	t.Helper()
	out, err := member(t, args...)
	if err != nil {
		t.Fatalf("member %v: %v", args, err)
	}
	return out
}

// createNetwork legt ein Netz an und liefert Kennung und Wurzelschlüssel.
func createNetwork(t *testing.T, dir string) (network, root string) {
	t.Helper()
	var out bytes.Buffer
	if err := cmdNetwork([]string{"create", "-dir", dir}, t.TempDir(), &out); err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`Kennung:\s+(m-[0-9a-f]+)\s+Wurzelschlüssel:\s+(\S+)`).FindStringSubmatch(out.String())
	if m == nil {
		t.Fatalf("ausgabe: %s", out.String())
	}
	return m[1], m[2]
}

func parseCRL(t *testing.T, raw []byte) membership.CRL {
	t.Helper()
	var l membership.CRL
	if err := json.Unmarshal(raw, &l); err != nil {
		t.Fatal(err)
	}
	return l
}

func peerOf(t *testing.T, dir, network string) string {
	t.Helper()
	mustMember(t, "request", "-dir", dir, "-net", network)
	id, err := identity.Load(membership.NodeKeyFile(dir, network))
	if err != nil {
		t.Fatal(err)
	}
	return id.ID.String()
}

// TestMemberFlow geht den Weg aus § 4 und § 7 durch: Netz anlegen, Aussteller
// ernennen, Mitglied aufnehmen, sperren.
func TestMemberFlow(t *testing.T) {
	rootDir, issuerDir, nodeDir, files := t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()
	network, root := createNetwork(t, rootDir)

	// Die Wurzel ernennt einen Aussteller.
	issuerPeer := peerOf(t, issuerDir, network)
	issuerCert := filepath.Join(files, "issuer.cert")
	mustMember(t, "issue", "-dir", rootDir, "-net", network, "-role", "issuer", "-name", "server-1", "-out", issuerCert, issuerPeer)
	mustMember(t, "install", "-dir", issuerDir, "-root", root, issuerCert)

	// Der Aussteller nimmt ein Mitglied auf, die Wurzel ist dafür nicht nötig.
	nodePeer := peerOf(t, nodeDir, network)
	nodeCert := filepath.Join(files, "node.cert")
	mustMember(t, "issue", "-dir", issuerDir, "-net", network, "-name", "notebook-2", "-out", nodeCert, nodePeer)
	if out := mustMember(t, "install", "-dir", nodeDir, "-root", root, nodeCert); !strings.Contains(out, "Rolle member") {
		t.Fatalf("ausgabe: %s", out)
	}

	b, err := membership.ReadBundle(membership.CertFile(nodeDir, network))
	if err != nil {
		t.Fatal(err)
	}
	n, err := b.Network()
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Chain) != 2 {
		t.Fatalf("kette mit %d stufen", len(b.Chain))
	}
	if _, err := n.Admit(b.Chain, nodePeer, memberNow); err != nil {
		t.Fatal(err)
	}
	if out := mustMember(t, "list", "-dir", issuerDir, "-net", network); !strings.Contains(out, "notebook-2") || !strings.Contains(out, "gültig") {
		t.Fatalf("liste: %s", out)
	}

	// Mitglied darf nicht ausstellen.
	other, _ := identity.New()
	if _, err := member(t, "issue", "-dir", nodeDir, "-net", network, "-out", filepath.Join(files, "x.cert"), other.ID.String()); err == nil {
		t.Fatal("mitglied hat ausgestellt")
	}
	// Aussteller ernennt keinen Aussteller.
	if _, err := member(t, "issue", "-dir", issuerDir, "-net", network, "-role", "issuer", "-out", filepath.Join(files, "y.cert"), other.ID.String()); err == nil {
		t.Fatal("aussteller hat aussteller ernannt")
	}

	// Sperren über den Namen; die Sperrliste wirkt bei der Prüfung.
	mustMember(t, "revoke", "-dir", issuerDir, "-net", network, "-reason", "gerät verloren", "notebook-2")
	if out := mustMember(t, "list", "-dir", issuerDir, "-net", network); !strings.Contains(out, "gesperrt") {
		t.Fatalf("liste nach sperre: %s", out)
	}
	raw, err := os.ReadFile(membership.CRLFile(issuerDir, network))
	if err != nil {
		t.Fatal(err)
	}
	l := parseCRL(t, raw)
	if err := n.AddCRL(l, memberNow); err != nil {
		t.Fatal(err)
	}
	if _, err := n.Admit(b.Chain, nodePeer, memberNow); !errors.Is(err, membership.ErrRevoked) {
		t.Fatalf("nach sperre: %v", err)
	}
	// Zweimal sperren und neu ausstellen sind Fehler.
	if _, err := member(t, "revoke", "-dir", issuerDir, "-net", network, nodePeer); err == nil {
		t.Fatal("doppelt gesperrt")
	}
	if _, err := member(t, "issue", "-dir", issuerDir, "-net", network, "-out", filepath.Join(files, "z.cert"), nodePeer); err == nil {
		t.Fatal("gesperrter knoten bekam ein neues zertifikat")
	}

	// Die Wurzel sperrt per Peer-ID auch, was sie nicht selbst ausgestellt hat,
	// und die Folgenummer ihres Buchs steigt.
	mustMember(t, "revoke", "-dir", rootDir, "-net", network, other.ID.String())
	reg, err := membership.OpenRegistry(membership.RegistryFile(rootDir, network))
	if err != nil || reg.Seq != 1 {
		t.Fatalf("buch: %+v %v", reg, err)
	}
}

func TestInstallRejects(t *testing.T) {
	rootDir, nodeDir, otherDir, files := t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()
	network, root := createNetwork(t, rootDir)
	_, otherRoot := createNetwork(t, t.TempDir())
	nodePeer := peerOf(t, nodeDir, network)
	cert := filepath.Join(files, "node.cert")
	mustMember(t, "issue", "-dir", rootDir, "-net", network, "-out", cert, nodePeer)

	for _, c := range []struct {
		name string
		args []string
		want string
	}{
		{"anderer wurzelschlüssel", []string{"install", "-dir", nodeDir, "-root", otherRoot, cert}, "anderen wurzelschlüssel"},
		{"ohne wurzelschlüssel", []string{"install", "-dir", nodeDir, cert}, "aufruf"},
		{"knoten ohne identität", []string{"install", "-dir", otherDir, "-root", root, cert}, "member request"},
	} {
		t.Run(c.name, func(t *testing.T) {
			if _, err := member(t, c.args...); err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("fehler: %v", err)
			}
		})
	}
	if fileExists(membership.CertFile(nodeDir, network)) || fileExists(membership.CertFile(otherDir, network)) {
		t.Fatal("abgewiesenes zertifikat wurde gespeichert")
	}

	// Zertifikat eines anderen Knotens.
	peerOf(t, otherDir, network)
	if _, err := member(t, "install", "-dir", otherDir, "-root", root, cert); !errors.Is(err, membership.ErrSubject) {
		t.Fatalf("fremdes zertifikat: %v", err)
	}
}

func TestRootKeyHandling(t *testing.T) {
	rootDir, usb, files := t.TempDir(), t.TempDir(), t.TempDir()
	network, _ := createNetwork(t, rootDir)
	keyPath := membership.RootKeyFile(rootDir, network)
	if info, err := os.Stat(keyPath); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("schlüsseldatei: %v %v", info, err)
	}
	other, _ := identity.New()

	// Ein Tippfehler legt keinen neuen Wurzelschlüssel an.
	missing := filepath.Join(usb, "fehlt.key")
	if _, err := member(t, "issue", "-root-key", missing, "-out", filepath.Join(files, "a.cert"), other.ID.String()); err == nil || fileExists(missing) {
		t.Fatalf("fehlender schlüssel: %v, angelegt: %v", err, fileExists(missing))
	}
	// Vorhandene Schlüsseldatei wird nie überschrieben.
	id, _ := identity.New()
	if err := id.Create(keyPath); err == nil {
		t.Fatal("wurzelschlüssel überschrieben")
	}
	// Schlüssel anderer Netze passen nicht.
	otherNet, _ := createNetwork(t, t.TempDir())
	if _, err := member(t, "issue", "-root-key", keyPath, "-net", otherNet, "-out", filepath.Join(files, "b.cert"), other.ID.String()); err == nil {
		t.Fatal("schlüssel eines anderen netzes akzeptiert")
	}
	// Vom Wechseldatenträger: Das Buch liegt neben dem Schlüssel.
	moved := membership.RootKeyFile(usb, network)
	if err := os.Rename(keyPath, moved); err != nil {
		t.Fatal(err)
	}
	mustMember(t, "issue", "-root-key", moved, "-out", filepath.Join(files, "c.cert"), other.ID.String())
	if !fileExists(membership.RegistryFile(usb, network)) {
		t.Fatal("buch liegt nicht neben dem schlüssel")
	}
	// Vorhandene Ausgabedatei bleibt erhalten.
	if _, err := member(t, "issue", "-root-key", moved, "-out", filepath.Join(files, "c.cert"), other.ID.String()); err == nil {
		t.Fatal("zertifikatsdatei überschrieben")
	}
	// Ohne Schlüssel im Verzeichnis gibt es nichts zu tun, und nichts wird angelegt.
	empty := t.TempDir()
	if _, err := member(t, "issue", "-dir", empty, "-net", network, other.ID.String()); err == nil {
		t.Fatal("ausgestellt ohne schlüssel")
	}
	if entries, _ := os.ReadDir(empty); len(entries) != 0 {
		t.Fatalf("angelegt: %v", entries)
	}
}

func TestNameMustBeUnique(t *testing.T) {
	rootDir, files := t.TempDir(), t.TempDir()
	network, _ := createNetwork(t, rootDir)
	a, _ := identity.New()
	b, _ := identity.New()
	mustMember(t, "issue", "-dir", rootDir, "-net", network, "-name", "gerät", "-out", filepath.Join(files, "a.cert"), a.ID.String())
	mustMember(t, "issue", "-dir", rootDir, "-net", network, "-name", "gerät", "-out", filepath.Join(files, "b.cert"), b.ID.String())
	if _, err := member(t, "revoke", "-dir", rootDir, "-net", network, "gerät"); err == nil || !strings.Contains(err.Error(), "peer-id") {
		t.Fatalf("mehrdeutiger name: %v", err)
	}
	if _, err := member(t, "revoke", "-dir", rootDir, "-net", network, "unbekannt"); err == nil {
		t.Fatal("unbekannter name gesperrt")
	}
	reg, _ := membership.OpenRegistry(membership.RegistryFile(rootDir, network))
	if len(reg.Revoked) != 0 || reg.Seq != 0 {
		t.Fatalf("buch verändert: %+v", reg)
	}
}

type noSensor struct{}

func (noSensor) Sample(context.Context) rem.HWSample {
	return rem.HWSample{At: time.Now(), GPUUtil: -1, GPUTempC: -1, GPUPowerW: -1, RAMAvailGiB: -1}
}

func testCfg(dir string) config.Config {
	c := config.Default()
	c.Node.DataDir = dir
	c.Node.Listen = []string{"/ip4/127.0.0.1/tcp/0"}
	c.Node.MDNS = false
	c.Node.AnnouncePrivate = true
	c.Resources.Schedule.Windows = nil
	c.Resources.Energy.Mode = "off"
	c.Resources.Hardware.RAMReserveGiB = 0
	return c
}

// TestJoinEndToEnd geht den ganzen Weg: Netz anlegen, Aussteller starten,
// Einladung erzeugen, mit `join` einlösen, aus der geschriebenen Konfiguration
// starten und zugelassen werden.
func TestJoinEndToEnd(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rootDir, issDir, files := t.TempDir(), t.TempDir(), t.TempDir()
	network, root := createNetwork(t, rootDir)
	issPeer := peerOf(t, issDir, network)
	cert := filepath.Join(files, "iss.cert")
	mustMember(t, "issue", "-dir", rootDir, "-net", network, "-role", "issuer", "-out", cert, issPeer)
	mustMember(t, "install", "-dir", issDir, "-root", root, cert)

	icfg := testCfg(issDir)
	icfg.Node.NetworkID, icfg.Node.Membership = network, &config.Membership{Root: root}
	if err := icfg.Validate(); err != nil {
		t.Fatal(err)
	}
	iss, err := node.New(ctx, icfg, node.Options{HW: noSensor{}})
	if err != nil {
		t.Fatal(err)
	}
	defer iss.Close()

	out := mustMember(t, "invite", "-dir", issDir, "-net", network, "-name", "notebook-3", "-peer", iss.P2P.Addrs()[0])
	inv, err := invite.Parse(strings.TrimSpace(out))
	if err != nil {
		t.Fatalf("link: %v (%s)", err, out)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := config.Save(path, testCfg(dir)); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := joinMember(path, inv, &buf); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("geschriebene konfiguration: %v", err)
	}
	if cfg.Node.NetworkID != network || cfg.Node.Membership == nil || cfg.Node.Membership.Root != root {
		t.Fatalf("konfiguration: %+v", cfg.Node)
	}
	n, err := node.New(ctx, cfg, node.Options{HW: noSensor{}})
	if err != nil {
		t.Fatal(err)
	}
	defer n.Close()
	deadline := time.Now().Add(15 * time.Second)
	for {
		ps := iss.P2P.Peers()
		if len(ps) == 1 && ps[0].Role == membership.RoleMember {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("nicht zugelassen: %+v", ps)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if out := mustMember(t, "list", "-dir", issDir, "-net", network); !strings.Contains(out, "notebook-3") {
		t.Fatalf("liste: %s", out)
	}

	// Derselbe Link wirkt kein zweites Mal.
	dir2 := t.TempDir()
	path2 := filepath.Join(dir2, "config.yaml")
	if err := config.Save(path2, testCfg(dir2)); err != nil {
		t.Fatal(err)
	}
	if err := joinMember(path2, inv, &buf); err == nil || !strings.Contains(err.Error(), "bereits benutzt") {
		t.Fatalf("zweiter beitritt: %v", err)
	}
	if fileExists(membership.CertFile(dir2, network)) {
		t.Fatal("zertifikat beim zweiten beitritt geschrieben")
	}
}

// Ein weiteres Netz neben einem bestehenden bekommt einen eigenen Port.
func TestInstallAddsSecondNetwork(t *testing.T) {
	rootDir, files, dir := t.TempDir(), t.TempDir(), t.TempDir()
	network, root := createNetwork(t, rootDir)
	path := filepath.Join(dir, "config.yaml")
	base := testCfg(dir)
	base.Node.NetworkID = "p-eigenes"
	base.Node.Listen = []string{"/ip4/0.0.0.0/tcp/4711"}
	if err := config.Save(path, base); err != nil {
		t.Fatal(err)
	}
	p := peerOf(t, dir, network)
	cert := filepath.Join(files, "n.cert")
	mustMember(t, "issue", "-dir", rootDir, "-net", network, "-out", cert, p)
	if _, err := member(t, "install", "-config", path, "-root", root, cert); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Node.NetworkID != "p-eigenes" || len(cfg.Node.Networks) != 1 || cfg.Node.Networks[0].NetworkID != network ||
		cfg.Node.Networks[0].Membership == nil || !strings.Contains(strings.Join(cfg.Node.Networks[0].Listen, " "), "4712") {
		t.Fatalf("konfiguration: %+v", cfg.Node)
	}
	if !fileExists(cfg.AllNetworks()[1].Membership.CertFile) {
		t.Fatalf("zertifikat fehlt: %+v", cfg.AllNetworks()[1].Membership)
	}
}

// Die Wurzel sperrt vom Wechseldatenträger, ein Knoten spielt die Liste ein.
func TestImportCRL(t *testing.T) {
	usb, nodeDir, files := t.TempDir(), t.TempDir(), t.TempDir()
	network, root := createNetwork(t, usb)
	_, otherRoot := createNetwork(t, t.TempDir())
	key := membership.RootKeyFile(usb, network)
	other, _ := identity.New()
	out := mustMember(t, "revoke", "-root-key", key, other.ID.String())
	if !strings.Contains(out, "import-crl") {
		t.Fatalf("hinweis fehlt: %s", out)
	}
	list := membership.CRLFile(usb, network)
	if _, err := member(t, "import-crl", "-dir", nodeDir, "-root", otherRoot, list); err == nil {
		t.Fatal("liste eines anderen netzes eingespielt")
	}
	mustMember(t, "import-crl", "-dir", nodeDir, "-root", root, list)
	found, _ := filepath.Glob(membership.CRLInbox(nodeDir, network))
	if len(found) != 1 {
		t.Fatalf("eingang: %v", found)
	}
	// Gefälschte Datei wird abgewiesen.
	bad := filepath.Join(files, "bad.json")
	raw, _ := os.ReadFile(list)
	if err := os.WriteFile(bad, bytes.Replace(raw, []byte(`"seq": 1`), []byte(`"seq": 2`), 1), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := member(t, "import-crl", "-dir", nodeDir, "-root", root, bad); err == nil {
		t.Fatal("veränderte liste eingespielt")
	}
}
