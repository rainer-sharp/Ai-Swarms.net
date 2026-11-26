// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Rainer Sestig. Kommerzielle Lizenz: siehe COMMERCIAL.md.

package node

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	lp2pproto "github.com/libp2p/go-libp2p/core/protocol"
	"github.com/libp2p/go-libp2p/p2p/protocol/circuitv2/client"

	"github.com/rainer-sharp/Ai-Swarms.net/internal/config"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/identity"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/membership"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/orchestrator"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/p2p"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/protocol"
)

// testNet ist ein Mitgliedsnetz für Tests.
type testNet struct {
	iss  *membership.Issuer
	root string
	id   string
}

func newTestNet(t *testing.T) testNet {
	t.Helper()
	key, _ := identity.New()
	iss, err := membership.NewRootIssuer(key)
	if err != nil {
		t.Fatal(err)
	}
	pub, _ := membership.RootKey(key)
	return testNet{iss, membership.EncodeRoot(pub), membership.NetworkID(pub)}
}

func (n testNet) chain(t *testing.T, id *identity.Identity, role string) []membership.Cert {
	t.Helper()
	c, err := n.iss.Issue(id.ID.String(), role, "", membership.NodeTTL, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// config richtet einen Knoten mit Zertifikat in diesem Netz ein.
func (n testNet) config(t *testing.T, id *identity.Identity, role string, extra func(*config.Config)) func(*config.Config) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "member.cert")
	if err := (membership.Bundle{V: 1, Root: n.root, Chain: n.chain(t, id, role)}).Write(path); err != nil {
		t.Fatal(err)
	}
	return func(c *config.Config) {
		c.Node.NetworkID = n.id
		c.Node.Membership = &config.Membership{Root: n.root, CertFile: path}
		c.Node.DNSSeeds = nil
		if extra != nil {
			extra(c)
		}
	}
}

// rawPeer ist ein libp2p-Knoten ohne AI-Swarms.net: ein Fremder, der von Hand spricht.
func rawPeer(t *testing.T, ctx context.Context, id *identity.Identity, target string) (host.Host, peer.ID) {
	t.Helper()
	h, err := libp2p.New(libp2p.Identity(id.Priv), libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"), libp2p.EnableRelay())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = h.Close() })
	ai, err := peer.AddrInfoFromString(target)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Connect(ctx, *ai); err != nil {
		t.Fatal(err)
	}
	return h, ai.ID
}

// readsNothing: Der Peer schickt auf diesem Protokoll keinerlei Daten.
func readsNothing(t *testing.T, ctx context.Context, h host.Host, p peer.ID, proto, send string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	s, err := h.NewStream(ctx, p, lp2pproto.ID(proto))
	if err != nil {
		return // gar nicht erst geöffnet
	}
	defer s.Close()
	_ = s.SetDeadline(time.Now().Add(5 * time.Second))
	if send != "" {
		_, _ = s.Write([]byte(send + "\n"))
	}
	buf := make([]byte, 256)
	if k, _ := s.Read(buf); k != 0 {
		t.Fatalf("%s lieferte daten: %q", proto, buf[:k])
	}
}

func hello(t *testing.T, ctx context.Context, h host.Host, p peer.ID, chain []membership.Cert) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	s, err := h.NewStream(ctx, p, lp2pproto.ID(protocol.ProtoMember))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	_ = s.SetDeadline(time.Now().Add(5 * time.Second))
	b, _ := json.Marshal(map[string]any{"chain": chain})
	_, _ = s.Write(append(b, '\n'))
	line, _ := bufio.NewReader(s).ReadBytes('\n')
	return line
}

func TestMembersWorkTogether(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	net := newTestNet(t)
	idA, _ := identity.New()
	idB, _ := identity.New()
	a := newTestNodeWith(t, ctx, idA, "mistral-small", "mistral", []string{"general"}, 32000, false, net.config(t, idA, membership.RoleMember, nil))
	b := newTestNodeWith(t, ctx, idB, "qwen2.5-coder:7b", "qwen", []string{"general"}, 32000, true, net.config(t, idB, membership.RoleMember, nil))
	if err := a.P2P.Connect(ctx, b.P2P.Addrs()[0]); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return len(a.Agents()) == 2 })
	if !b.P2P.Trusted(idA.ID) || !a.P2P.Trusted(idB.ID) {
		t.Fatal("zulassung nicht auf beiden seiten")
	}
	if _, err := a.Ask(ctx, orchestrator.Request{
		Messages: []protocol.ChatMessage{{Role: "user", Content: "hallo"}},
		Pattern:  orchestrator.PatternSingle, Agents: []string{"fake/qwen2.5-coder:7b"},
	}); err != nil {
		t.Fatal(err)
	}
	if b.calls.Load() != 1 {
		t.Fatalf("aufrufe bei b: %d", b.calls.Load())
	}
	st := a.Status()
	if st.Networks[0].Member == nil || st.Networks[0].Member.Role != membership.RoleMember || st.Networks[0].DHT != nil {
		t.Fatalf("status: %+v", st.Networks[0])
	}
}

