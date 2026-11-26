// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Rainer Sestig und Mitwirkende.

package p2p

import (
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/p2p/net/swarm"

	"github.com/rainer-sharp/Ai-Swarms.net/internal/identity"
)

// TestIslandRejoinsStartNode bildet den Hardwaretest vom 18.11.2026 nach:
// A kennt den Startknoten S nur als Einstieg, S ist aus, und A ist allein mit
// C verbunden, der weder DHT-Server noch Relay ist. Kommt S zurück, muss A ihn
// wieder anwählen, obwohl schon ein Peer verbunden ist; sonst bleiben A und C
// eine Insel, die neue Knoten über S nicht finden.
func TestIslandRejoinsStartNode(t *testing.T) {
	ctx := t.Context()

	// Freien Port für S merken; S startet erst später darauf.
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	idS, _ := identity.New()
	start := fmt.Sprintf("/ip4/127.0.0.1/tcp/%d/p2p/%s", port, idS.ID)

	idC, _ := identity.New()
	c, err := New(ctx, Options{Identity: idC, Listen: []string{"/ip4/127.0.0.1/tcp/0"}, NetworkID: "swarmainet",
		AnnouncePrivate: true, DHTServer: "false", RelayService: "false"})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	idA, _ := identity.New()
	a, err := New(ctx, Options{Identity: idA, NetworkID: "swarmainet", Bootstrap: []string{start}})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if err := a.host.Connect(ctx, c.host.Peerstore().PeerInfo(c.ID())); err != nil {
		t.Fatal(err)
	}
	if !a.needEntry() {
		t.Fatal("insel ohne dht und relay-platz braucht einen einstieg")
	}

	s, err := New(ctx, Options{Identity: idS, Listen: []string{fmt.Sprintf("/ip4/127.0.0.1/tcp/%d", port)},
		NetworkID: "swarmainet", AnnouncePrivate: true, DHTServer: "true", RelayService: "true"})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	// Der erste Versuch ging fehl; libp2p sperrt die Adresse danach einige
	// Sekunden (im Betrieb höchstens 5 Minuten, die Schleife läuft jede Minute).
	a.host.Network().(*swarm.Swarm).Backoff().Clear(idS.ID)
	a.connectBootstrap(ctx) // was die Schleife jede Minute tut
	deadline := time.Now().Add(10 * time.Second)
	for !a.connected(s.ID()) {
		if time.Now().After(deadline) {
			t.Fatal("startknoten nach rückkehr nicht wieder angewählt")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// TestNoEntryWhenCarried: Mit DHT-Partnern und Relay-Platz bleibt es beim
// Peer-Austausch; der Einstieg wird nicht erneut angewählt.
func TestNoEntryWhenCarried(t *testing.T) {
	ctx := t.Context()
	idS, _ := identity.New()
	s, err := New(ctx, Options{Identity: idS, Listen: []string{"/ip4/127.0.0.1/tcp/0"}, NetworkID: "swarmainet",
		AnnouncePrivate: true, DHTServer: "true", RelayService: "true"})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	idA, _ := identity.New()
	a, err := New(ctx, Options{Identity: idA, NetworkID: "swarmainet", Bootstrap: s.Addrs()})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	deadline := time.Now().Add(10 * time.Second)
	for a.needEntry() {
		if time.Now().After(deadline) {
			t.Fatalf("einstieg weiter nötig: dht %d, relay-plätze %d", a.dht.RoutingTable().Size(), len(a.Reservations()))
		}
		time.Sleep(50 * time.Millisecond)
	}
}
