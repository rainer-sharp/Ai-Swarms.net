# Nutzung

[English](../usage.md) · **Deutsch**

Jeder Knoten stellt auf `http://127.0.0.1:7711` eine OpenAI-kompatible API und eine Status-Seite bereit. Das Gateway lauscht nur auf localhost. Jede Anwendung, die einen OpenAI-Endpunkt kennt, kann den Schwarm nutzen: Open WebUI, eine IDE oder ein Coding-Agent. Als Endpunkt `http://127.0.0.1:7711/v1` eintragen; einen API-Schlüssel braucht das Gateway nicht.

## Modellnamen: Wie der Schwarm arbeitet

Der Modellname wählt das Muster, nach dem die Modelle zusammenarbeiten:

| Modellname | Muster |
|---|---|
| `swarmainet` | Standard aus `tasks.pattern` (Entwurf + Prüfer + Synthese) |
| `swarmainet/single` | bester Spezialist, bei Ausfall der nächste |
| `swarmainet/parallel` | bis zu drei Modelle verschiedener Familien unabhängig, Synthese lokal |
| `swarmainet/draft_verify_synthesize` | Entwurf, Prüfung durch eine andere Familie, Synthese nur bei Mängeln |
| `swarmainet/parallel_verify` | wie `parallel`, danach Prüfung der Synthese durch eine andere Familie, Überarbeitung nur bei Mängeln |
| `swarmainet/cascade` | erst ein günstiges Modell, Prüfung durch eine andere Familie; nur bei Mängeln löst das beste andere Modell neu |

