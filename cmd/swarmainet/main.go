// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Rainer Sestig. Kommerzielle Lizenz: siehe COMMERCIAL.md.

// swarmainet ist der SwarmAiNet-Knoten.
//
//	swarmainet init      Konfiguration und Schlüssel anlegen
//	swarmainet run       Knoten starten
//	swarmainet status    Diagnose des laufenden Knotens
//	swarmainet ping      Weg zu einem Peer prüfen
//	swarmainet pause | resume | stop
//	swarmainet psk       Schlüssel für einen privaten Schwarm erzeugen
//	swarmainet invite    Einladungslink erzeugen
//	swarmainet join      Netz aus einer Einladung beitreten
//	swarmainet network   Mitgliedsnetz anlegen
//	swarmainet member    Zertifikate eines Mitgliedsnetzes verwalten
//	swarmainet tracker   HTTPS-Adressbriefkasten betreiben
//	swarmainet bench     Benchmark ausführen und auswerten
//	swarmainet version   Version anzeigen
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/rainer-sharp/Ai-Swarms.net/internal/config"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/gateway"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/node"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/tracker"
)

func usage() {
	fmt.Fprintln(os.Stderr, `SwarmAiNet Knoten

Befehle:
  init      Konfiguration und Schlüssel anlegen, standardmäßig mit eigenem privatem Netz
              -open      stattdessen dem offenen Netz beitreten (für andere sichtbar)
  run       Knoten starten
  status    Status des laufenden Knotens anzeigen
  ping      Weg zu einem Peer prüfen: ping [-net ID] <peer-id | /ip4/…/p2p/<peer-id>>
  pause     Annahme fremder Aufträge pausieren
  resume    fortsetzen
  stop      Sofort-Stopp: Fremdjobs abbrechen, keine Annahmen mehr
  psk       Schlüsseldatei für einen privaten Schwarm erzeugen
  invite    Einladungslink erzeugen (Knoten muss laufen)
              -net ID    Netz, in das eingeladen wird (Standard: erstes Netz)
              -with-key  Netzschlüssel einbetten (nur privat; vertraulich weitergeben!)
              -lan       im offenen Netz auch lokale Adressen aufnehmen
              -peer ADR  zusätzliche Adresse, z. B. eines Brückenknotens (mehrfach)
              -qr        Link zusätzlich als QR-Code im Terminal anzeigen
  join      Netz aus einer Einladung beitreten, auch Mitgliedsnetze: join [-key DATEI] <link>;
            gehört der Knoten schon zu einem Netz, kommt es unter node.networks dazu
  network   network create: Mitgliedsnetz mit Wurzelschlüssel anlegen (spec/membership.md)
  member    Zertifikate eines Mitgliedsnetzes verwalten:
              invite -net ID [-role R] [-name N] [-ttl D] [-peer ADR] [-qr]   Einladung, einmalig, höchstens 24h
              request -net ID                Peer-ID dieses Knotens im Netz nennen
              issue -net ID [-role R] [-name N] [-ttl D] [-out DATEI] <peer-id>
              install -root SCHLÜSSEL DATEI  Zertifikat prüfen und übernehmen
              revoke -net ID [-reason T] <peer-id|name>
              import-crl -root SCHLÜSSEL DATEI   Sperrliste einspielen, etwa die der Wurzel
              list -net ID
              -root-key DATEI  Wurzelschlüssel, etwa vom Wechseldatenträger
              -dir VERZ.       statt node.data_dir
  tracker   Adressbriefkasten für Knoten betreiben (spec/p2p-layer.md § 2.3)
              -listen ADR      Standard 127.0.0.1:7720 (hinter einem TLS-Reverse-Proxy)
              -tls-cert DATEI, -tls-key DATEI   selbst per HTTPS lauschen
              -trust-proxy     Absender-IP aus X-Forwarded-For
  bench     Benchmark ausführen und auswerten (spec/benchmark.md)
  version   Version anzeigen

Optionen:
  -config PFAD   (Standard ~/.swarmainet/config.yaml)
  -v             ausführliche Protokollierung
Optionen stehen vor den übrigen Argumenten, z. B. swarmainet ping -net p-home <peer-id>.`)
}

