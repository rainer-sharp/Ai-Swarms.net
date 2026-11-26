# Änderungen

[English](../CHANGELOG.md) · **Deutsch**

Alle nennenswerten Änderungen an SwarmAiNet. Das Format folgt [Keep a Changelog](https://keepachangelog.com/de/1.1.0/); die Versionen folgen [Semantic Versioning](https://semver.org/lang/de/) mit dem üblichen Vorbehalt für 0.x: Zwischen Minor-Versionen sind inkompatible Änderungen noch möglich.

## [Unveröffentlicht]

### Hinzugefügt

- **Asynchrone Aufträge (Kooperation Stufe 5):** `POST /v1/jobs` nimmt eine Chat-Anfrage an und antwortet sofort mit einer Auftragskennung; `GET /v1/jobs/{id}` liefert Zustand und, wenn fertig, die Antwort, `DELETE` bricht ab oder löscht. Mit dem Header `X-SwarmAiNet-Coordinator` koordiniert ein vertrauter Knoten mit `cooperation.coordinate_for_others: true` den Auftrag, sodass der Auftraggeber zwischendurch offline gehen darf (neues Protokoll `/swarmainet/job/0.1`). Abgeschlossene Aufträge bleiben 24 Stunden abrufbar, auch nach einem Neustart. ([Nutzung](../docs/de/usage.md#asynchrone-aufträge), [Kooperation](../spec/de/cooperation.md) § 7.4)
- **Ersatzkoordinator (Lease):** Mit `X-SwarmAiNet-Substitute: <peer-id>` oder `auto` hält ein zweiter vertrauter Knoten Prüfpunkte eines Auftrags (den Auftrag, bei `decompose` auch fertige Teilaufgaben) und übernimmt, wenn der Koordinator seinen Lease 60 Sekunden lang nicht verlängert; der Auftraggeber holt das Ergebnis dann dort ab. Anbieter lehnen Teilaufträge einer älteren Lease-Epoche ab, damit ein zurückkehrender Koordinator nicht doppelt arbeitet. Neues Protokoll `/swarmainet/lease/0.1` und optionales Feld `job` in Teilaufträgen. ([Kooperation](../spec/de/cooperation.md) § 7.4, [Protokoll](../spec/de/protocol-v0.1.md) § 4.2)

## [0.7.0] – 2026-11-24

Ein Planer kann mehrteilige Aufgaben jetzt in einen Graphen aus Teilaufgaben zerlegen, die verschiedene Modelle bearbeiten (Muster `decompose`), und Reasoning-Modelle bekommen mehr Raum: Die Standard-Token-Grenze steigt von 2048 auf 8192. Das Protokoll bleibt bei Version 0.1 ohne neue Felder: Knoten mit 0.7.0 und 0.6.0 arbeiten zusammen. Bestehende Konfigurationen funktionieren ohne Änderung weiter.

### Hinzugefügt

- **Task-Graph (Kooperation Stufe 4):** neues Muster `decompose` (Modellname `swarmainet/decompose`). Ein Planer, bevorzugt das eigene Modell, zerlegt eine mehrteilige Aufgabe in Teilaufgaben mit Abhängigkeiten; unabhängige laufen parallel, jede mit eigenem Muster, und eine Synthese führt die Ergebnisse zusammen. Scheitert eine Teilaufgabe, entfallen ihre Nachfolger, und die Lücke wird genannt. Gibt es nichts zu zerlegen oder ist der Plan ungültig, läuft der Auftrag ohne Zerlegung. Die Antwort nennt die Teilaufgaben unter `graph`. Vorerst nur auf Wunsch; der Bandit wählt es nicht. ([Kooperation](../spec/de/cooperation.md) § 6, [Nutzung](../docs/de/usage.md#modellnamen-wie-der-schwarm-arbeitet))

### Geändert

- Der Standard für `tasks.max_tokens` ist jetzt 8192 statt 2048: Reasoning-Modelle mit langen Verläufen (etwa Open WebUI mit Werkzeugen) verbrauchten 2048 Tokens schon mit Nachdenken. Der Anbieter senkt die Grenze, damit Eingabe und Antwort in den `context` des Modells passen. ([Konfiguration](../docs/de/configuration.md))
- Verbraucht ein Modell `max_tokens` ohne Antwort, meldet der Anbieter jetzt `token-limit erreicht` statt `ausführung fehlgeschlagen`, und der Gateway ergänzt, wie es sich beheben lässt. ([Protokoll](../spec/de/protocol-v0.1.md) § 4.2, [Nutzung](../docs/de/usage.md#diagnose))

### Hinweise zum Aktualisieren

- Ohne `tasks.max_tokens` in der Konfiguration gilt der neue Standard 8192; ein ausdrücklich gesetzter Wert (etwa der frühere Beispielwert 2048) bleibt und sollte für Reasoning-Modelle erhöht werden. Antworten können jetzt länger dauern und mehr GPU-Zeit brauchen.
- Den Grund `token-limit erreicht` meldet der Anbieter: Modelle auf Knoten mit 0.6.0 oder älter melden weiterhin nur `ausführung fehlgeschlagen`.
- `decompose` braucht keine Konfiguration. Es kostet je einen Teilauftrag für Planer und Synthese und dazu die Teilaufgaben selbst; mit dem Standard `tasks.max_subtasks: 8` darf der Planer höchstens 3 Teilaufgaben bilden, mit 14 das Maximum von 6.

## [0.6.0] – 2026-11-24

Anfragen dürfen jetzt Bilder enthalten, und mit `reduced` bekommen fremde Knoten nur noch eine gekürzte Aufgabe statt des ganzen Gesprächs. Mit dem Bandit passen zwei Regler den Prüfaufwand an die Sollwerte an. Das Protokoll bleibt bei Version 0.1 und bekommt nur das optionale Feld `images`: Knoten mit 0.6.0 und 0.5.0 arbeiten zusammen. Bestehende Konfigurationen funktionieren ohne Änderung weiter.

### Hinzugefügt

- **Bilder in Anfragen:** Chat-Nachrichten dürfen Bilder als `image_url`-Teile mit eingebetteter data-URL enthalten (PNG, JPEG, WebP, GIF, zusammen höchstens 2,75 MiB), so wie Open WebUI sie schickt. Sie gehen nur an Modelle mit der neuen Einstellung `vision: true`, auch als Prüfer oder Synthese; das Backend bekommt sie im OpenAI-Format. Bei `reduced` bleiben Anfragen mit Bildern im vertrauten Kreis, weil sich ein Bild nicht pseudonymisieren lässt. Neues optionales Protokollfeld `images` in den Nachrichten; Anbieter mit älteren Versionen melden nie die Modalität `image` und bekommen deshalb nie Bilder. ([Nutzung](../docs/de/usage.md#coding-agenten-und-werkzeuge), [Freigabeklassen](../spec/de/privacy-classes.md) Regel 10, [Protokoll](../spec/de/protocol-v0.1.md) § 4.2)
- **Minimierung für `reduced`:** Enthält eine Anfrage einen Verlauf oder Systemanweisungen, schreibt ein eigenes oder vertrautes Modell zuerst die letzte Anfrage als in sich geschlossene Aufgabe, nur mit dem Nötigen; fremde Knoten, auch als Prüfer oder Synthese, bekommen diese Aufgabe pseudonymisiert statt des Gesprächs. Ohne sie geht nichts an Fremde. Mit Werkzeugen bleibt der Verlauf vollständig. ([Freigabeklassen](../spec/de/privacy-classes.md) Regel 9)
- **Regler für den Aufwand (Kooperation Stufe 3):** Mit `cooperation.selection: bandit` hält ein PI-Regler je Aufgabenkategorie die Erfolgsquote beim Sollwert `quality`, indem er dem Bandit einen Mindestaufwand vorgibt, und ein zweiter die Fehlerquote der günstigen Stufe der Kaskade beim neuen Sollwert `cascade_error` (Standard 0,05) über eine Konfidenzschwelle `θ`: Meldet der Prüfer `ok` mit einer Konfidenz unter `θ`, eskaliert die Kaskade. Beide lernen aus Rückmeldungen zum Endergebnis; ihr Zustand steht in `GET /api/profile` unter `controls` und übersteht Neustarts. Mit Simulationen geprüft, noch nicht mit echten Modellen. ([Kooperation](../spec/de/cooperation.md) § 4.2, § 5)
- **Container-Image mit `ffmpeg`:** `docker build --target audio` baut eine Variante (Alpine, etwa 230 MB statt 50 MB) für ein Gateway, das lange Aufnahmen teilt. ([Installation](../docs/de/installation.md#dauerbetrieb))

### Geändert

- Bei `reduced` sind die Platzhalter je Teilauftrag ab 1 nummeriert: Ein fremder Agent erkennt an `[PERSON_2]` nicht mehr, dass die Anfrage noch eine weitere Person nennt.
- Beschränkt die Anfrage die Auswahl auf Agenten (`X-SwarmAiNet-Agents`), die die Freigabeklasse ausschließt, antwortet das Gateway jetzt mit 400 und dem Grund statt mit 502.

### Hinweise zum Aktualisieren

- Bilder gehen nur an Modelle mit `vision: true`; für Modelle, die Bilder verstehen, in `backends[].models` ergänzen. Ein Modell auf einem anderen Knoten bekommt Bilder erst, wenn dieser Knoten 0.6.0 verwendet; auch der Knoten, dessen Gateway die Anfrage annimmt, braucht 0.6.0.
- Mit `reduced` kosten Anfragen mit Verlauf oder Systemanweisungen jetzt einen weiteren Modellaufruf beim eigenen oder einem vertrauten Modell (Kürzen), aber nur, wenn ein fremder Agent beteiligt ist.
- Die Regler wirken nur mit `cooperation.selection: bandit` und beginnen ohne Vorgaben (`θ` = 0, kein Mindestaufwand); sie brauchen Rückmeldungen über `POST /v1/feedback`, um zu lernen.
- Für ein Gateway im Container, das lange Aufnahmen teilt, das Image mit `--target audio` bauen.

## [0.5.0] – 2026-11-23

Fremde Knoten können jetzt auch bei persönlichen Anfragen helfen: Mit der Freigabeklasse `reduced` sehen sie nur Platzhalter statt Namen, Adressen und Kontonummern. Pause und Sofort-Stopp überstehen jetzt einen Neustart. Das Protokoll bleibt bei Version 0.1: Knoten mit 0.5.0 und 0.4.0 arbeiten zusammen. Bestehende Konfigurationen funktionieren ohne Änderung weiter.

### Hinzugefügt

- **Freigabeklasse `reduced` mit Pseudonymisierung:** Auch fremde Knoten bekommen jetzt `reduced`-Anfragen, aber nur mit Platzhaltern wie `[PERSON_1]` oder `[EMAIL_1]` statt Namen, Firmen, Orten, E-Mail-Adressen, Telefonnummern, IBAN, Kartennummern, IP-Adressen und Zugangsdaten in URLs; Antworten kommen mit den Originalen zurück, auch beim Streaming. Namen erkennt ein eigenes oder vertrautes Modell, dazu kommt die neue Liste `tasks.pseudonymize_terms`; ohne Erkennung geht nichts an Fremde. Eigene und vertraute Knoten sehen weiter das Original; Audio bleibt im vertrauten Kreis. Anbieter nehmen `reduced` von Fremden mit `accept_public_tasks` an, wie bei `public`; Anbieter mit älteren Versionen lehnen weiter ab. ([Freigabeklassen](../spec/de/privacy-classes.md) Regel 9, [Nutzung](../docs/de/usage.md#optionale-header))

### Geändert

- **Pause und Sofort-Stopp überstehen einen Neustart:** `swarmainet pause` und `stop` werden in `~/.swarmainet/control.json` gespeichert und gelten auch nach einem Neustart weiter, etwa wenn systemd den Dienst neu startet, bis `swarmainet resume`. Das Startprotokoll und der erste Hinweis in `swarmainet status` sagen es, mit Zeitpunkt. Eine unlesbare Steuerdatei führt zum Sofort-Stopp, statt übergangen zu werden. Lässt sich der Zustand nicht speichern, gilt er trotzdem sofort; der Befehl meldet dann aber einen Fehler, weil er einen Neustart nicht übersteht. ([Nutzung](../docs/de/usage.md#befehle))

### Behoben

- Firewall-Hinweise in `swarmainet status` und `swarmainet ping` nennen die Ports, die der Knoten tatsächlich verwendet (etwa `4711/tcp und 4711/udp`), statt immer `4711/tcp`.

### Hinweise zum Aktualisieren

- Fremde bekommen `reduced`-Anfragen erst, wenn sie 0.5.0 verwenden und `accept_public_tasks` eingeschaltet haben; bis dahin bleiben solche Anfragen wie bisher bei eigenen und vertrauten Knoten. Auch der koordinierende Knoten braucht 0.5.0, dazu ein eigenes oder vertrautes Modell, das Namen erkennt; ohne ein solches geht nichts an Fremde.
- Die Pseudonymisierung kostet einen zusätzlichen Modellaufruf je Anfrage, aber nur, wenn wirklich ein fremder Agent beteiligt ist. Was den vertrauten Kreis auf keinen Fall verlassen darf, gehört zu `trusted`: Modelle können Namen übersehen, und mittelbare Hinweise wie Rollen oder ungewöhnliche Einzelheiten bleiben im Text.
- Ein Knoten, der vor dem Aktualisieren pausiert oder gestoppt war, läuft nach dem Aktualisieren normal an; ab dann gelten `pause` und `stop` bis `swarmainet resume`.

## [0.4.0] – 2026-11-23

Antworten kommen jetzt gestreamt, auch von Modellen auf anderen Knoten und mit Werkzeugen, und jedes Release bringt fertige Programmdateien für Linux, macOS und Windows mit. Das Protokoll bleibt bei Version 0.1 und bekommt nur optionale Felder: Knoten mit 0.4.0 und 0.3.0 arbeiten zusammen. Bestehende Konfigurationen funktionieren ohne Änderung weiter.

### Hinzugefügt

- **Streaming:** Mit `stream: true` kommt die Antwort Stück für Stück, auch von Modellen auf anderen Knoten. Ungeprüfter Text (Entwurf, Urteil, Nachdenken des Modells) steht in `delta.reasoning_content`, das Open WebUI als eingeklappte Denkphase zeigt; `delta.content` trägt nur Text, nach dem keine Prüfung mehr folgt. Findet der Prüfer keine Mängel, folgt der geprüfte Entwurf als ein Block. Mit Werkzeugen läuft der Text live, und die Werkzeug-Aufrufe folgen vollständig am Ende, nach der Prüfung gegen die erlaubten Werkzeuge. Neue optionale Protokollfelder: `stream` im Teilauftrag und `delta`/`reasoning` in `TASK_PROGRESS`; Anbieter mit älteren Versionen antworten als ein Block. ([Nutzung](../docs/de/usage.md#streaming), [Protokoll](../spec/de/protocol-v0.1.md) § 4.2)
- **Fertige Programmdateien:** Jedes Release ab 0.4.0 enthält Archive für Linux, macOS und Windows (amd64 und arm64) mit `SHA256SUMS`, reproduzierbar gebaut von einem GitHub-Workflow bei jedem Tag `v*` (`scripts/build-release.sh`). ([Installation](../docs/de/installation.md#fertige-programmdateien))
- **`swarmainet version`**; ein laufender Knoten nennt seine Version auch in `swarmainet status` und beim Start. Container-Images bekommen sie über das Build-Argument `VERSION`.

### Hinweise zum Aktualisieren

- Ein Modell auf einem anderen Knoten streamt erst, wenn dieser Knoten 0.4.0 verwendet; bis dahin kommt seine Antwort wie bisher als ein Block. Auch der Knoten, dessen Gateway die Anfrage annimmt, braucht 0.4.0.
- Clients, die `delta.reasoning_content` nicht auswerten, sehen Entwürfe und Urteile nicht; sie bekommen die Antwort, sobald sie beginnt. Open WebUI zeigt sie als eingeklappte Denkphase.
- Selbst gebaute Container-Images melden als Version `dev`, wenn sie nicht mitgegeben wird: `SWARMAINET_VERSION=$(git describe --tags --always) docker compose build`.

## [0.3.0] – 2026-11-22

Phase 3 beginnt: Jeder Knoten führt ein Kompetenzprofil und kann das Muster per Bandit wählen; Aufnahmen lassen sich über den Schwarm abschreiben. Das Protokoll bleibt bei Version 0.1 und bekommt nur das optionale Feld `audio`: Knoten mit 0.3.0 und 0.2.0 arbeiten zusammen, Transkription anbieten kann aber nur ein Knoten mit 0.3.0. Bestehende Konfigurationen funktionieren ohne Änderung weiter; der neue Abschnitt `cooperation` ist optional.

### Hinzugefügt

- **Transkription:** `POST /v1/audio/transcriptions` im Format der OpenAI-API lässt eine Aufnahme von einem Modell mit der Fähigkeit `audio.transcribe` abschreiben, auch auf einem anderen Knoten (neues optionales Feld `audio` im Teilauftrag, höchstens 2,75 MiB). Audio geht höchstens bis zur Freigabeklasse `trusted`; Koordinator und Anbieter setzen das beide durch. Kennt ein Backend `/audio/transcriptions` nicht (etwa vLLM mit Nemotron-Transcribe), schickt der Knoten die Aufnahme über den Chat als `audio_url` mit abgeschaltetem Denkmodus und entfernt Steuer-Markierungen des Modells aus der Abschrift. Aufnahmen über 2,75 MiB (bis 512 MiB) teilt das Gateway mit `ffmpeg` in überlappende Stücke von 10 Minuten, lässt sie parallel abschreiben und fügt sie in jeder Überlappung an der längsten gemeinsamen Wortfolge zusammen. ([Nutzung](../docs/de/usage.md#transkription), [Protokoll](../spec/de/protocol-v0.1.md) § 4.2)
- **Kompetenzprofil** (Phase 3, Stufe 1): Jeder Knoten bewertet die Agenten, mit denen er arbeitet, je Fähigkeit (Glicko-2) und führt eine Prüferstatistik aus Treffern, Fehlalarmen und übersehenen Fehlern. Urteile bewährter Prüfer zählen als Signal; Rückmeldungen über `POST /v1/feedback`, Anzeige über `GET /api/profile`, gespeichert in `~/.swarmainet/profile.json`. `swarmainet bench run` sendet seine Bewertungen als objektive Rückmeldung. Die Auswahl der Agenten nutzt das Profil noch nicht. ([Nutzung](../docs/de/usage.md#rückmeldungen-und-kompetenzprofil), [Kooperation](../spec/de/cooperation.md) § 2)
- `steps[].verdict` in der Antwort zeigt das Urteil jedes Prüfers.
- **Muster `cascade`** (`swarmainet/cascade`, auch für `tasks.pattern` zulässig): erst ein günstiges Modell, Prüfung durch eine andere Familie; nur bei Mängeln löst das nach gemessener Bewertung beste andere Modell neu (`escalated: true`).
- **Bandit über Muster** (Phase 3, Stufe 2, Opt-in über `cooperation.selection: bandit`): Bei Anfragen ohne Muster wählt der Knoten je Aufgabenkategorie per Thompson Sampling das günstigste Muster, das den Qualitäts-Sollwert voraussichtlich erreicht, und lernt aus Rückmeldungen zu Endergebnissen. Standard bleibt `rules`. Die Antwort nennt, wer das Muster gewählt hat (`selected_by`). ([Konfiguration](../docs/de/configuration.md#wahl-des-musters-cooperation), [Kooperation](../spec/de/cooperation.md) § 3, § 5)

### Geändert

- **Agenten ohne passende Fähigkeit** dienen nur noch als Rückfall, wenn gar kein Agent passt; Agenten mit nur Audio-Fähigkeiten bekommen nie Textaufgaben, und Text-Generalisten (`general`) nie Audio.

### Behoben

- **Wiederverbinden nach kurzem Abriss:** Zwei vertraute Knoten wählten einander bisher im selben Augenblick neu an. Über TCP mit Port-Wiederverwendung entstand dabei ein Simultaneous Open, bei dem der Handshake auf beiden Seiten scheiterte, und beide sperrten einander danach einige Sekunden (dial backoff); ein Auftrag in dieser Zeit scheiterte, obwohl beide erreichbar waren. Jetzt wählt nur die Seite mit der kleineren Peer-ID sofort neu, die andere nach 2 bis 3 Sekunden, falls noch nötig, und ein Auftrag, der auf eine Wählsperre trifft, hebt sie auf und wählt einmal neu. ([Protokoll](../spec/de/protocol-v0.1.md) § 4.1)

### Hinweise zum Aktualisieren

- Für die Transkription braucht der Knoten mit dem Modell 0.3.0, ebenso der Knoten, dessen Gateway die Aufnahme annimmt. Um Aufnahmen über 2,75 MiB zu teilen, braucht dieser Gateway-Knoten außerdem `ffmpeg` und `ffprobe` (Debian/Ubuntu: `apt install ffmpeg`).
- Beim ersten Start legt der Knoten `~/.swarmainet/profile.json` für das Kompetenzprofil an. Die Wahl des Musters ändert sich nur mit `cooperation.selection: bandit`.
- Die Korrektur beim Wiederverbinden wirkt erst ganz, wenn beide Seiten einer Verbindung 0.3.0 verwenden.

## [0.2.0] – 2026-11-21

Das Protokoll ist unverändert (Version 0.1): Knoten mit 0.2.0 und 0.1.0 arbeiten zusammen. Bestehende Konfigurationen funktionieren ohne Änderung weiter.

### Geändert

- **`swarmainet init` richtet jetzt ein eigenes privates Netz ein**, mit zufälliger Kennung `p-…` und frischem Schlüssel, statt dem offenen Netz beizutreten. So wird niemand ungewollt für Fremde sichtbar. Das bisherige Verhalten gibt es mit `swarmainet init -open`. ([Netzarten](../docs/de/networks.md#privates-netz-eigene-geräte))
- **`swarmainet join` ersetzt das von `init` angelegte private Netz**, solange der Knoten noch nie lief und keine Einstiegsadressen hat, statt ein zweites Netz daneben anzulegen. Netze, die schon in Gebrauch sind, bleiben unangetastet.
- **Einstiegsknoten und DNS-Seeds werden erneut angewählt**, wenn die DHT-Routing-Tabelle leer ist oder ein Knoten hinter NAT keinen Relay-Platz hat, nicht nur ohne jeden Peer. Zwei Knoten hinter NAT, die nach dem Ausfall des Startknotens nur noch einander kannten, blieben bisher eine Insel, die neue Knoten nicht fanden; jetzt verbinden sie sich wenige Sekunden nach seiner Rückkehr wieder mit ihm. Fehlgeschlagene Einstiegsversuche erscheinen nur noch auf Stufe INFO, solange gar kein Peer verbunden ist. ([P2P-Schicht](../spec/de/p2p-layer.md) § 2)
- **`dns_seeds` gilt auch für die Netze unter `node.networks`**, soweit sie keine eigenen angeben; Mitgliedsnetze fragen nie einen Seed.
- **Verständlichere Meldung, wenn die Freigabeklasse alle Agenten ausschließt:** Bei `trusted`, `reduced` oder `local` ohne passende Agenten nennt der Fehler den Grund und den Ausweg (`X-SwarmAiNet-Privacy: public`), und das Gateway antwortet mit **400** statt 502.
- **Dokumentation auf Englisch und Deutsch:** Die englische Fassung ist jetzt die Hauptfassung; die deutsche liegt mit gleichem Dateinamen in Unterordnern `de/`. Die Doku-Dateien haben englische Namen bekommen, z. B. `docs/netze.md` → `docs/networks.md` (Deutsch: `docs/de/networks.md`), `spec/p2p-schicht.md` → `spec/p2p-layer.md`. Links auf die alten Pfade funktionieren nicht mehr.
- **Gesamtkonzept** auf aktuellen Stand gebracht, als Markdown in beiden Sprachen ([Konzept](../docs/de/concept.md)), mit neuen Grafiken; das Original vom 14.10.2026 liegt als PDF im Archiv.

### Hinzugefügt

- **QR-Code für Einladungen:** `swarmainet invite -qr` und `swarmainet member invite -qr` zeigen den Link zusätzlich als QR-Code im Terminal (neue Abhängigkeit `rsc.io/qr`, BSD-Lizenz).
- **Hardwaretest des offenen Netzes** über zwei Standorte ([P2P-Schicht](../spec/de/p2p-layer.md) § 15): Einstieg allein über den DNS-Seed, öffentlicher Auftrag zwischen Heimnetz und Mobilfunk, Hole Punching über IPv6/QUIC, Ausfall des Startknotens, Werkzeugrechte für Fremde.

### Hinweise zum Aktualisieren

- Trägt das eigene private Netz noch die Kennung `swarmainet` (den Namen des offenen Netzes), es auf allen Knoten gleichzeitig in eine eigene Kennung umbenennen, etwa `p-…` ([P2P-Schicht](../spec/de/p2p-layer.md) § 1).
- Von der Korrektur der Wiederanwahl profitieren Knoten hinter NAT erst, wenn sie 0.2.0 verwenden; Startknoten und Helfer betrifft sie nicht.

## [0.1.0] – 2026-11-17

Erste öffentliche Version: lokaler Schwarm (Phase 1) und P2P-Schicht mit privaten Netzen und Mitgliedsnetzen, auf Hardware über zwei Standorte getestet.

[0.7.0]: https://github.com/rainer-sharp/Ai-Swarms.net/compare/v0.6.0...v0.7.0
[0.6.0]: https://github.com/rainer-sharp/Ai-Swarms.net/compare/v0.5.0...v0.6.0
[0.5.0]: https://github.com/rainer-sharp/Ai-Swarms.net/compare/v0.4.0...v0.5.0
[0.4.0]: https://github.com/rainer-sharp/Ai-Swarms.net/compare/v0.3.0...v0.4.0
[0.3.0]: https://github.com/rainer-sharp/Ai-Swarms.net/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/rainer-sharp/Ai-Swarms.net/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/rainer-sharp/Ai-Swarms.net/releases/tag/v0.1.0