// TestStrangersLearnNothing: Fremde bekommen keine Anzeige, keinen
// Peer-Austausch, keinen Auftrag und auf ihr Hallo keine Antwort.
func TestStrangersLearnNothing(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	net, other := newTestNet(t), newTestNet(t)
	idB, _ := identity.New()
	b := newTestNodeWith(t, ctx, idB, "qwen2.5-coder:7b", "qwen", []string{"general"}, 32000, true, net.config(t, idB, membership.RoleMember, nil))
	target := b.P2P.Addrs()[0]

	expired := func(t *testing.T, id *identity.Identity) []membership.Cert {
		c, err := net.iss.Issue(id.ID.String(), membership.RoleMember, "", time.Hour, time.Now().Add(-2*time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	foreignSubject, _ := identity.New()
	for _, c := range []struct {
		name  string
		chain func(*testing.T, *identity.Identity) []membership.Cert
	}{
		{"ohne zertifikat", func(*testing.T, *identity.Identity) []membership.Cert { return nil }},
		{"zertifikat eines anderen netzes", func(t *testing.T, id *identity.Identity) []membership.Cert {
			return other.chain(t, id, membership.RoleMember)
		}},
		{"zertifikat eines anderen peers", func(t *testing.T, _ *identity.Identity) []membership.Cert {
			return net.chain(t, foreignSubject, membership.RoleMember)
		}},
		{"abgelaufen", expired},
	} {
		t.Run(c.name, func(t *testing.T) {
			id, _ := identity.New()
			h, p := rawPeer(t, ctx, id, target)
			if line := hello(t, ctx, h, p, c.chain(t, id)); len(line) != 0 {
				t.Fatalf("antwort auf ungültiges hallo: %s", line)
			}
			for _, proto := range []string{protocol.ProtoCapability, protocol.ProtoPEX, protocol.ProtoMemberRenew} {
				readsNothing(t, ctx, h, p, proto, "")
			}
			readsNothing(t, ctx, h, p, protocol.ProtoMemberCRL, `{"lists":[]}`)
			readsNothing(t, ctx, h, p, protocol.ProtoTask, `{"type":"TASK_PROPOSE"}`)
			if slices.ContainsFunc(b.P2P.Peers(), func(i p2p.PeerInfo) bool { return i.PeerID == id.ID.String() && i.Role != "" }) {
				t.Fatal("fremder gilt als zugelassen")
			}
		})
	}
	if b.calls.Load() != 0 {
		t.Fatal("fremder auftrag ausgeführt")
	}

	// Gültiges Hallo bekommt eine Antwort, und der Austausch nennt keine Fremden.
	id, _ := identity.New()
	stranger, _ := identity.New()
	_, _ = rawPeer(t, ctx, stranger, target)
	h, p := rawPeer(t, ctx, id, target)
	line := hello(t, ctx, h, p, net.chain(t, id, membership.RoleMember))
	if !strings.Contains(string(line), idB.ID.String()) {
		t.Fatalf("antwort: %s", line)
	}
	s, err := h.NewStream(ctx, p, lp2pproto.ID(protocol.ProtoPEX))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	pex, _ := bufio.NewReader(s).ReadBytes('\n')
	if strings.Contains(string(pex), stranger.ID.String()) || !strings.Contains(string(pex), idB.ID.String()) {
		t.Fatalf("peer-austausch: %s", pex)
	}

	// Der Peer-Cache enthält das Mitglied, aber nie den Fremden.
	time.Sleep(200 * time.Millisecond) // Identify des Fremden abwarten
	cache := filepath.Join(b.Cfg.Node.DataDir, "peers-"+net.id+".json")
	_ = b.P2P.Close()
	data, err := os.ReadFile(cache)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), stranger.ID.String()) || !strings.Contains(string(data), id.ID.String()) {
		t.Fatalf("peer-cache: %s", data)
	}
}

func TestStrangerIsDisconnected(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	net := newTestNet(t)
	idB, _ := identity.New()
	b := newTestNodeWith(t, ctx, idB, "qwen2.5-coder:7b", "qwen", []string{"general"}, 32000, true, net.config(t, idB, membership.RoleMember, nil))
	id, _ := identity.New()
	h, p := rawPeer(t, ctx, id, b.P2P.Addrs()[0])
	deadline := time.Now().Add(15 * time.Second) // Frist für Fremde: 10 s
	for h.Network().Connectedness(p) == network.Connected {
		if time.Now().After(deadline) {
			t.Fatal("fremder bleibt verbunden")
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// TestHelperCarriesButGetsNoTasks: Über einen Helfer finden sich Mitglieder hinter
// NAT und arbeiten zusammen, der Helfer bekommt und erteilt keine Aufträge, und
// sein Relay dient Fremden nicht.
func TestHelperCarriesButGetsNoTasks(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	net := newTestNet(t)
	idH, _ := identity.New()
	idA, _ := identity.New()
	idC, _ := identity.New()
	h := newTestNodeWith(t, ctx, idH, "helfer-modell", "evil", []string{"general"}, 32000, true, net.config(t, idH, membership.RoleHelper, func(c *config.Config) {
		c.Node.RelayService = "true"
	}))
	static := []string{h.P2P.Addrs()[0]}
	behindNAT := func(c *config.Config) { c.Node.Listen, c.Node.StaticPeers = nil, static }
	a := newTestNodeWith(t, ctx, idA, "mistral-small", "mistral", []string{"general"}, 32000, false, net.config(t, idA, membership.RoleMember, behindNAT))
	c := newTestNodeWith(t, ctx, idC, "qwen2.5-coder:7b", "qwen", []string{"general"}, 32000, true, net.config(t, idC, membership.RoleMember, behindNAT))
	waitFor(t, func() bool { return len(a.P2P.Reservations()) == 1 && len(c.P2P.Reservations()) == 1 })
	waitFor(t, func() bool { return len(a.Agents()) == 2 }) // eigener Agent und der von C, nicht der des Helfers
	for _, ag := range a.Agents() {
		if ag.Family == "evil" {
			t.Fatalf("helfer als kandidat: %+v", ag)
		}
	}
	if a.P2P.Trusted(idH.ID) {
		t.Fatal("helfer gilt als vertraut")
	}
	if _, err := a.Ask(ctx, orchestrator.Request{
		Messages: []protocol.ChatMessage{{Role: "user", Content: "hallo"}},
		Pattern:  orchestrator.PatternSingle, Agents: []string{"fake/qwen2.5-coder:7b"},
	}); err != nil {
		t.Fatal(err)
	}
	if c.calls.Load() != 1 || h.calls.Load() != 0 {
		t.Fatalf("aufrufe: c %d, helfer %d", c.calls.Load(), h.calls.Load())
	}

	// Auch ein öffentlicher Auftrag geht nicht an den Helfer.
	if _, err := a.Ask(ctx, orchestrator.Request{
		Messages: []protocol.ChatMessage{{Role: "user", Content: "hallo"}},
		Pattern:  orchestrator.PatternSingle, Privacy: protocol.PrivacyPublic,
	}); err != nil {
		t.Fatal(err)
	}
	if h.calls.Load() != 0 {
		t.Fatal("öffentlicher auftrag beim helfer")
	}

	// Der Helfer als Koordinator wird abgewiesen.
	if _, err := h.Ask(ctx, orchestrator.Request{
		Messages: []protocol.ChatMessage{{Role: "user", Content: "hallo"}},
		Pattern:  orchestrator.PatternSingle, Agents: []string{"fake/qwen2.5-coder:7b"},
	}); err == nil {
		t.Fatal("helfer konnte einen auftrag erteilen")
	}
	if c.calls.Load() != 1 {
		t.Fatal("auftrag des helfers ausgeführt")
	}
	// Ein Peer mit Rolle helper fragt selbst bei einem Mitglied nach.
	raw, _ := identity.New()
	rh, rp := rawPeer(t, ctx, raw, h.P2P.Addrs()[0])
	if _, err := client.Reserve(ctx, rh, peer.AddrInfo{ID: rp}); err == nil {
		t.Fatal("relay reserviert für einen fremden")
	}
	if line := hello(t, ctx, rh, rp, net.chain(t, raw, membership.RoleHelper)); len(line) == 0 {
		t.Fatal("helfer nicht zugelassen")
	}
	readsNothing(t, ctx, rh, rp, protocol.ProtoCapability, "")
	readsNothing(t, ctx, rh, rp, protocol.ProtoTask, `{"type":"TASK_PROPOSE"}`)

	// Auch ein zugelassenes Mitglied kann dem Helfer keinen Auftrag schicken:
	// Sein Knoten bietet das Protokoll gar nicht an, trotz provide und Modell.
	mid, _ := identity.New()
	mh, mp := rawPeer(t, ctx, mid, h.P2P.Addrs()[0])
	if line := hello(t, ctx, mh, mp, net.chain(t, mid, membership.RoleMember)); len(line) == 0 {
		t.Fatal("mitglied nicht zugelassen")
	}
	if s, err := mh.NewStream(ctx, mp, lp2pproto.ID(protocol.ProtoTask)); err == nil {
		_ = s.Reset()
		t.Fatal("helfer bietet das auftragsprotokoll an")
	}
}

func TestMemberCertProblems(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	net, other := newTestNet(t), newTestNet(t)
	id, _ := identity.New()
	newCfg := func(mod func(*config.Config)) config.Config {
		cfg := config.Default()
		cfg.Node.DataDir = t.TempDir()
		cfg.Node.Listen = []string{"/ip4/127.0.0.1/tcp/0"}
		cfg.Node.MDNS = false
		cfg.Node.AnnouncePrivate = true
		cfg.Node.DNSSeeds = []string{"none"}
		cfg.Resources.Energy.Mode = "off"
		mod(&cfg)
		return cfg
	}
	expiredPath := filepath.Join(t.TempDir(), "alt.cert")
	old, err := net.iss.Issue(id.ID.String(), membership.RoleMember, "", time.Hour, time.Now().Add(-3*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if err := (membership.Bundle{V: 1, Root: net.root, Chain: old}).Write(expiredPath); err != nil {
		t.Fatal(err)
	}
	// Als einziges Netz: Der Knoten startet nicht und nennt den Grund.
	first := newCfg(func(c *config.Config) {
		c.Node.NetworkID, c.Node.Membership = net.id, &config.Membership{Root: net.root, CertFile: expiredPath}
		c.Node.DNSSeeds = nil
	})
	if _, err := New(ctx, first, Options{Identity: id, HW: noHW{}}); err == nil || !strings.Contains(err.Error(), "abgelaufen") {
		t.Fatalf("abgelaufenes zertifikat: %v", err)
	}
	// Datei gehört zu einem anderen Wurzelschlüssel als die Konfiguration.
	wrong := newCfg(func(c *config.Config) {
		c.Node.NetworkID, c.Node.Membership = other.id, &config.Membership{Root: other.root, CertFile: expiredPath}
		c.Node.DNSSeeds = nil
	})
	if _, err := New(ctx, wrong, Options{Identity: id, HW: noHW{}}); err == nil || !strings.Contains(err.Error(), "wurzelschlüssel") {
		t.Fatalf("falsche wurzel: %v", err)
	}
	// Als weiteres Netz: Das andere Netz läuft weiter, der Status nennt den Fehler.
	second := newCfg(func(c *config.Config) {
		c.Node.Networks = []config.Network{{NetworkID: net.id, Listen: []string{"/ip4/127.0.0.1/tcp/0"}, Membership: &config.Membership{Root: net.root, CertFile: expiredPath}}}
	})
	if err := second.Validate(); err != nil {
		t.Fatal(err)
	}
	n, err := New(ctx, second, Options{Identity: id, HW: noHW{}})
	if err != nil {
		t.Fatal(err)
	}
	defer n.Close()
	if st := n.Status(); len(st.NetErrors) != 1 || len(st.Networks) != 1 {
		t.Fatalf("status: %+v", st)
	}
}

// issuerNode startet einen Aussteller mit Dateien am Standardort.
func issuerNode(t *testing.T, ctx context.Context, net testNet, extra func(*config.Config)) (testNode, string) {
	t.Helper()
	id, _ := identity.New()
	dir := t.TempDir()
	if err := id.Create(membership.NodeKeyFile(dir, net.id)); err != nil {
		t.Fatal(err)
	}
	if err := (membership.Bundle{V: 1, Root: net.root, Chain: net.chain(t, id, membership.RoleIssuer)}).Write(membership.CertFile(dir, net.id)); err != nil {
		t.Fatal(err)
	}
	n := newTestNodeWith(t, ctx, id, "mistral-small", "mistral", []string{"general"}, 32000, true, func(c *config.Config) {
		c.Node.DataDir = dir
		c.Node.NetworkID = net.id
		c.Node.Membership = &config.Membership{Root: net.root}
		c.Node.DNSSeeds = nil
		if extra != nil {
			extra(c)
		}
	})
	return n, dir
}

func newInvite(t *testing.T, dir, network, role string) string {
	t.Helper()
	a, err := membership.OpenAdmin(dir, network, "", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	tok, err := a.Registry.NewInvite(role, "", 0, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

func TestInviteRedeem(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	net := newTestNet(t)
	iss, dir := issuerNode(t, ctx, net, nil)
	addrs := iss.P2P.Addrs()
	issuer := iss.ID.ID.String()
	tok := newInvite(t, dir, net.id, membership.RoleMember)
	idA, _ := identity.New()
	chain, err := p2p.RedeemInvite(ctx, idA, addrs, issuer, tok)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := mustNet(t, net).Admit(chain, idA.ID.String(), time.Now()); err != nil || got.Role != membership.RoleMember {
		t.Fatalf("%+v %v", got, err)
	}
	// Abgefangener Link: Der Zweite scheitert sichtbar.
	idB, _ := identity.New()
	if _, err := p2p.RedeemInvite(ctx, idB, addrs, issuer, tok); err == nil || !strings.Contains(err.Error(), "bereits benutzt") {
		t.Fatalf("zweites einlösen: %v", err)
	}
	if _, err := p2p.RedeemInvite(ctx, idB, addrs, issuer, "erfunden"); err == nil || !strings.Contains(err.Error(), "ungültig") {
		t.Fatalf("erfundenes token: %v", err)
	}
	// Ein Mitglied ohne Ausstellerrechte weist Einladungen ab, ohne etwas zu verraten.
	idM, _ := identity.New()
	m := newTestNodeWith(t, ctx, idM, "m", "m", []string{"general"}, 32000, false, net.config(t, idM, membership.RoleMember, nil))
	if _, err := p2p.RedeemInvite(ctx, idB, m.P2P.Addrs(), idM.ID.String(), newInvite(t, dir, net.id, membership.RoleMember)); err == nil || !strings.Contains(err.Error(), "ungültig") {
		t.Fatalf("mitglied als aussteller: %v", err)
	}
}

// TestInviteThroughHelper: Der Aussteller sitzt hinter NAT, der Neue erreicht ihn
// nur über den Helfer und tritt danach dem Netz bei. Zu einem einfachen Mitglied
// lässt das Relay einen Fremden nicht durch.
func TestInviteThroughHelper(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	net := newTestNet(t)
	idH, _ := identity.New()
	h := newTestNodeWith(t, ctx, idH, "-", "-", nil, 0, false, net.config(t, idH, membership.RoleHelper, func(c *config.Config) {
		c.Node.RelayService = "true"
	}))
	static := []string{h.P2P.Addrs()[0]}
	behindNAT := func(c *config.Config) { c.Node.Listen, c.Node.StaticPeers = nil, static }
	iss, dir := issuerNode(t, ctx, net, behindNAT)
	idM, _ := identity.New()
	m := newTestNodeWith(t, ctx, idM, "qwen2.5-coder:7b", "qwen", []string{"general"}, 32000, true, net.config(t, idM, membership.RoleMember, behindNAT))
	waitFor(t, func() bool { return len(iss.P2P.Reservations()) == 1 && len(m.P2P.Reservations()) == 1 })
	circuit := func(n testNode) []string {
		var out []string
		for _, a := range n.P2P.Addrs() {
			if strings.Contains(a, "p2p-circuit") {
				out = append(out, a)
			}
		}
		return out
	}
	waitFor(t, func() bool { return len(circuit(iss)) > 0 && len(circuit(m)) > 0 })

	stranger, _ := identity.New()
	if _, err := p2p.RedeemInvite(ctx, stranger, circuit(m), idM.ID.String(), "egal"); err == nil || !strings.Contains(err.Error(), "nicht erreichbar") {
		t.Fatalf("relay zu einem mitglied: %v", err)
	}
	tok := newInvite(t, dir, net.id, membership.RoleMember)
	chain, err := p2p.RedeemInvite(ctx, stranger, circuit(iss), iss.ID.ID.String(), tok)
	if err != nil {
		t.Fatalf("einladung über den helfer: %v", err)
	}
	path := filepath.Join(t.TempDir(), "neu.cert")
	if err := (membership.Bundle{V: 1, Root: net.root, Chain: chain}).Write(path); err != nil {
		t.Fatal(err)
	}
	joined := newTestNodeWith(t, ctx, stranger, "neu", "neu", []string{"general"}, 32000, false, func(c *config.Config) {
		c.Node.NetworkID = net.id
		c.Node.Membership = &config.Membership{Root: net.root, CertFile: path}
		c.Node.DNSSeeds = nil
		behindNAT(c)
	})
	waitFor(t, func() bool { return len(joined.Agents()) >= 2 })
}

func mustNet(t *testing.T, n testNet) *membership.Network {
	t.Helper()
	root, err := membership.DecodeRoot(n.root)
	if err != nil {
		t.Fatal(err)
	}
	nw, err := membership.NewNetwork(n.id, root)
	if err != nil {
		t.Fatal(err)
	}
	return nw
}

// TestToolListsAcrossNetworks: Werkzeuglisten wirken über echte Verbindungen.
func TestToolListsAcrossNetworks(t *testing.T) {
	const tools = `[{"type":"function","function":{"name":"read_file"}},{"type":"function","function":{"name":"run_command"}}]`
	ask := func(ctx context.Context, n testNode, privacy protocol.PrivacyClass) (*orchestrator.Answer, error) {
		return n.Ask(ctx, orchestrator.Request{
			Messages: []protocol.ChatMessage{{Role: "user", Content: "Lies main.go"}},
			Privacy:  privacy, Tools: json.RawMessage(tools),
		})
	}
	withTools := func(c *config.Config) { c.Backends[0].Models[0].Tools = true }

	t.Run("mitgliedsnetz ohne liste", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		net := newTestNet(t)
		idA, _ := identity.New()
		idB, _ := identity.New()
		a := newTestNodeWith(t, ctx, idA, "m", "m", []string{"general"}, 32000, false, net.config(t, idA, membership.RoleMember, nil))
		b := newTestNodeWith(t, ctx, idB, "qwen", "qwen", []string{"general"}, 32000, true, net.config(t, idB, membership.RoleMember, withTools))
		if err := a.P2P.Connect(ctx, b.P2P.Addrs()[0]); err != nil {
			t.Fatal(err)
		}
		waitFor(t, func() bool { return len(a.Agents()) == 2 })
		if _, err := ask(ctx, a, protocol.PrivacyTrusted); !errors.Is(err, orchestrator.ErrToolsNotAllowed) {
			t.Fatalf("fehler %v", err)
		}
		if b.calls.Load() != 0 {
			t.Fatal("werkzeuge gingen an ein mitglied ohne freigabe")
		}
	})
	t.Run("mitgliedsnetz mit liste", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		net := newTestNet(t)
		idA, _ := identity.New()
		idB, _ := identity.New()
		a := newTestNodeWith(t, ctx, idA, "m", "m", []string{"general"}, 32000, false, net.config(t, idA, membership.RoleMember, func(c *config.Config) {
			c.Node.AllowedTools = []string{"read_file"} // die Testantwort ruft read_file auf
		}))
		b := newTestNodeWith(t, ctx, idB, "qwen", "qwen", []string{"general"}, 32000, true, net.config(t, idB, membership.RoleMember, withTools))
		if err := a.P2P.Connect(ctx, b.P2P.Addrs()[0]); err != nil {
			t.Fatal(err)
		}
		waitFor(t, func() bool { return len(a.Agents()) == 2 })
		ans, err := ask(ctx, a, protocol.PrivacyTrusted)
		if err != nil || len(ans.ToolCalls) != 1 || ans.ToolCalls[0].Function.Name != "read_file" {
			t.Fatalf("%+v %v", ans, err)
		}
	})
	t.Run("offenes netz", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		idA, _ := identity.New()
		idB, _ := identity.New()
		open := func(c *config.Config) { c.Resources.AcceptPublicTasks = true; withTools(c) }
		// A gibt nur ein Werkzeug frei, das die Testantwort nicht aufruft: Der
		// erfundene Aufruf von read_file darf nicht durchkommen.
		a := newTestNodeWith(t, ctx, idA, "m", "m", []string{"general"}, 32000, false, func(c *config.Config) {
			c.Node.AllowedTools = []string{"run_command"}
		})
		b := newTestNodeWith(t, ctx, idB, "qwen", "qwen", []string{"general"}, 32000, true, open)
		if err := a.P2P.Connect(ctx, b.P2P.Addrs()[0]); err != nil {
			t.Fatal(err)
		}
		waitFor(t, func() bool { return len(a.Agents()) == 2 })
		ans, err := ask(ctx, a, protocol.PrivacyPublic)
		if err == nil || !strings.Contains(err.Error(), "nicht freigegebenes werkzeug") || len(ans.ToolCalls) != 0 {
			t.Fatalf("%+v %v", ans, err)
		}
		if b.calls.Load() != 1 {
			t.Fatalf("aufrufe bei b: %d", b.calls.Load())
		}
		// Vertraulicher Auftrag geht ohnehin nicht an einen Fremden.
		if _, err := ask(ctx, a, protocol.PrivacyTrusted); !errors.Is(err, orchestrator.ErrToolsNotAllowed) {
			t.Fatalf("fehler %v", err)
		}
	})
}

// revoke sperrt einen Peer so wie `member revoke`: Buch und Datei beim Aussteller.
func revoke(t *testing.T, dir, network string, p peer.ID) membership.CRL {
	t.Helper()
	a, err := membership.OpenAdmin(dir, network, "", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	l, err := a.Registry.Revoke(a.Iss, p.String(), "test", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := membership.WriteCRL(membership.CRLFile(dir, network), l); err != nil {
		t.Fatal(err)
	}
	return l
}

func hasAgentOf(n testNode, p peer.ID) bool {
	return slices.ContainsFunc(n.Agents(), func(a orchestrator.Agent) bool { return a.PeerID == p.String() })
}

func peerRole(n testNode, p peer.ID) string {
	for _, i := range n.P2P.Peers() {
		if i.PeerID == p.String() {
			return i.Role
		}
	}
	return ""
}

// TestRevocationReachesEveryone: Der Widerruf wirkt, ohne die anderen anzufassen:
// Alle trennen den Gesperrten, er kommt nicht zurück, auch nicht nach einem Neustart.
func TestRevocationReachesEveryone(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	net := newTestNet(t)
	iss, dir := issuerNode(t, ctx, net, nil)
	addr := iss.P2P.Addrs()[0]
	static := func(c *config.Config) { c.Node.StaticPeers = []string{addr} }
	idA, _ := identity.New()
	idB, _ := identity.New()
	dirA, dirB := t.TempDir(), t.TempDir()
	cfgA := net.config(t, idA, membership.RoleMember, func(c *config.Config) { static(c); c.Node.DataDir = dirA })
	a := newTestNodeWith(t, ctx, idA, "m", "m", []string{"general"}, 32000, false, cfgA)
	b := newTestNodeWith(t, ctx, idB, "qwen", "qwen", []string{"general"}, 32000, true, net.config(t, idB, membership.RoleMember, func(c *config.Config) { static(c); c.Node.DataDir = dirB }))
	waitFor(t, func() bool {
		return peerRole(a, idB.ID) == membership.RoleMember && peerRole(iss, idB.ID) == membership.RoleMember
	})
	waitFor(t, func() bool { return hasAgentOf(a, idB.ID) })

	l := revoke(t, dir, net.id, idB.ID)
	waitFor(t, func() bool { return peerRole(iss, idB.ID) == "" && peerRole(a, idB.ID) == "" && !hasAgentOf(a, idB.ID) })
	// Der Gesperrte weiß es selbst und nimmt nicht mehr teil: Er wählt niemanden
	// neu an, auch nicht, wenn sich jemand bei ihm meldet.
	waitFor(t, func() bool { return b.P2P.MemberInfo().Revoked })
	waitFor(t, func() bool { return len(b.P2P.Peers()) == 0 })
	// Gezählt werden die Verbindungsversuche: Wer neu anwählt und sofort wieder
	// getrennt wird, fiele bei einem Blick auf die Peers nicht auf.
	var redials, dials atomic.Int32
	iss.P2P.Host().Network().Notify(&network.NotifyBundle{ConnectedF: func(_ network.Network, c network.Conn) {
		if c.RemotePeer() == idB.ID {
			if c.Stat().Direction == network.DirOutbound {
				dials.Add(1)
			} else {
				redials.Add(1)
			}
		}
	}})
	time.Sleep(1500 * time.Millisecond)
	if got := redials.Load(); got != 0 {
		t.Fatalf("gesperrter wählt neu an: %d versuche", got)
	}
	if got := dials.Load(); got != 0 {
		t.Fatalf("aussteller wählt den gesperrten an: %d versuche", got)
	}
	if st := b.Status(); !slices.ContainsFunc(st.Networks[0].Hints, func(h string) bool { return strings.Contains(h, "entfernt") }) {
		t.Fatalf("hinweis fehlt: %+v", st.Networks[0].Hints)
	}
	// A kennt die Liste, und seine Verbindung zum Aussteller bleibt.
	if a.P2P.MemberInfo().CRLs[membership.IssuerRoot] != 0 || peerRole(a, iss.ID.ID) != membership.RoleIssuer {
		t.Fatalf("a: %+v", a.P2P.MemberInfo())
	}
	waitFor(t, func() bool { return a.P2P.MemberInfo().CRLs[l.Issuer] == l.Seq })

	// Auch beim erneuten Anwählen wird er nicht zugelassen.
	if err := b.P2P.Connect(ctx, addr); err == nil {
		t.Fatal("gesperrter knoten verbindet sich als mitglied")
	}

	// Nach einem Neustart gilt die Sperre weiter, ohne jede Verbindung, und der
	// Gesperrte steht nicht mehr im Peer-Cache.
	_ = a.Close()
	if data, err := os.ReadFile(filepath.Join(dirA, "peers-"+net.id+".json")); err != nil || strings.Contains(string(data), idB.ID.String()) {
		t.Fatalf("peer-cache: %v %s", err, data)
	}
	again := newTestNodeWith(t, ctx, idA, "m", "m", []string{"general"}, 32000, false, func(c *config.Config) { cfgA(c); c.Node.StaticPeers = nil })
	if got := again.P2P.MemberInfo().CRLs[l.Issuer]; got != l.Seq {
		t.Fatalf("sperrliste nach neustart: %d", got)
	}
	// Der Gesperrte selbst startet nicht mehr in diesem Netz.
	_ = b.Close()
	cfg := config.Default()
	cfg.Node.DataDir = dirB
	cfg.Node.Listen = []string{"/ip4/127.0.0.1/tcp/0"}
	cfg.Node.MDNS, cfg.Node.AnnouncePrivate = false, true
	cfg.Resources.Energy.Mode = "off"
	net.config(t, idB, membership.RoleMember, nil)(&cfg)
	cfg.Node.DataDir = dirB
	if _, err := New(ctx, cfg, Options{Identity: idB, HW: noHW{}}); !errors.Is(err, membership.ErrRevoked) {
		t.Fatalf("start nach widerruf: %v", err)
	}
}

// TestCRLCatchUpOnConnect: Wer während des Widerrufs nicht verbunden war, erfährt
// ihn bei der nächsten Zulassung und trennt den Gesperrten.
func TestCRLCatchUpOnConnect(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	net := newTestNet(t)
	iss, dir := issuerNode(t, ctx, net, nil)
	idA, _ := identity.New()
	idB, _ := identity.New()
	a := newTestNodeWith(t, ctx, idA, "m", "m", []string{"general"}, 32000, false, net.config(t, idA, membership.RoleMember, nil))
	b := newTestNodeWith(t, ctx, idB, "qwen", "qwen", []string{"general"}, 32000, true, net.config(t, idB, membership.RoleMember, nil))
	// A und B kennen sich, der Aussteller noch keinen von beiden.
	if err := a.P2P.Connect(ctx, b.P2P.Addrs()[0]); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return peerRole(a, idB.ID) == membership.RoleMember })
	l := revoke(t, dir, net.id, idB.ID)
	waitFor(t, func() bool { return iss.P2P.MemberInfo().CRLs[l.Issuer] == l.Seq })
	if peerRole(a, idB.ID) != membership.RoleMember {
		t.Fatal("a kann es noch nicht wissen")
	}
	if err := a.P2P.Connect(ctx, iss.P2P.Addrs()[0]); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return peerRole(a, idB.ID) == "" && a.P2P.MemberInfo().CRLs[l.Issuer] == l.Seq })
}

// TestRevokedTaskAborts: Der laufende Auftrag eines Gesperrten bricht ab.
func TestRevokedTaskAborts(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	net := newTestNet(t)
	iss, dir := issuerNode(t, ctx, net, nil)
	started, aborted := make(chan struct{}), make(chan struct{})
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]string{{"id": "qwen"}}})
			return
		}
		// Erst den Rumpf lesen: Der HTTP-Server bemerkt einen Abbruch sonst nicht.
		_, _ = io.Copy(io.Discard, r.Body)
		close(started)
		<-r.Context().Done()
		close(aborted)
	}))
	defer slow.Close()
	defer slow.CloseClientConnections() // sonst wartet Close bei einem Fehlschlag ewig
	idA, _ := identity.New()
	idB, _ := identity.New()
	addr := iss.P2P.Addrs()[0]
	a := newTestNodeWith(t, ctx, idA, "m", "m", []string{"general"}, 32000, false, net.config(t, idA, membership.RoleMember, func(c *config.Config) { c.Node.StaticPeers = []string{addr} }))
	newTestNodeWith(t, ctx, idB, "qwen", "qwen", []string{"general"}, 32000, true, net.config(t, idB, membership.RoleMember, func(c *config.Config) {
		c.Node.StaticPeers = []string{addr}
		c.Backends[0].URL = slow.URL + "/v1"
	}))
	waitFor(t, func() bool { return hasAgentOf(a, idB.ID) })
	go func() {
		_, _ = a.Ask(ctx, orchestrator.Request{
			Messages: []protocol.ChatMessage{{Role: "user", Content: "hallo"}},
			Pattern:  orchestrator.PatternSingle, Agents: []string{"fake/qwen"},
		})
	}()
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("auftrag kam nicht an")
	}
	revoke(t, dir, net.id, idA.ID)
	select {
	case <-aborted:
	case <-time.After(15 * time.Second):
		t.Fatal("auftrag des gesperrten läuft weiter")
	}
}

