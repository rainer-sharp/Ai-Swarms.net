// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Rainer Sestig und Mitwirkende.

package invite

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rainer-sharp/Ai-Swarms.net/internal/identity"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/membership"
)

const addr = "/ip4/192.168.101.52/tcp/4711/p2p/12D3KooWJGzUm1EzV4v88f1rDPF3TkX1bZYy3U7CahkTZuBrDYTP"

func TestRoundtrip(t *testing.T) {
	in := Invite{Network: "p-3fa91c07", Peers: []string{addr, addr + "/p2p-circuit/p2p/12D3KooWF1Yvbw9HELe3xoKyc1BuE2Uhv2aLQU9fq3uC8RH1w8BT"}, Trackers: []string{"https://example.org/t.php"}, PSK: bytes.Repeat([]byte{7}, 32)}
	link := in.String()
	if !strings.HasPrefix(link, "swarmainet://join?") {
		t.Fatal(link)
	}
	out, err := Parse(link + "&zukunft=1") // unbekannte Parameter ignorieren
	if err != nil {
		t.Fatal(err)
	}
	if out.Network != in.Network || len(out.Peers) != 2 || out.Trackers[0] != in.Trackers[0] || !bytes.Equal(out.PSK, in.PSK) {
		t.Fatalf("%+v", out)
	}
}

func TestParseRejects(t *testing.T) {
	for _, link := range []string{
		"https://join?v=1&net=x&peer=" + addr,
		"swarmainet://join?v=2&net=x&peer=" + addr,
		"swarmainet://join?v=1&net=Groß&peer=" + addr,
		"swarmainet://join?v=1&net=x",
		"swarmainet://join?v=1&net=x&peer=/ip4/1.2.3.4/tcp/1",
		"swarmainet://join?v=1&net=x&peer=" + addr + "&psk=AAAA",
		"swarmainet://join?v=1&net=x&peer=" + addr + "&tracker=http://example.org",
	} {
		if _, err := Parse(link); err == nil {
			t.Errorf("nicht abgelehnt: %s", link)
		}
	}
}

func TestPSKFile(t *testing.T) {
	key := bytes.Repeat([]byte{0xab}, 32)
	p := filepath.Join(t.TempDir(), "k")
	_ = os.WriteFile(p, EncodePSK(key), 0o600)
	got, err := ReadPSK(p)
	if err != nil || !bytes.Equal(got, key) {
		t.Fatalf("%x %v", got, err)
	}
}

func TestMemberInvite(t *testing.T) {
	root, _ := identity.New()
	issuer, _ := identity.New()
	helper, _ := identity.New()
	pub, _ := membership.RootKey(root)
	inv := Invite{
		Network: membership.NetworkID(pub), Root: membership.EncodeRoot(pub), Token: "abc", Issuer: issuer.ID.String(),
		Peers: []string{"/ip4/192.0.2.1/tcp/4713/p2p/" + helper.ID.String(), "/ip4/192.0.2.1/tcp/4713/p2p/" + helper.ID.String() + "/p2p-circuit/p2p/" + issuer.ID.String()},
	}
	back, err := Parse(inv.String())
	if err != nil {
		t.Fatal(err)
	}
	if back.Issuer != inv.Issuer || back.Token != "abc" || back.Root != inv.Root {
		t.Fatalf("%+v", back)
	}
	for name, mod := range map[string]func(*Invite){
		"ohne aussteller":              func(i *Invite) { i.Issuer = "" },
		"ohne adresse des ausstellers": func(i *Invite) { i.Peers = i.Peers[:1] },
		"kennung passt nicht":          func(i *Invite) { i.Network = "m-0000000000000000" },
		"mit netzschlüssel":            func(i *Invite) { i.PSK = make([]byte, 32) },
	} {
		c := inv
		mod(&c)
		if err := c.Validate(); err == nil {
			t.Fatalf("%s: akzeptiert", name)
		}
	}
}
