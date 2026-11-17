# SwarmAiNet

[English](README.md) · **Deutsch**

**Lokale KI-Modelle bilden ein Team.** SwarmAiNet verbindet die Rechner, auf denen Sie Modelle mit Ollama, llama.cpp, vLLM oder LM Studio betreiben, zu einem Schwarm. Für jede Anfrage stellt er ein Team zusammen: Ein Modell entwirft, ein Modell einer anderen Familie prüft, und nur bei Mängeln wird nachgebessert. Ihre Anwendungen sprechen mit dem Schwarm wie mit einem einzelnen OpenAI-Endpunkt.

> **Status: Forschungsprototyp.** Der lokale Schwarm sowie private Netze und Gruppennetze über mehrere Standorte laufen auf echter Hardware. Das offene Netz ist bisher nur im Prozess getestet. Ob der Schwarm bessere Antworten liefert als das beste Einzelmodell, ist noch nicht belegt. Protokoll in Version 0.1, inkompatible Änderungen sind möglich. Details: [Stand](docs/stand.md).

## Was es kann

- **Ein Endpunkt für alle Modelle:** OpenAI-kompatible API auf `http://127.0.0.1:7711/v1` für Open WebUI, IDEs und Coding-Agenten, auch mit Werkzeug-Aufrufen.
- **Zusammenarbeit nach Muster:** bester Spezialist, mehrere Modelle parallel mit Synthese oder Entwurf mit Prüfung durch eine andere Modellfamilie. Die Antwort zeigt, welches Modell was beigetragen hat.
- **Drei Netzarten:**
  - **Privates Netz** für die eigenen Geräte, abgeschlossen mit einem gemeinsamen Schlüssel.
  - **Gruppennetz** für Vereine, Teams und Firmen: Zertifikat je Knoten, Mitglieder per Einladungslink aufnehmen und einzeln in Sekunden entfernen, ohne zentralen Server.
  - **Offenes Netz** für alle, mit eingeschränkten Rechten für Fremde.
- **Über Standorte hinweg:** Knoten hinter NAT, CGNAT oder DS-Lite finden sich ohne offene Ports, direkt, per Hole Punching oder über einen Helfer.
- **Ihre Daten bleiben bei Ihnen:** Sie legen je Anfrage fest, wer sie sehen darf (nur lokal, vertraute Geräte, öffentlich). Welche Werkzeuge fremde Modelle sehen, bestimmen Sie je Netz.
- **Ihre Hardware bleibt Ihre:** Eigene Anfragen haben Vorrang. Für andere rechnet ein Knoten nur, wenn Sie es einschalten, und nur in Zeitfenstern, unter GPU- und Temperaturgrenzen oder bei PV-Überschuss.

## Schnellstart

Voraussetzungen: Go 1.27 und ein laufender Inferenz-Server mit OpenAI-kompatibler API, z. B. Ollama.

```sh
git clone https://github.com/rainer-sharp/Ai-Swarms.net.git
cd Ai-Swarms.net
go build -o swarmainet ./cmd/swarmainet

./swarmainet init                    # legt ~/.swarmainet/config.yaml an
$EDITOR ~/.swarmainet/config.yaml    # Modelle unter backends eintragen
./swarmainet run
```

Ein Beispiel für den Abschnitt `backends` mit zwei Modellfamilien steht in [`config.example.yaml`](config.example.yaml). Danach:

```sh
curl -s http://127.0.0.1:7711/v1/chat/completions -H 'Content-Type: application/json' \
  -d '{"model":"swarmainet","messages":[{"role":"user","content":"Was ist ein Primzahlsieb?"}]}'
```

Oder in Open WebUI unter *Einstellungen → Verbindungen* die URL `http://127.0.0.1:7711/v1` eintragen und das Modell `swarmainet` wählen.

> Ohne weitere Einstellungen ist ein neuer Knoten im offenen Netz. Er nimmt dort keine fremden Aufträge an und gibt Ihre Anfragen nicht nach außen, ist für andere aber mit Peer-ID und Adresse sichtbar. Für die eigenen Geräte richten Sie besser ein [privates Netz](docs/netze.md#privates-netz-eigene-geräte) ein.

**Weitere Geräte verbinden:** Auf einem laufenden Knoten `swarmainet invite -with-key` aufrufen und auf dem neuen Gerät `swarmainet join '<link>'`. Im LAN finden sich die Geräte danach selbst; für mehrere Standorte siehe [Netzarten](docs/netze.md#mehrere-standorte).

## Dokumentation

| | |
|---|---|
| [Installation](docs/installation.md) | Bauen, Einrichten, Dauerbetrieb unter Linux und Docker |
| [Nutzung](docs/nutzung.md) | Modellnamen und Muster, Open WebUI, Coding-Agenten, Befehle, Diagnose |
| [Netzarten](docs/netze.md) | Privates Netz, Gruppennetz, offenes Netz, mehrere Standorte, Helfer |
| [Konfiguration](docs/konfiguration.md) | Alle Schlüssel und Dateien |
| [Stand](docs/stand.md) | Was erprobt ist, was fehlt |
| [Spezifikation](spec/README.md) | Protokoll, Netzschicht, Sicherheitsmodell |

## Sicherheit

SwarmAiNet ist nicht auditiert. Geben Sie vertrauliche Inhalte nur an eigene Geräte oder Mitglieder eines Gruppennetzes. Im offenen Netz sehen fremde Knoten, was Sie als `public` senden. Schwachstellen bitte nicht öffentlich melden, sondern wie in [SECURITY.md](SECURITY.md) beschrieben.

## Mitwirken

Fehlerberichte, Messungen mit anderen Modellen und Erfahrungen aus anderen Netzen sind willkommen, siehe [CONTRIBUTING.md](CONTRIBUTING.md). Code-Beiträge setzen die [Beitragsvereinbarung](CLA.md) voraus.

## Lizenz

SwarmAiNet ist freie Software unter der [GNU AGPL-3.0](LICENSE). Wer es verändert und weitergibt oder als Dienst im Netz anbietet, muss den Quellcode offenlegen. Für den Einbau in geschlossene Produkte gibt es eine [kommerzielle Lizenz](COMMERCIAL.md).

Die Dokumentation und die Spezifikation stehen unter [CC BY 4.0](LICENSE-DOCS), Abgrenzung siehe [COMMERCIAL.md](COMMERCIAL.md#dokumentation-cc-by-40).

Die Benchmark-Aufgaben in `bench/` stammen aus HumanEval und GSM8K (© OpenAI, MIT).
