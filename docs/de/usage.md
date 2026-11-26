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
| `swarmainet/decompose` | ein Planer (bevorzugt das eigene Modell) zerlegt eine mehrteilige Aufgabe in Teilaufgaben mit Abhängigkeiten; jede läuft mit eigenem Muster, unabhängige parallel, und eine Synthese führt die Ergebnisse zusammen. Gibt es nichts zu zerlegen, läuft der Auftrag wie mit `tasks.pattern` |

Mit `cooperation.selection: bandit` wählt der Knoten beim Modellnamen `swarmainet` ohne Zusatz das Muster je Aufgabenkategorie nach seinem [Kompetenzprofil](#rückmeldungen-und-kompetenzprofil), statt `tasks.pattern` zu nehmen.

```sh
curl -s http://127.0.0.1:7711/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -H 'X-SwarmAiNet-Privacy: trusted' \
  -d '{"model":"swarmainet","messages":[{"role":"user","content":"Schreibe eine Python-Funktion für Primzahlen"}]}'
```

Die Antwort enthält zusätzlich das Feld `swarmainet` mit Ablauf, Prüfurteil, gefundenen Mängeln und Einschränkungen. In `steps[]` stehen je Schritt Agent, Peer, Dauer und Tokens. `selected_by` nennt, wer das Muster gewählt hat (`request`, `rules` oder `bandit`), `escalated: true`, dass die Kaskade eskaliert hat, `graph[]` die Teilaufgaben von `decompose` mit Fähigkeit, Abhängigkeiten, Muster und Status.

## Streaming

Mit `stream: true` kommt die Antwort Stück für Stück als Server-Sent Events im OpenAI-Format. Was noch nicht geprüft ist, steht in `delta.reasoning_content`; Open WebUI zeigt das eingeklappt als Denkphase: je Schritt eine Überschrift (Entwurf, Prüfung, Überarbeitung, Synthese, Eskalation) mit dem Modell, der Text des Entwurfs, das Urteil und das Nachdenken des Modells, das die Antwort schreibt. `delta.content` trägt nur Text, nach dem keine Prüfung mehr folgt: bei `single` sofort, bei `draft_verify_synthesize`, `parallel_verify` und `cascade` die Überarbeitung oder Eskalation. Findet der Prüfer keine Mängel, folgt der geprüfte Entwurf als ein Block.

Scheitert ein Schritt, nachdem er schon Text gestreamt hat, folgt ein Trenner („Diese Fassung wurde verworfen; es folgt eine neue.“) und dann die neue Fassung. Das letzte Stück trägt `finish_reason` und das Feld `swarmainet`. Fehler vor dem ersten Stück sind gewöhnliche HTTP-Fehler (etwa 400), danach erscheinen sie im Text. Mit Werkzeugen läuft der Text ebenfalls live; die Werkzeug-Aufrufe kommen vollständig im letzten Stück, erst nachdem der Knoten geprüft hat, dass das Modell nur erlaubte Werkzeuge aufruft. Modelle auf Knoten mit Version 0.3.0 oder älter antworten als ein Block.

## Optionale Header

| Header | Bedeutung |
|---|---|
| `X-SwarmAiNet-Privacy` | Freigabeklasse `local`, `trusted`, `reduced` oder `public`: wer die Anfrage sehen darf ([Freigabeklassen](../../spec/de/privacy-classes.md)) |
| `X-SwarmAiNet-Skill` | Fähigkeit, z. B. `code.python`; ohne Angabe entscheidet der Router |
| `X-SwarmAiNet-Agents` | kommagetrennte Agent-IDs, auf die die Auswahl beschränkt wird (für Messungen) |
| `X-SwarmAiNet-Trace: 1` | Texte der Zwischenschritte in `steps[].output` |
| `X-SwarmAiNet-Coordinator` | nur für `/v1/jobs`: Peer-ID eines vertrauten Knotens, der den Auftrag koordiniert ([unten](#asynchrone-aufträge)) |

Ohne Header gilt `tasks.privacy_class` aus der Konfiguration (Standard `trusted`): Die Anfrage geht nur an eigene oder vertraute Geräte. An fremde Knoten im offenen Netz geht nur, was als `public` markiert ist. Lässt die Freigabeklasse keinen der bekannten Agenten zu, antwortet das Gateway mit 400 und nennt den Grund, etwa bei `trusted` ohne eigenes Modell und ohne vertraute Knoten.

Mit `reduced` kommen auch fremde Knoten im offenen Netz in Frage, sie sehen aber nur eine gekürzte Aufgabe mit Platzhaltern. Enthält die Anfrage einen Verlauf oder Systemanweisungen, schreibt das eigene oder ein vertrautes Modell zuerst die letzte Anfrage als in sich geschlossene Aufgabe, nur mit dem Nötigen; fremde Knoten bekommen diese Aufgabe statt des Gesprächs. Darin werden Namen, Firmen, Orte, E-Mail-Adressen, Telefonnummern, IBAN und Ähnliches zu `[PERSON_1]`, `[EMAIL_1]` usw., und die Antwort kommt mit den Originalen zurück. Namen erkennt das eigene oder ein vertrautes Modell; Begriffe aus `tasks.pseudonymize_terms` werden immer ersetzt. Eigene und vertraute Geräte sehen weiter das Original. In der Denkphase zeigen die Schritte „Minimierung“ und „Pseudonymisierung“, wie stark gekürzt wurde und wie viele Begriffe ersetzt werden. Ohne Modell für Kürzen und Erkennung geht nichts an fremde Knoten. Einzelheiten und Grenzen: [Freigabeklassen, Regel 9](../../spec/de/privacy-classes.md).

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

Inhalte als Liste von Textteilen werden zusammengefügt. Bilder kommen als Teile vom Typ `image_url` mit eingebetteter data-URL (`data:image/png;base64,…`; PNG, JPEG, WebP, GIF), zusammen höchstens 2,75 MiB je Anfrage, Bilder aus dem Verlauf eingerechnet; Links lädt das Gateway nicht nach. Sie gehen nur an Modelle mit `vision: true` ([Konfiguration](configuration.md)), auch als Prüfer; bei `reduced` bleiben sie bei eigenen und vertrauten Geräten, weil sich ein Bild nicht pseudonymisieren lässt ([Freigabeklassen, Regel 10](../../spec/de/privacy-classes.md)). Open WebUI schickt Bilder auf diese Weise.

## Transkription

`POST /v1/audio/transcriptions` nimmt eine Aufnahme im Format der OpenAI-Transkriptions-API an (`multipart/form-data` mit `file`, optional `language`, `prompt`, `response_format` `json` oder `text`) und lässt sie von einem Modell mit der Fähigkeit `audio.transcribe` abschreiben, etwa auf einem anderen Knoten des eigenen Netzes. Der Knoten mit dem Modell ruft `/audio/transcriptions` seines Backends auf (etwa Whisper); gibt es das nicht, schickt er die Aufnahme über den Chat als `audio_url` mit abgeschaltetem Denkmodus (etwa vLLM mit Nemotron-Transcribe). Das Feld `model` wird ignoriert; der Schwarm wählt das Modell. Audio geht nur an eigene oder vertraute Geräte, nie ins offene Netz (`X-SwarmAiNet-Privacy: public` ergibt 400).

```sh
curl -s http://127.0.0.1:7711/v1/audio/transcriptions -F file=@aufnahme.wav -F language=de
```

Ein Teilauftrag trägt höchstens 2,75 MiB. Längere Aufnahmen (bis 512 MiB) teilt das Gateway selbst: in Stücke von 10 Minuten mit 1 Minute Überlappung, als Mono-MP3 mit 16 kHz. Bis zu vier Stücke schickt es gleichzeitig durch den Schwarm und fügt die Abschriften zusammen; in jeder Überlappung sucht es die längste gemeinsame Wortfolge und schneidet in deren Mitte, sodass keine Wörter fehlen oder doppelt stehen. Bleibt ein Anbieter ausgelastet, wartet das Stück und wird erneut geschickt; scheitert ein Stück, scheitert die ganze Anfrage mit der Ursache. Zum Teilen braucht der Knoten mit dem Gateway `ffmpeg` und `ffprobe` (Debian/Ubuntu: `apt install ffmpeg`; Container: die Image-Variante `docker build --target audio -t swarmainet:audio .`, etwa 230 MB statt 50 MB); ohne sie ergeben Aufnahmen über 2,75 MiB 413. Mit `response_format=verbose_json` enthält das Feld `swarmainet` `duration_s`, `chunks` und den Ablauf jedes Stücks in `parts[]`.

In Open WebUI: *Admin-Einstellungen → Audio → Sprache-zu-Text*, Engine *OpenAI*, URL `http://127.0.0.1:7711/v1`, Schlüssel und Modell beliebig.

Ein Transkriptionsmodell wird wie jedes andere eingetragen, nur mit eigener Fähigkeit, damit es nie Textfragen bekommt:

```yaml
- name: nvidia/NVIDIA-Nemotron-3.5-Transcribe-30B-A4B-NVFP4
  family: nemotron
  skills: [audio.transcribe]
  offer: true
```

## Asynchrone Aufträge

Lange Aufträge, etwa mit `swarmainet/decompose`, können im Hintergrund laufen: `POST /v1/jobs` nimmt dieselbe Anfrage an wie `/v1/chat/completions` und antwortet sofort mit einer Auftragskennung. Das Ergebnis holt man später ab; der Client muss die Verbindung nicht offen halten.

```sh
curl -s http://127.0.0.1:7711/v1/jobs -H 'Content-Type: application/json' \
  -d '{"model":"swarmainet/decompose","messages":[{"role":"user","content":"…"}]}'
# {"id":"job-…","object":"job","status":"running","coordinator":"local",…}
curl -s http://127.0.0.1:7711/v1/jobs/job-…
```

| Anfrage | Bedeutung |
|---|---|
| `POST /v1/jobs` | Auftrag abgeben (202); `stream` wird ignoriert |
| `GET /v1/jobs/{id}` | Zustand `running`, `done`, `failed` oder `canceled`; wenn fertig, unter `result` die Antwort im Format der Chat-API |
| `GET /v1/jobs` | eigene Aufträge, die neuesten zuerst |
| `DELETE /v1/jobs/{id}` | laufenden Auftrag abbrechen (200) oder abgeschlossenen löschen (204) |

Mit dem Header `X-SwarmAiNet-Coordinator: <peer-id>` koordiniert ein vertrauter Knoten den Auftrag statt des eigenen, etwa ein Rechner mit Modellen, der rund um die Uhr läuft, während das Notebook ausgeschaltet ist. Dieser Knoten braucht `cooperation.coordinate_for_others: true` ([Konfiguration](configuration.md)). Er sieht den Auftrag im Klartext und wendet die Freigabeklasse aus seiner Sicht an; `local` verlässt den eigenen Knoten nie. Ist der Koordinator nicht erreichbar, liefert `GET` den letzten bekannten Stand mit `coordinator_error`. Abgeschlossene Aufträge bleiben 24 Stunden abrufbar, auch nach einem Neustart; ein Auftrag, der auf einem Knoten lief, als er beendet wurde, ist verloren und gilt als gescheitert. Details: [Kooperation](../../spec/de/cooperation.md) § 7.4.

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
| `pause`, `resume`, `stop` | fremde Aufträge pausieren, fortsetzen, Sofort-Stopp (bricht laufende fremde Aufträge ab); Pause und Sofort-Stopp gelten auch nach einem Neustart, bis `resume` |
| `ping [-net ID] <peer>` | Weg zu einem Peer prüfen |
| `psk DATEI` | Schlüssel für ein privates Netz erzeugen |
| `invite [-with-key] [-lan] [-peer ADR] [-net ID] [-qr]` | Einladungslink erzeugen (Knoten muss laufen), mit `-qr` auch als QR-Code |
| `join [-key DATEI] <link>` | Netz aus einer Einladung beitreten |
| `network create` | Mitgliedsnetz mit Wurzelschlüssel anlegen |
| `member invite\|request\|issue\|install\|revoke\|import-crl\|list` | Mitglieder aufnehmen und Zertifikate verwalten |
| `tracker [-listen ADR] [-tls-cert F -tls-key F] [-trust-proxy]` | Adressbriefkasten betreiben |
| `bench run\|report\|hash` | Benchmark, siehe [`bench/`](../../bench/de/README.md) |
| `version` | Version anzeigen |

Alle Befehle nehmen `-config PFAD` (Standard `~/.swarmainet/config.yaml`) und `-v` für ausführliche Protokolle; Optionen stehen vor den übrigen Argumenten.

## Diagnose

- **`swarmainet status`:** je Netz (`networks[]`) Peer-ID, Adressen, `reachability` (offenes Netz), Relay-Dienst und -Reservierungen, DHT-Modus und -Größe, veröffentlichte Fähigkeiten und `hints` mit Hinweisen im Klartext; je Peer Netz, Rolle und `via: direct|relay`.
- **`swarmainet ping <peer-id>`** (oder `/ip4/…/tcp/4711/p2p/<peer-id>`, `-net` für ein bestimmtes Netz): bekannte Adressen, Suche in der DHT, Verbindungsaufbau direkt oder über Relay (mit kurzer Wartezeit auf Hole Punching), Laufzeit, angebotene Agenten; bei Fehlschlag mit Hinweisen zu Firewall, Netzschlüssel oder fehlendem Relay.
- **Status-Seite** unter `http://127.0.0.1:7711`: Ressourcen, eigene Agenten, verbundene Knoten, eigene Adressen und Quittungen; dort lässt sich auch ein Peer von Hand anwählen.
- **„kein passender agent verfügbar“:** Kein verbundenes Modell hat die verlangte Fähigkeit und Freigabeklasse, oder alle sagen gerade ab (Zeitfenster, Auslastung). `status` zeigt, welche Agenten bekannt sind.
- **„token-limit erreicht“:** Das Modell hat `max_tokens` verbraucht, bevor es antwortete, meist durch Nachdenken (Reasoning-Modelle, lange Verläufe mit Werkzeugen). `max_tokens` der Anfrage (in Open WebUI unter *Advanced Params*) oder `tasks.max_tokens` erhöhen; andere Fehlschläge eines Anbieters erscheinen nur als „ausführung fehlgeschlagen“, die Ursache steht im Log des Anbieters.
