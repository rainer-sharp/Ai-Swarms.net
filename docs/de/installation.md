# Installation und erster Start

[English](../installation.md) · **Deutsch**

## Voraussetzungen

- **Ein Inferenz-Server mit OpenAI-kompatibler API** auf jedem Gerät, das Modelle beisteuert: Ollama, llama.cpp, vLLM, LM Studio oder MLX. Geräte ohne eigene Modelle können trotzdem Aufträge stellen oder das Netz tragen.
- **Go 1.27** nur zum Bauen aus den Quellen (Version siehe `go.mod`); ab Version 0.4.0 gibt es [fertige Programmdateien](#fertige-programmdateien).
- Linux ist das Hauptsystem. macOS sollte laufen, ist aber nicht geprüft. Windows ist ebenfalls nicht geprüft.

## Fertige Programmdateien

Ab Version 0.4.0 enthält jedes [Release](https://github.com/rainer-sharp/Ai-Swarms.net/releases) Archive für Linux, macOS und Windows, je für `amd64` und `arm64` (`arm64` etwa für einen Raspberry Pi 4 oder 5 mit 64-Bit-System oder einen Mac mit Apple-Chip). Jedes Archiv enthält das Programm, die Lizenzen, `README.md`, `CHANGELOG.md` und `config.example.yaml`. Unter Linux:

```sh
v=0.4.0
curl -LO https://github.com/rainer-sharp/Ai-Swarms.net/releases/download/v$v/swarmainet_${v}_linux_amd64.tar.gz
curl -LO https://github.com/rainer-sharp/Ai-Swarms.net/releases/download/v$v/SHA256SUMS
sha256sum -c SHA256SUMS --ignore-missing
tar -xzf swarmainet_${v}_linux_amd64.tar.gz
install -m 755 swarmainet_${v}_linux_amd64/swarmainet ~/.local/bin/
swarmainet version
```

- **macOS:** Die Archive heißen `…_darwin_arm64.tar.gz` (Apple-Chip) oder `…_darwin_amd64.tar.gz` (Intel); prüfen mit `shasum -a 256 -c SHA256SUMS --ignore-missing`. Das Programm ist nicht signiert; macOS blockiert es, bis die Quarantäne-Markierung entfernt ist: `xattr -d com.apple.quarantine swarmainet`.
- **Windows:** ein ZIP-Archiv mit `swarmainet.exe`; prüfen mit `Get-FileHash swarmainet_….zip` in der PowerShell und Vergleich mit `SHA256SUMS`. SmartScreen warnt unter Umständen vor einem unbekannten Herausgeber.
- **Reproduzierbar:** Die Archive baut [`scripts/build-release.sh`](../../scripts/build-release.sh) auf GitHub. Wer den Tag auscheckt und das Skript mit der Go-Version aus `go.mod` ausführt, sollte byte-gleiche Archive erhalten.

`swarmainet version` zeigt die Version; ein laufender Knoten nennt sie auch in `swarmainet status` und beim Start. Ein eigener Build nennt stattdessen den Git-Commit (`dev-1a2b3c4d`).

## Aus den Quellen bauen

```sh
git clone https://github.com/rainer-sharp/Ai-Swarms.net.git
cd Ai-Swarms.net
go build -o swarmainet ./cmd/swarmainet
go test ./...          # optional
```

Oder ohne Klonen, direkt nach `~/go/bin`:

```sh
go install github.com/rainer-sharp/Ai-Swarms.net/cmd/swarmainet@latest
```

Unter Ubuntu ohne installiertes Go erledigt [`deploy/linux/gruppe-beitreten.sh`](../../deploy/linux/de/README.md#knoten-für-ein-gruppennetz-einrichten) die Installation von Go mit, allerdings nur für den Beitritt zu einem Gruppennetz.

## Ersten Knoten einrichten

```sh
./swarmainet init               # legt ~/.swarmainet/config.yaml, Schlüssel und ein eigenes privates Netz an
$EDITOR ~/.swarmainet/config.yaml
./swarmainet run
```

Danach steht die OpenAI-kompatible API unter `http://127.0.0.1:7711/v1` bereit, und unter `http://127.0.0.1:7711` erscheint eine Status-Seite. Wie man sie nutzt, steht in [Nutzung](usage.md).

> **Hinweis:** `init` richtet ein **eigenes privates Netz** ein (Kennung `p-…`, Schlüssel in `~/.swarmainet/p-….key`). Weitere eigene Geräte kommen per [Einladung](networks.md#einladungen) dazu. Wer stattdessen einem anderen Netz beitreten will, ruft `swarmainet join '<link>'` auf, bevor der Knoten zum ersten Mal läuft; das neue private Netz weicht dann. Für das **offene Netz** `swarmainet init -open`: Andere Teilnehmer sehen dann Peer-ID und öffentliche Adresse des Knotens; fremde Aufträge nimmt er erst an, wenn Sie das ausdrücklich einschalten.

## Die wichtigsten Einstellungen

Ein vollständiges, kommentiertes Beispiel ist [`config.example.yaml`](../../config.example.yaml); alle Netzschlüssel stehen in [Konfiguration](configuration.md).

1. **Modelle eintragen:** Unter `backends[].models` die Modelle mit Familie (`family`), Fähigkeiten (`skills`) und Stärke (`strength`, 1–10) eintragen. Für einen Prüfer braucht der Schwarm mindestens zwei **verschiedene Modellfamilien**, etwa `qwen` und `llama`.
2. **Modelle anbieten:** `node.provide: true` und bei den Modellen `offer: true` setzen, wenn das Gerät für andere rechnen soll.
3. **Vertrauen herstellen:** Wie sich Geräte gegenseitig vertrauen, hängt von der Netzart ab, siehe [Netzarten](networks.md).
4. **Geräte finden:** Im LAN finden sich die Geräte per mDNS. Sonst unter `node.static_peers` eine Adresse aus `swarmainet status` eintragen. Erfolgreich verbundene Peers merkt sich der Knoten in `~/.swarmainet/peers-<network_id>.json` und wählt sie nach einem Neustart zuerst an.
5. **Zeitfenster beachten:** In der Standardkonfiguration nimmt ein Knoten fremde Aufträge nur werktags von 22 bis 7 Uhr und am Wochenende an. Zum Ausprobieren `resources.schedule.windows: []` setzen.
6. **Werkzeuge:** Modelle, die Werkzeug-Aufrufe beherrschen, mit `tools: true` kennzeichnen. Sie bedienen dann Coding-Agenten, siehe [Nutzung](usage.md#coding-agenten-und-werkzeuge).

Der Ressourcenmanager unter `resources` begrenzt, wie viel ein Knoten für andere rechnet: parallele Aufträge, GPU-Auslastung und -Minuten, Temperatur, RAM-Reserve und optional Überschuss aus einer PV-Anlage über Home Assistant. Eigene Anfragen haben immer Vorrang.

## Dauerbetrieb

- **Linux:** als systemd-Benutzerdienst, siehe [`deploy/linux/`](../../deploy/linux/de/README.md).
- **Docker:** für Helferknoten ohne Modelle, siehe [Netzarten – Helfer mit Docker](networks.md#helferknoten-mit-docker).
- **Raspberry Pi** als Helfer hinter einer Portfreigabe: [`deploy/raspberry-pi/`](../../deploy/raspberry-pi/de/README.md).
- **VPS** mit Startknoten und Helfer: [`deploy/vps/`](../../deploy/vps/de/README.md).

## Aktualisieren

Neu bauen und den Knoten neu starten. Konfiguration und Schlüssel unter `~/.swarmainet/` bleiben erhalten. Das Protokoll ist noch in Version 0.1; zwischen Entwicklungsständen kann es inkompatible Änderungen geben, deshalb sollten alle Knoten eines Netzes auf demselben Stand sein.
