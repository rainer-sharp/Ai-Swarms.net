# Änderungen

[English](../CHANGELOG.md) · **Deutsch**

Alle nennenswerten Änderungen an SwarmAiNet. Das Format folgt [Keep a Changelog](https://keepachangelog.com/de/1.1.0/); die Versionen folgen [Semantic Versioning](https://semver.org/lang/de/) mit dem üblichen Vorbehalt für 0.x: Zwischen Minor-Versionen sind inkompatible Änderungen noch möglich.

## [Unveröffentlicht]

### Hinzugefügt

- **Streaming:** Mit `stream: true` kommt die Antwort Stück für Stück, auch von Modellen auf anderen Knoten. Ungeprüfter Text (Entwurf, Urteil, Nachdenken des Modells) steht in `delta.reasoning_content`, das Open WebUI als eingeklappte Denkphase zeigt; `delta.content` trägt nur Text, nach dem keine Prüfung mehr folgt. Findet der Prüfer keine Mängel, folgt der geprüfte Entwurf als ein Block. Mit Werkzeugen läuft der Text live, und die Werkzeug-Aufrufe folgen vollständig am Ende, nach der Prüfung gegen die erlaubten Werkzeuge. Neue optionale Protokollfelder: `stream` im Teilauftrag und `delta`/`reasoning` in `TASK_PROGRESS`; Anbieter mit älteren Versionen antworten als ein Block. ([Nutzung](../docs/de/usage.md#streaming), [Protokoll](../spec/de/protocol-v0.1.md) § 4.2)
- **Fertige Programmdateien:** Jedes Release ab 0.4.0 enthält Archive für Linux, macOS und Windows (amd64 und arm64) mit `SHA256SUMS`, reproduzierbar gebaut von einem GitHub-Workflow bei jedem Tag `v*` (`scripts/build-release.sh`). ([Installation](../docs/de/installation.md#fertige-programmdateien))
- **`swarmainet version`**; ein laufender Knoten nennt seine Version auch in `swarmainet status` und beim Start. Container-Images bekommen sie über das Build-Argument `VERSION`.

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

[0.3.0]: https://github.com/rainer-sharp/Ai-Swarms.net/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/rainer-sharp/Ai-Swarms.net/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/rainer-sharp/Ai-Swarms.net/releases/tag/v0.1.0