func main() {
	home, _ := os.UserHomeDir()
	fs := flag.NewFlagSet("swarmainet", flag.ExitOnError)
	cfgPath := fs.String("config", filepath.Join(home, ".swarmainet", "config.yaml"), "Konfigurationsdatei")
	verbose := fs.Bool("v", false, "ausführliche Protokollierung")
	withKey := fs.Bool("with-key", false, "invite: Netzschlüssel einbetten")
	lan := fs.Bool("lan", false, "invite: lokale Adressen aufnehmen")
	openNet := fs.Bool("open", false, "init: offenes Netz statt eines eigenen privaten Netzes")
	showQR := fs.Bool("qr", false, "invite: Link zusätzlich als QR-Code anzeigen")
	keyFile := fs.String("key", "", "join: Schlüsseldatei des privaten Netzes")
	netID := fs.String("net", "", "ping, invite: Netz-Kennung (Standard: erstes Netz)")
	listen := fs.String("listen", "127.0.0.1:7720", "tracker: Adresse")
	tlsCert := fs.String("tls-cert", "", "tracker: TLS-Zertifikat")
	tlsKey := fs.String("tls-key", "", "tracker: TLS-Schlüssel")
	trustProxy := fs.Bool("trust-proxy", false, "tracker: Absender-IP aus X-Forwarded-For")
	var extraPeers multiFlag
	fs.Var(&extraPeers, "peer", "invite: zusätzliche Adresse (mehrfach)")
	fs.Usage = usage
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	cmd := os.Args[1]
	if cmd == "network" || cmd == "member" { // eigene Unterbefehle und Optionen
		run := cmdNetwork
		if cmd == "member" {
			run = func(args []string, home string, out io.Writer) error { return cmdMember(args, home, time.Now(), out) }
		}
		if err := run(os.Args[2:], home, os.Stdout); err != nil {
			if !errors.Is(err, flag.ErrHelp) {
				fmt.Fprintln(os.Stderr, "fehler:", err)
			}
			os.Exit(1)
		}
		return
	}
	_ = fs.Parse(os.Args[2:])
	level := slog.LevelInfo
	if *verbose {
		level = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))

	var err error
	switch cmd {
	case "init":
		err = cmdInit(*cfgPath, *openNet, os.Stdout)
	case "run":
		err = cmdRun(*cfgPath)
	case "status":
		err = cmdAPI(*cfgPath, http.MethodGet, "/api/status")
	case "ping":
		err = cmdPing(*cfgPath, fs.Arg(0), *netID)
	case "pause", "resume", "stop":
		err = cmdAPI(*cfgPath, http.MethodPost, "/api/"+cmd)
	case "psk":
		err = cmdPSK(fs.Arg(0))
	case "invite":
		err = cmdInvite(*cfgPath, *netID, *withKey, *lan, *showQR, extraPeers)
	case "join":
		err = cmdJoin(*cfgPath, fs.Arg(0), *keyFile)
	case "tracker":
		err = cmdTracker(*listen, *tlsCert, *tlsKey, *trustProxy)
	case "bench":
		err = cmdBench(fs.Args())
	case "version", "-version", "--version":
		fmt.Println("swarmainet", versionString())
	case "help", "-h", "--help":
		usage()
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "fehler:", err)
		os.Exit(1)
	}
}