// TestForgedAndOldCRLsHaveNoEffect: Ein zugelassener Peer kann niemanden mit einer
// gefälschten Liste sperren und keine Sperre mit einer alten Liste aufheben.
func TestForgedAndOldCRLsHaveNoEffect(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	net := newTestNet(t)
	idB, _ := identity.New()
	b := newTestNodeWith(t, ctx, idB, "qwen", "qwen", []string{"general"}, 32000, true, net.config(t, idB, membership.RoleMember, nil))
	raw, _ := identity.New()
	h, p := rawPeer(t, ctx, raw, b.P2P.Addrs()[0])
	if line := hello(t, ctx, h, p, net.chain(t, raw, membership.RoleMember)); len(line) == 0 {
		t.Fatal("nicht zugelassen")
	}
	push := func(l membership.CRL) {
		t.Helper()
		s, err := h.NewStream(ctx, p, lp2pproto.ID(protocol.ProtoMemberCRL))
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		_ = s.SetDeadline(time.Now().Add(5 * time.Second))
		enc, _ := json.Marshal(map[string]any{"lists": []membership.CRL{l}})
		_, _ = s.Write(append(enc, '\n'))
		_, _ = bufio.NewReader(s).ReadBytes('\n')
	}
	victim, _ := identity.New()
	// Ein anderes Netz signiert, als Wurzel ausgegeben.
	other := newTestNet(t)
	forged, _ := other.iss.SignCRL(9, []membership.Revocation{{Subject: idB.ID.String(), RevokedAt: time.Now()}}, time.Now())
	forged.NetworkID = net.id
	push(forged)
	if b.P2P.MemberInfo().Revoked || len(b.P2P.MemberInfo().CRLs) != 0 {
		t.Fatalf("gefälschte liste wirkt: %+v", b.P2P.MemberInfo())
	}
	// Echte Liste Nummer 2 sperrt, ältere Nummer 1 hebt nichts auf.
	newList, _ := net.iss.SignCRL(2, []membership.Revocation{{Subject: victim.ID.String(), RevokedAt: time.Now()}}, time.Now())
	push(newList)
	waitFor(t, func() bool { return b.P2P.MemberInfo().CRLs[membership.IssuerRoot] == 2 })
	old, _ := net.iss.SignCRL(1, nil, time.Now())
	push(old)
	time.Sleep(100 * time.Millisecond)
	if got := b.P2P.MemberInfo().CRLs[membership.IssuerRoot]; got != 2 {
		t.Fatalf("alte liste übernommen: %d", got)
	}
	if line := hello(t, ctx, h, p, net.chain(t, victim, membership.RoleMember)); len(line) != 0 {
		t.Fatal("zertifikat fremder peer-id zugelassen")
	}
}

