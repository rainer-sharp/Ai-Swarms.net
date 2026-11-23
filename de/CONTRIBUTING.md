# Mitwirken

[English](../CONTRIBUTING.md) · **Deutsch**

Danke für Ihr Interesse. SwarmAiNet ist ein Forschungsprototyp; besonders hilfreich sind:

- **Fehlerberichte** mit Ausgabe von `swarmainet status`, dem Protokoll (`-v`) und einer Beschreibung des Netzes (Netzart, NAT, IPv6). Peer-IDs, Adressen und Einladungslinks vorher unkenntlich machen.
- **Erfahrungen aus anderen Netzen:** andere Router, Mobilfunk, DS-Lite, Firmennetze, Windows und macOS.
- **Messungen** mit anderen Modellen nach [`spec/de/measurement-plan.md`](../spec/de/measurement-plan.md) und [`bench/`](../bench/de/README.md).

Für größere Änderungen bitte zuerst ein Issue eröffnen, damit wir den Weg abstimmen können.

## Bauen und testen

```sh
go build -o swarmainet ./cmd/swarmainet
go vet ./...
go test -race ./...
```

Die Tests starten mehrere Knoten im selben Prozess und brauchen weder Netz noch Modelle. `internal/node` läuft etwa eine Minute.

## Veröffentlichen

Für Maintainer: den Abschnitt der Version in `CHANGELOG.md` und `de/CHANGELOG.md` vervollständigen, dann einen annotierten Tag pushen:

```sh
git tag -a v0.4.0 -m "SwarmAiNet 0.4.0"
git push origin v0.4.0
```

Der [Release-Workflow](../.github/workflows/release.yml) testet dann, baut mit [`scripts/build-release.sh`](../scripts/build-release.sh) die Archive für alle Plattformen und hängt sie mit `SHA256SUMS` an das GitHub-Release; den Text nimmt er aus dem englischen Changelog. Gibt es das Release schon, werden nur die Dateien ergänzt. Das Skript läuft auch lokal (`scripts/build-release.sh v0.4.0` schreibt nach `dist/`).

## Konventionen

- **Sprache:** Bezeichner im Code, Konfigurationsschlüssel und -werte, Protokoll- und API-Namen auf Englisch (z. B. `node.trusted_peers`, `tasks.pattern: draft_verify_synthesize`). Protokollmeldungen, Texte für Nutzer und Code-Kommentare auf Deutsch. Die Dokumentation gibt es auf Englisch und Deutsch: die englische Fassung am Hauptpfad, die deutsche mit gleichem Dateinamen im Unterordner `de/` daneben (z. B. `docs/networks.md` und `docs/de/networks.md`). Bitte beide Fassungen ändern oder im Pull Request vermerken, dass die andere noch nachzuziehen ist.
- **Protokoll:** Änderungen an `internal/protocol` und an den libp2p-Protokollen gegen [`spec/`](../spec/de/README.md) abgleichen und die Spezifikation im selben Pull Request anpassen.
- **Sicherheit:** Das Gateway lauscht nur auf localhost; das bleibt so. Jede neue Prüfung (Zulassung, Rechte, Signaturen) bekommt einen Test, der fehlschlägt, wenn man die Prüfung entfernt.
- **Abhängigkeiten:** sparsam; neue Abhängigkeiten im Pull Request begründen, Lizenz siehe unten.
- **Formatierung:** `gofmt`.

## Lizenz und Beitragsvereinbarung

SwarmAiNet steht unter der [GNU AGPL-3.0](../LICENSE) und zusätzlich unter einer [kommerziellen Lizenz](../COMMERCIAL.md). Damit beide möglich bleiben, nehmen wir Beiträge nur mit der [Beitragsvereinbarung](../CLA.md) auf: Sie behalten Ihre Rechte, räumen dem Projekt aber ein Nutzungsrecht ein, das auch die kommerzielle Lizenz abdeckt. Zustimmung per Kommentar im ersten Pull Request.

Beiträge zur Dokumentation (`docs/`, `spec/`, Anleitungen) stehen unter [CC BY 4.0](../LICENSE-DOCS); auch für sie gilt die Beitragsvereinbarung.

Neue Quelldateien beginnen mit:

```go
// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Rainer Sestig. Kommerzielle Lizenz: siehe COMMERCIAL.md.
```

Neue Abhängigkeiten müssen mit der AGPL-3.0 und mit der kommerziellen Lizenz verträglich sein, also etwa MIT, BSD, ISC, Apache-2.0 oder MPL-2.0; GPL- und AGPL-Bibliotheken anderer gehen nicht.