Mit `cooperation.selection: bandit` wählt der Knoten beim Modellnamen `swarmainet` ohne Zusatz das Muster je Aufgabenkategorie nach seinem [Kompetenzprofil](#rückmeldungen-und-kompetenzprofil), statt `tasks.pattern` zu nehmen.

```sh
curl -s http://127.0.0.1:7711/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -H 'X-SwarmAiNet-Privacy: trusted' \
  -d '{"model":"swarmainet","messages":[{"role":"user","content":"Schreibe eine Python-Funktion für Primzahlen"}]}'
```

Die Antwort enthält zusätzlich das Feld `swarmainet` mit Ablauf, Prüfurteil, gefundenen Mängeln und Einschränkungen. In `steps[]` stehen je Schritt Agent, Peer, Dauer und Tokens. `selected_by` nennt, wer das Muster gewählt hat (`request`, `rules` oder `bandit`), `escalated: true`, dass die Kaskade eskaliert hat.

## Optionale Header

| Header | Bedeutung |
|---|---|
| `X-SwarmAiNet-Privacy` | Freigabeklasse `local`, `trusted`, `reduced` oder `public`: wer die Anfrage sehen darf ([Freigabeklassen](../../spec/de/privacy-classes.md)) |
| `X-SwarmAiNet-Skill` | Fähigkeit, z. B. `code.python`; ohne Angabe entscheidet der Router |
| `X-SwarmAiNet-Agents` | kommagetrennte Agent-IDs, auf die die Auswahl beschränkt wird (für Messungen) |
| `X-SwarmAiNet-Trace: 1` | Texte der Zwischenschritte in `steps[].output` |

Ohne Header gilt `tasks.privacy_class` aus der Konfiguration (Standard `trusted`): Die Anfrage geht nur an eigene oder vertraute Geräte. An fremde Knoten im offenen Netz geht nur, was als `public` markiert ist. Lässt die Freigabeklasse keinen der bekannten Agenten zu, antwortet das Gateway mit 400 und nennt den Grund, etwa bei `trusted` ohne eigenes Modell und ohne vertraute Knoten.

## Open WebUI

1. *Einstellungen → Verbindungen → OpenAI-API*: URL `http://127.0.0.1:7711/v1`, Schlüssel beliebig.
2. Die Modelle `swarmainet`, `swarmainet/single` usw. erscheinen in der Modellauswahl.
3. Für Werkzeuge (z. B. Open Terminal): beim Modell unter *Erweiterte Parameter* den **Funktionsaufruf auf „Nativ“** stellen.

Läuft Open WebUI in Docker, erreicht es localhost des Rechners nur mit Host-Netz (`network_mode: host`) oder über `host.docker.internal` mit einer Weiterleitung. Das Gateway selbst bleibt auf localhost.

## Coding-Agenten und Werkzeuge

Anfragen mit `tools` (z. B. von pi oder Open WebUI mit Funktionsaufruf „Nativ“) gehen an genau ein Modell, das in der Konfiguration `tools: true` trägt; dabei gilt immer das Muster `single`, unabhängig vom Modellnamen. Die Antwort enthält `tool_calls` und `finish_reason: "tool_calls"`, auch beim Streaming.

Die Werkzeuge führt die **Anwendung selbst** aus, also Open WebUI oder der Agent auf Ihrem Rechner, nicht das Modell. Jedes Ergebnis geht aber zurück an das Modell. Deshalb legt `allowed_tools` je Netz fest, welche Werkzeuge dessen Modelle sehen dürfen:

| Netz | ohne `allowed_tools` | mit Liste |
|---|---|---|
| eigener Knoten | alle | alle |
| privates Netz (PSK) | alle | nur die Liste |
| Mitgliedsnetz | keine | nur die Liste |
| offenes Netz, fremde Knoten | keine | nur die Liste |
| `trusted_peers` | alle | alle |

Findet sich kein freigegebener Agent, antwortet das Gateway mit 400. Ruft ein Modell ein Werkzeug auf, das ihm nicht gezeigt wurde, wird die Antwort verworfen. Einzelheiten: [Freigabeklassen, Regel 6](../../spec/de/privacy-classes.md).

Inhalte als Liste von Textteilen werden zusammengefügt. Bilder in Chat-Nachrichten werden noch nicht unterstützt (400).

## Transkription

`POST /v1/audio/transcriptions` nimmt eine Aufnahme im Format der OpenAI-Transkriptions-API an (`multipart/form-data` mit `file`, optional `language`, `prompt`, `response_format` `json` oder `text`) und lässt sie von einem Modell mit der Fähigkeit `audio.transcribe` abschreiben, etwa auf einem anderen Knoten des eigenen Netzes. Das Feld `model` wird ignoriert; der Schwarm wählt das Modell. Höchstens 2,75 MiB je Aufnahme; längere Aufnahmen müssen geteilt werden. Audio geht nur an eigene oder vertraute Geräte, nie ins offene Netz (`X-SwarmAiNet-Privacy: public` ergibt 400).

```sh
curl -s http://127.0.0.1:7711/v1/audio/transcriptions -F file=@aufnahme.wav -F language=de
```

In Open WebUI: *Admin-Einstellungen → Audio → Sprache-zu-Text*, Engine *OpenAI*, URL `http://127.0.0.1:7711/v1`, Schlüssel und Modell beliebig.

Ein Transkriptionsmodell wird wie jedes andere eingetragen, nur mit eigener Fähigkeit, damit es nie Textfragen bekommt:

```yaml
- name: nvidia/NVIDIA-Nemotron-3.5-Transcribe-30B-A4B-NVFP4
  family: nemotron
  skills: [audio.transcribe]
  offer: true
```

## Rückmeldungen und Kompetenzprofil

Jeder Knoten führt ein Kompetenzprofil der Agenten, mit denen er arbeitet: eine Glicko-2-Bewertung je Agent und Fähigkeit, dazu eine Prüferstatistik (wie oft sich das `issues` oder `ok` eines Prüfers als richtig erwies). Es sammelt vorerst nur; die Auswahl der Agenten nutzt es noch nicht ([Kooperation](../../spec/de/cooperation.md) § 2).

Eine Rückmeldung zu einer Antwort nutzt deren `task_id` aus dem Feld `swarmainet`:

```sh
curl -s http://127.0.0.1:7711/v1/feedback -H 'Content-Type: application/json' \
  -d '{"task_id":"<task_id>","good":true}'
```

| Feld | Bedeutung |
|---|---|
| `good` | das Ergebnis war gut (`true`) oder nicht (`false`) |
| `objective` | `true` für ein geprüftes Ergebnis (Tests, exakte Lösung): volles Gewicht; sonst Nutzerfeedback mit Gewicht ¼ |
| `step` | Index in `steps[]`, etwa um einen Entwurf einzeln zu bewerten; ohne Angabe zählt das Endergebnis |

Die Antwort ist 204, bei unbekanntem Auftrag 404, bei schon gemeldetem Schritt 409, bei ungültigem Schritt 400. `swarmainet bench run` sendet seine Bewertungen automatisch als objektive Rückmeldung. `GET http://127.0.0.1:7711/api/profile` zeigt das Profil; gespeichert wird es in `~/.swarmainet/profile.json`. In der Antwort zeigt `steps[].verdict` das Urteil jedes Prüfers.

## Befehle

| Befehl | Zweck |
|---|---|
| `init [-open]` | Konfiguration und Schlüssel anlegen, standardmäßig mit eigenem privatem Netz; `-open` für das offene Netz |
| `run` | Knoten starten |
| `status` | Status des laufenden Knotens (JSON) |
| `pause`, `resume`, `stop` | fremde Aufträge pausieren, fortsetzen, Sofort-Stopp |
| `ping [-net ID] <peer>` | Weg zu einem Peer prüfen |
| `psk DATEI` | Schlüssel für ein privates Netz erzeugen |
| `invite [-with-key] [-lan] [-peer ADR] [-net ID] [-qr]` | Einladungslink erzeugen (Knoten muss laufen), mit `-qr` auch als QR-Code |
| `join [-key DATEI] <link>` | Netz aus einer Einladung beitreten |
| `network create` | Mitgliedsnetz mit Wurzelschlüssel anlegen |
| `member invite\|request\|issue\|install\|revoke\|import-crl\|list` | Mitglieder aufnehmen und Zertifikate verwalten |
| `tracker [-listen ADR] [-tls-cert F -tls-key F] [-trust-proxy]` | Adressbriefkasten betreiben |
| `bench run\|report\|hash` | Benchmark, siehe [`bench/`](../../bench/de/README.md) |

Alle Befehle nehmen `-config PFAD` (Standard `~/.swarmainet/config.yaml`) und `-v` für ausführliche Protokolle; Optionen stehen vor den übrigen Argumenten.

## Diagnose

- **`swarmainet status`:** je Netz (`networks[]`) Peer-ID, Adressen, `reachability` (offenes Netz), Relay-Dienst und -Reservierungen, DHT-Modus und -Größe, veröffentlichte Fähigkeiten und `hints` mit Hinweisen im Klartext; je Peer Netz, Rolle und `via: direct|relay`.
- **`swarmainet ping <peer-id>`** (oder `/ip4/…/tcp/4711/p2p/<peer-id>`, `-net` für ein bestimmtes Netz): bekannte Adressen, Suche in der DHT, Verbindungsaufbau direkt oder über Relay (mit kurzer Wartezeit auf Hole Punching), Laufzeit, angebotene Agenten; bei Fehlschlag mit Hinweisen zu Firewall, Netzschlüssel oder fehlendem Relay.
- **Status-Seite** unter `http://127.0.0.1:7711`: Ressourcen, eigene Agenten, verbundene Knoten, eigene Adressen und Quittungen; dort lässt sich auch ein Peer von Hand anwählen.
- **„kein passender agent verfügbar“:** Kein verbundenes Modell hat die verlangte Fähigkeit und Freigabeklasse, oder alle sagen gerade ab (Zeitfenster, Auslastung). `status` zeigt, welche Agenten bekannt sind.