// TestRenewal: Ab der Hälfte der Laufzeit bekommt ein Knoten ein neues Zertifikat
// mit derselben Rolle, und die Zugelassenen kennen es danach.
func TestRenewal(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	net := newTestNet(t)
	iss, dir := issuerNode(t, ctx, net, nil)
	idA, _ := identity.New()
	old, err := net.iss.Issue(idA.ID.String(), membership.RoleHelper, "server-1", membership.NodeTTL, time.Now().Add(-20*24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "member.cert")
	if err := (membership.Bundle{V: 1, Root: net.root, Chain: old}).Write(path); err != nil {
		t.Fatal(err)
	}
	a := newTestNodeWith(t, ctx, idA, "-", "-", nil, 0, false, func(c *config.Config) {
		c.Node.NetworkID, c.Node.DNSSeeds = net.id, nil
		c.Node.Membership = &config.Membership{Root: net.root, CertFile: path}
	})
	if err := a.P2P.RenewMembership(ctx); err == nil {
		t.Fatal("verlängert ohne aussteller")
	}
	if err := a.P2P.Connect(ctx, iss.P2P.Addrs()[0]); err == nil {
		t.Fatal("helfer bekam eine fähigkeitsanzeige") // Connect holt die Anzeige, ein helper bekommt keine
	}
	waitFor(t, func() bool {
		return peerRole(a, iss.ID.ID) == membership.RoleIssuer && peerRole(iss, idA.ID) == membership.RoleHelper
	})
	before := a.P2P.MemberInfo().ExpiresAt
	if err := a.P2P.RenewMembership(ctx); err != nil {
		t.Fatal(err)
	}
	if !a.P2P.MemberInfo().ExpiresAt.After(before.Add(10 * 24 * time.Hour)) {
		t.Fatalf("läuft bis %v", a.P2P.MemberInfo().ExpiresAt)
	}
	b, err := membership.ReadBundle(path)
	if err != nil || b.Leaf().Role != membership.RoleHelper || b.Leaf().Name != "server-1" || !b.Leaf().ExpiresAt.After(before) {
		t.Fatalf("datei: %+v %v", b, err)
	}
	reg, _ := membership.OpenRegistry(membership.RegistryFile(dir, net.id))
	if len(reg.Issued) != 1 || reg.Issued[0].Subject != idA.ID.String() {
		t.Fatalf("buch: %+v", reg.Issued)
	}
	// Ein gesperrter Knoten wird nicht verlängert.
	revoke(t, dir, net.id, idA.ID)
	waitFor(t, func() bool { return a.P2P.MemberInfo().Revoked })
	if err := a.P2P.RenewMembership(ctx); err == nil {
		t.Fatal("gesperrter knoten verlängert")
	}
}

const farewellLimit = 4 * time.Second

// TestRevokedPeerIsCutOff: Auch ein Gesperrter, der sich nicht an die Regeln hält,
// verliert die Verbindung und bekommt nichts mehr.
func TestRevokedPeerIsCutOff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	net := newTestNet(t)
	idB, _ := identity.New()
	b := newTestNodeWith(t, ctx, idB, "qwen", "qwen", []string{"general"}, 32000, true, net.config(t, idB, membership.RoleMember, nil))
	raw, _ := identity.New()
	h, p := rawPeer(t, ctx, raw, b.P2P.Addrs()[0])
	if line := hello(t, ctx, h, p, net.chain(t, raw, membership.RoleMember)); len(line) == 0 {
		t.Fatal("nicht zugelassen")
	}
	waitFor(t, func() bool { return peerRole(b, raw.ID) == membership.RoleMember })
	l, _ := net.iss.SignCRL(1, []membership.Revocation{{Subject: raw.ID.String(), RevokedAt: time.Now()}}, time.Now())
	s, err := h.NewStream(ctx, p, lp2pproto.ID(protocol.ProtoMemberCRL))
	if err != nil {
		t.Fatal(err)
	}
	enc, _ := json.Marshal(map[string]any{"lists": []membership.CRL{l}})
	_, _ = s.Write(append(enc, '\n'))
	// Deutlich vor der Frist für Fremde (10 s), die ihn sonst ebenfalls träfe.
	deadline := time.Now().Add(farewellLimit)
	for h.Network().Connectedness(p) == network.Connected {
		if time.Now().After(deadline) {
			t.Fatal("gesperrter bleibt verbunden")
		}
		time.Sleep(50 * time.Millisecond)
	}
	_ = s.Close()
	// Bei erneutem Anwählen gibt es keine Antwort mehr.
	h2, p2 := rawPeer(t, ctx, raw, b.P2P.Addrs()[0])
	if line := hello(t, ctx, h2, p2, net.chain(t, raw, membership.RoleMember)); len(line) != 0 {
		t.Fatalf("antwort an den gesperrten: %s", line)
	}
}

