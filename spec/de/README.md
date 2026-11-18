# Spezifikation (Phase 0)

[English](../README.md) · **Deutsch**

Versionierte, sprachunabhängige Spezifikation von SwarmAiNet. Grundlage ist das [Gesamtkonzept](../../docs/gesamtkonzept.pdf).

| Dokument | Inhalt |
|---|---|
| [protocol-v0.1.md](protocol-v0.1.md) | Identität, Transport, Envelope, Nachrichten, Ablauf, Zustände |
| [p2p-layer.md](p2p-layer.md) | Phase 2: private und offene Netze, Netzeintritt, Erreichbarkeit, DHT, Relay, Ergebnis auf Hardware |
| [membership.md](membership.md) | Entwurf: Mitgliedsnetze für Gruppen und Firmen, Zertifikate statt Netzschlüssel, Rollen, Widerruf |
| [cooperation.md](cooperation.md) | Phase 3 (Entwurf): Kompetenzprofil, Bandit über Rezepte, PI-Regler, Kaskade, Task-Graph, Lease-Koordinator |
| [privacy-classes.md](privacy-classes.md) | Wer darf welche Daten sehen |
| [threat-model.md](threat-model.md) | Angreifer, Angriffe, Gegenmaßnahmen, Umsetzungsstand |
| [measurement-plan.md](measurement-plan.md) | Einzelmodell gegen Schwarm bei gleichem Budget |
| [benchmark.md](benchmark.md) | Erste Benchmark-Runde: Aufbau, Aufgaben, Ablauf, Ergebnis, offene Fragen |

**Versionierung:** `MAJOR.MINOR`. Inkompatible Änderungen erhöhen die Protokoll-ID (`/swarmainet/<version>`). Neue optionale Payload-Felder sind kompatibel. Eine zweite, unabhängige Implementierung (z. B. Rust) prüft die Spezifikation auf Interoperabilität.

**Lizenz:** Die Spezifikation steht unter [CC BY 4.0](../../LICENSE-DOCS). Eigene Umsetzungen des Protokolls sind ausdrücklich erwünscht.

**Gate Phase 0:** Spezifikation versioniert, Testfälle und Bedrohungsmodell vorhanden. Die Testfälle liegen als Go-Tests neben dem Code (`go test ./...`).
