# Mitwirken

Danke für Ihr Interesse. SwarmAiNet ist ein Forschungsprototyp; besonders hilfreich sind:

- **Fehlerberichte** mit Ausgabe von `swarmainet status`, dem Protokoll (`-v`) und einer Beschreibung des Netzes (Netzart, NAT, IPv6). Peer-IDs, Adressen und Einladungslinks vorher unkenntlich machen.
- **Erfahrungen aus anderen Netzen:** andere Router, Mobilfunk, DS-Lite, Firmennetze, Windows und macOS.
- **Messungen** mit anderen Modellen nach [`spec/messplan.md`](spec/messplan.md) und [`bench/`](bench/README.md).

Für größere Änderungen bitte zuerst ein Issue eröffnen, damit wir den Weg abstimmen können.

## Bauen und testen

```sh
go build -o swarmainet ./cmd/swarmainet
go vet ./...
go test -race ./...
```

Die Tests starten mehrere Knoten im selben Prozess und brauchen weder Netz noch Modelle. `internal/node` läuft etwa eine Minute.

## Konventionen

- **Sprache:** Bezeichner im Code, Konfigurationsschlüssel und -werte, Protokoll- und API-Namen auf Englisch (z. B. `node.trusted_peers`, `tasks.pattern: draft_verify_synthesize`). Protokollmeldungen, Texte für Nutzer und Dokumentation auf Deutsch.
- **Protokoll:** Änderungen an `internal/protocol` und an den libp2p-Protokollen gegen [`spec/`](spec/README.md) abgleichen und die Spezifikation im selben Pull Request anpassen.
- **Sicherheit:** Das Gateway lauscht nur auf localhost; das bleibt so. Jede neue Prüfung (Zulassung, Rechte, Signaturen) bekommt einen Test, der fehlschlägt, wenn man die Prüfung entfernt.
- **Abhängigkeiten:** sparsam; neue Abhängigkeiten im Pull Request begründen, Lizenz siehe unten.
- **Formatierung:** `gofmt`.

## Lizenz und Beitragsvereinbarung

SwarmAiNet steht unter der [GNU AGPL-3.0](LICENSE) und zusätzlich unter einer [kommerziellen Lizenz](COMMERCIAL.md). Damit beide möglich bleiben, nehmen wir Beiträge nur mit der [Beitragsvereinbarung](CLA.md) auf: Sie behalten Ihre Rechte, räumen dem Projekt aber ein Nutzungsrecht ein, das auch die kommerzielle Lizenz abdeckt. Zustimmung per Kommentar im ersten Pull Request.

Beiträge zur Dokumentation (`docs/`, `spec/`, Anleitungen) stehen unter [CC BY 4.0](LICENSE-DOCS); auch für sie gilt die Beitragsvereinbarung.

Neue Quelldateien beginnen mit:

```go
// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Rainer Sestig. Kommerzielle Lizenz: siehe COMMERCIAL.md.
```

Neue Abhängigkeiten müssen mit der AGPL-3.0 und mit der kommerziellen Lizenz verträglich sein, also etwa MIT, BSD, ISC, Apache-2.0 oder MPL-2.0; GPL- und AGPL-Bibliotheken anderer gehen nicht.