// TestBadRenewalIsRejected: Ein böswilliger oder fehlerhafter Aussteller kann einem
// Knoten keine falsche Rolle, keine fremde Kette und keine kürzere Laufzeit unterschieben.
func TestBadRenewalIsRejected(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	net, other := newTestNet(t), newTestNet(t)
	idA, _ := identity.New()
	stranger, _ := identity.New()
	start := time.Now().Add(-20 * 24 * time.Hour)
	old, err := net.iss.Issue(idA.ID.String(), membership.RoleHelper, "", membership.NodeTTL, start)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "member.cert")
	if err := (membership.Bundle{V: 1, Root: net.root, Chain: old}).Write(path); err != nil {
		t.Fatal(err)
	}
	a := newTestNodeWith(t, ctx, idA, "-", "-", nil, 0, false, func(c *config.Config) {
		c.Node.NetworkID, c.Node.DNSSeeds = net.id, nil
		c.Node.Membership = &config.Membership{Root: net.root, CertFile: path}
	})
	issue := func(iss *membership.Issuer, subject, role string, ttl time.Duration, at time.Time) []membership.Cert {
		c, err := iss.Issue(subject, role, "", ttl, at)
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	self := idA.ID.String()
	tampered := issue(net.iss, self, membership.RoleHelper, membership.NodeTTL, time.Now())
	tampered[0].ExpiresAt = tampered[0].ExpiresAt.Add(time.Hour)

	rawIss, _ := identity.New()
	h, p := rawPeer(t, ctx, rawIss, a.P2P.Addrs()[0])
	if line := hello(t, ctx, h, p, net.chain(t, rawIss, membership.RoleIssuer)); len(line) == 0 {
		t.Fatal("aussteller nicht zugelassen")
	}
	waitFor(t, func() bool { return peerRole(a, rawIss.ID) == membership.RoleIssuer })
	for _, c := range []struct {
		name  string
		chain []membership.Cert
	}{
		{"andere rolle", issue(net.iss, self, membership.RoleMember, membership.NodeTTL, time.Now())},
		{"aussteller als rolle", issue(net.iss, self, membership.RoleIssuer, membership.NodeTTL, time.Now())},
		{"fremder peer", issue(net.iss, stranger.ID.String(), membership.RoleHelper, membership.NodeTTL, time.Now())},
		{"anderes netz", issue(other.iss, self, membership.RoleHelper, membership.NodeTTL, time.Now())},
		{"kürzer als bisher", issue(net.iss, self, membership.RoleHelper, time.Hour, time.Now())},
		{"verändert", tampered},
	} {
		t.Run(c.name, func(t *testing.T) {
			h.SetStreamHandler(lp2pproto.ID(protocol.ProtoMemberRenew), func(s network.Stream) {
				defer s.Close()
				b, _ := json.Marshal(map[string]any{"chain": c.chain})
				_, _ = s.Write(append(b, '\n'))
			})
			before, _ := os.ReadFile(path)
			if err := a.P2P.RenewMembership(ctx); err == nil {
				t.Fatal("übernommen")
			}
			if after, _ := os.ReadFile(path); !bytes.Equal(before, after) || a.P2P.MemberInfo().Role != membership.RoleHelper {
				t.Fatalf("zustand geändert: %s", after)
			}
		})
	}
}