func cmdRun(path string) error {
	cfg, err := config.Load(path)
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%s fehlt – zuerst `swarmainet init` ausführen", path)
	}
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	n, err := node.New(ctx, cfg, node.Options{Version: versionString()})
	if err != nil {
		return err
	}
	defer n.Close()
	n.ProbeBackends(ctx)

	var _ gateway.JobBackend = n // asynchrone Aufträge (/v1/jobs)
	srv := &http.Server{Addr: cfg.Node.Gateway, Handler: gateway.Handler(n), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(sctx)
	}()
	slog.Info("knoten gestartet", "version", versionString(), "peer", n.ID.ID.String(), "gateway", "http://"+cfg.Node.Gateway, "provide", cfg.Node.Provide)
	for _, pn := range n.Nets {
		for _, a := range pn.Addrs() {
			slog.Info("p2p-adresse", "netz", pn.NetworkID(), "addr", a)
		}
	}
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func cmdAPI(path, method, endpoint string) error {
	cfg, err := config.Load(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	req, _ := http.NewRequest(method, "http://"+cfg.Node.Gateway+endpoint, nil)
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return fmt.Errorf("knoten nicht erreichbar (%s): %w", cfg.Node.Gateway, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var pretty any
	if json.Unmarshal(body, &pretty) == nil {
		out, _ := json.MarshalIndent(pretty, "", "  ")
		body = out
	}
	fmt.Println(string(body))
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("%s", resp.Status)
	}
	return nil
}

// cmdPSK schreibt einen libp2p-PSK (Format /key/swarm/psk/1.0.0/).
func cmdPSK(path string) error {
	if path == "" {
		return errors.New("aufruf: swarmainet psk DATEI")
	}
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("%s existiert bereits", path)
	}
	if err := writeNewPSK(path); err != nil {
		return err
	}
	fmt.Printf("Schlüssel geschrieben: %s\nAuf alle Geräte des privaten Schwarms kopieren und unter node.network_key_file eintragen.\n", path)
	return nil
}

// cmdPing lässt den laufenden Knoten den Weg zu einem Peer prüfen und gibt
// den Bericht als Text aus.
func cmdPing(path, target, network string) error {
	if target == "" {
		return errors.New("aufruf: swarmainet ping <peer-id | /ip4/…/tcp/4711/p2p/<peer-id>>")
	}
	cfg, err := config.Load(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	body, _ := json.Marshal(map[string]string{"peer": target, "network": network})
	resp, err := (&http.Client{Timeout: 70 * time.Second}).Post("http://"+cfg.Node.Gateway+"/api/ping", "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("knoten nicht erreichbar (%s): %w", cfg.Node.Gateway, err)
	}
	defer resp.Body.Close()
	var rep struct {
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
		OK    bool     `json:"ok"`
		Via   string   `json:"via"`
		Steps []string `json:"steps"`
		Hints []string `json:"hints"`
		Addrs []string `json:"addrs"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rep); err != nil {
		return err
	}
	if rep.Error != nil {
		return errors.New(rep.Error.Message)
	}
	for _, s := range rep.Steps {
		fmt.Println("  " + s)
	}
	for _, a := range rep.Addrs {
		fmt.Println("  Adresse: " + a)
	}
	for _, h := range rep.Hints {
		fmt.Println("Hinweis: " + h)
	}
	if !rep.OK {
		return errors.New("peer nicht erreichbar")
	}
	fmt.Printf("Erreichbar (%s).\n", map[string]string{"direct": "direkt", "relay": "über Relay"}[rep.Via])
	return nil
}

// cmdTracker betreibt einen Tracker. Ohne TLS-Angaben lauscht er nur für
// einen Reverse-Proxy; Knoten akzeptieren Tracker nur per HTTPS.
func cmdTracker(addr, cert, key string, trustProxy bool) error {
	if (cert == "") != (key == "") {
		return errors.New("-tls-cert und -tls-key nur zusammen")
	}
	t := tracker.NewServer()
	t.TrustProxy = trustProxy
	srv := &http.Server{Addr: addr, Handler: t, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 20 * time.Second, WriteTimeout: 20 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(sctx)
	}()
	slog.Info("tracker gestartet", "adresse", addr, "tls", cert != "", "trust_proxy", trustProxy)
	var err error
	if cert != "" {
		err = srv.ListenAndServeTLS(cert, key)
	} else {
		err = srv.ListenAndServe()
	}
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
