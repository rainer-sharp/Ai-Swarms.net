# Installation und erster Start

## Voraussetzungen

- **Ein Inferenz-Server mit OpenAI-kompatibler API** auf jedem Gerät, das Modelle beisteuert: Ollama, llama.cpp, vLLM, LM Studio oder MLX. Geräte ohne eigene Modelle können trotzdem Aufträge stellen oder das Netz tragen.
- **Go 1.27** zum Bauen (Version siehe `go.mod`). Fertige Programmdateien gibt es noch nicht.
- Linux ist das Hauptsystem. macOS sollte laufen, ist aber nicht geprüft. Windows ist ebenfalls nicht geprüft.

## Bauen

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

Unter Ubuntu ohne installiertes Go erledigt [`deploy/linux/gruppe-beitreten.sh`](../deploy/linux/README.md#knoten-für-ein-gruppennetz-einrichten) die Installation von Go mit, allerdings nur für den Beitritt zu einem Gruppennetz.

## Ersten Knoten einrichten

```sh
./swarmainet init               # legt ~/.swarmainet/config.yaml und den Schlüssel an, zeigt die Peer-ID
$EDITOR ~/.swarmainet/config.yaml
./swarmainet run
```

Danach steht die OpenAI-kompatible API unter `http://127.0.0.1:7711/v1` bereit, und unter `http://127.0.0.1:7711` erscheint eine Status-Seite. Wie man sie nutzt, steht in [Nutzung](nutzung.md).

> **Hinweis:** Ohne Netzschlüssel oder Einladung ist ein neuer Knoten im **offenen Netz** und meldet sich beim eingebauten Startknoten. Andere Teilnehmer sehen dann seine Peer-ID und seine öffentliche Adresse. Fremde Aufträge nimmt er erst an, wenn Sie das ausdrücklich einschalten. Wer nur die eigenen Geräte verbinden will, richtet zuerst ein [privates Netz](netze.md#privates-netz-eigene-geräte) ein oder setzt `node.dns_seeds: [none]`.

## Die wichtigsten Einstellungen

Ein vollständiges, kommentiertes Beispiel ist [`config.example.yaml`](../config.example.yaml); alle Netzschlüssel stehen in [Konfiguration](konfiguration.md).

1. **Modelle eintragen:** Unter `backends[].models` die Modelle mit Familie (`family`), Fähigkeiten (`skills`) und Stärke (`strength`, 1–10) eintragen. Für einen Prüfer braucht der Schwarm mindestens zwei **verschiedene Modellfamilien**, etwa `qwen` und `llama`.
2. **Modelle anbieten:** `node.provide: true` und bei den Modellen `offer: true` setzen, wenn das Gerät für andere rechnen soll.
3. **Vertrauen herstellen:** Wie sich Geräte gegenseitig vertrauen, hängt von der Netzart ab, siehe [Netzarten](netze.md).
4. **Geräte finden:** Im LAN finden sich die Geräte per mDNS. Sonst unter `node.static_peers` eine Adresse aus `swarmainet status` eintragen. Erfolgreich verbundene Peers merkt sich der Knoten in `~/.swarmainet/peers-<network_id>.json` und wählt sie nach einem Neustart zuerst an.
5. **Zeitfenster beachten:** In der Standardkonfiguration nimmt ein Knoten fremde Aufträge nur werktags von 22 bis 7 Uhr und am Wochenende an. Zum Ausprobieren `resources.schedule.windows: []` setzen.
6. **Werkzeuge:** Modelle, die Werkzeug-Aufrufe beherrschen, mit `tools: true` kennzeichnen. Sie bedienen dann Coding-Agenten, siehe [Nutzung](nutzung.md#coding-agenten-und-werkzeuge).

Der Ressourcenmanager unter `resources` begrenzt, wie viel ein Knoten für andere rechnet: parallele Aufträge, GPU-Auslastung und -Minuten, Temperatur, RAM-Reserve und optional Überschuss aus einer PV-Anlage über Home Assistant. Eigene Anfragen haben immer Vorrang.

## Dauerbetrieb

- **Linux:** als systemd-Benutzerdienst, siehe [`deploy/linux/`](../deploy/linux/README.md).
- **Docker:** für Helferknoten ohne Modelle, siehe [Netzarten – Helfer mit Docker](netze.md#helferknoten-mit-docker).
- **Raspberry Pi** als Helfer hinter einer Portfreigabe: [`deploy/raspberry-pi/`](../deploy/raspberry-pi/README.md).
- **VPS** mit Startknoten und Helfer: [`deploy/vps/`](../deploy/vps/README.md).

## Aktualisieren

Neu bauen und den Knoten neu starten. Konfiguration und Schlüssel unter `~/.swarmainet/` bleiben erhalten. Das Protokoll ist noch in Version 0.1; zwischen Entwicklungsständen kann es inkompatible Änderungen geben, deshalb sollten alle Knoten eines Netzes auf demselben Stand sein.