// TestTokenGoesOnlyToIssuer: Steht ein anderer Peer mit im Link, erfährt er das
// Token nicht. Sonst könnte ein übernommener Helfer die Einladung selbst einlösen.
func TestTokenGoesOnlyToIssuer(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	net := newTestNet(t)
	iss, dir := issuerNode(t, ctx, net, nil)

	// Ein böswilliger Helfer, der alles mitschreibt, was auf dem Protokoll ankommt.
	evil, _ := identity.New()
	h, err := libp2p.New(libp2p.Identity(evil.Priv), libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"))
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	var seen atomic.Int32
	h.SetStreamHandler(lp2pproto.ID(protocol.ProtoMemberJoin), func(s network.Stream) {
		seen.Add(1)
		_ = s.Reset()
	})
	evilAddr := h.Addrs()[0].String() + "/p2p/" + evil.ID.String()

	idA, _ := identity.New()
	tok := newInvite(t, dir, net.id, membership.RoleMember)
	peers := append([]string{evilAddr}, iss.P2P.Addrs()...)
	if _, err := p2p.RedeemInvite(ctx, idA, peers, iss.ID.ID.String(), tok); err != nil {
		t.Fatal(err)
	}
	if seen.Load() != 0 {
		t.Fatal("token ging an einen anderen peer")
	}
	// Ohne Adresse des Ausstellers geht es gar nicht erst los.
	if _, err := p2p.RedeemInvite(ctx, idA, []string{evilAddr}, iss.ID.ID.String(), tok); err == nil || seen.Load() != 0 {
		t.Fatalf("nur fremde adresse: %v, gesehen %d", err, seen.Load())
	}
}
