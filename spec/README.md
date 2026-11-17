# Spezifikation (Phase 0)

Versionierte, sprachunabhängige Spezifikation von SwarmAiNet. Grundlage ist das [Gesamtkonzept](../docs/gesamtkonzept.pdf).

| Dokument | Inhalt |
|---|---|
| [protokoll-v0.1.md](protokoll-v0.1.md) | Identität, Transport, Envelope, Nachrichten, Ablauf, Zustände |
| [p2p-schicht.md](p2p-schicht.md) | Phase 2: private und offene Netze, Netzeintritt, Erreichbarkeit, DHT, Relay, Ergebnis auf Hardware |
| [mitgliedschaft.md](mitgliedschaft.md) | Entwurf: Mitgliedsnetze für Gruppen und Firmen, Zertifikate statt Netzschlüssel, Rollen, Widerruf |
| [kooperation.md](kooperation.md) | Phase 3 (Entwurf): Kompetenzprofil, Bandit über Rezepte, PI-Regler, Kaskade, Task-Graph, Lease-Koordinator |
| [freigabeklassen.md](freigabeklassen.md) | Wer darf welche Daten sehen |
| [bedrohungsmodell.md](bedrohungsmodell.md) | Angreifer, Angriffe, Gegenmaßnahmen, Umsetzungsstand |
| [messplan.md](messplan.md) | Einzelmodell gegen Schwarm bei gleichem Budget |
| [benchmark.md](benchmark.md) | Erste Benchmark-Runde: Aufbau, Aufgaben, Ablauf, Ergebnis, offene Fragen |

**Versionierung:** `MAJOR.MINOR`. Inkompatible Änderungen erhöhen die Protokoll-ID (`/swarmainet/<version>`). Neue optionale Payload-Felder sind kompatibel. Eine zweite, unabhängige Implementierung (z. B. Rust) prüft die Spezifikation auf Interoperabilität.

**Lizenz:** Die Spezifikation steht unter [CC BY 4.0](../LICENSE-DOCS). Eigene Umsetzungen des Protokolls sind ausdrücklich erwünscht.

**Gate Phase 0:** Spezifikation versioniert, Testfälle und Bedrohungsmodell vorhanden. Die Testfälle liegen als Go-Tests neben dem Code (`go test ./...`).
