# Bedrohungsmodell v0.1

[English](../threat-model.md) · **Deutsch**

**Schützenswert:** Prompts und Ergebnisse der Nutzer, die Hardware und Energie der Betreiber, die Integrität der Ergebnisse, die Verfügbarkeit des Netzes.

**Nicht versprochen:** Geheimhaltung auf fremden Rechnern, Korrektheit von Mehrheitsantworten, vollständige Sybil-Resistenz, grenzenlose Echtzeit-Skalierung.

## Angreifer

| Angreifer | Fähigkeiten |
|---|---|
| Neugieriger Anbieter | liest alle Prompts, die er verarbeitet |
| Böswilliger Anbieter | liefert falsche oder manipulierte Ergebnisse, auch Prompt-Injection |
| Trittbrettfahrer | nutzt Kapazität, ohne beizutragen |
| Sybil | erzeugt beliebig viele Identitäten |
| Netzangreifer | Replay, Eclipse, DHT-Vergiftung, Gossip-Spam |
| Webseite im Browser des Betreibers | spricht das lokale Gateway an (CSRF, DNS-Rebinding) |

## Angriffe und Gegenmaßnahmen

| Angriff | Gegenmaßnahme | Stand |
|---|---|---|
| Datenabgriff | Freigabeklassen, Fragmentierung, sensible Teile lokal | Klassen umgesetzt; Fragmentierung offen |
| Gefälschte Ergebnisse | Prüfer anderer Familien, Canary-Aufgaben, Stichproben | Prüfer anderer Familie umgesetzt; Canary ab Phase 3 |
| Überhöhte Selbsteinschätzung (`strength`) | wirkt nur als Reihung bei gleicher Eignung; Prüfer bleibt aus anderer Familie; gemessene Bewertung (Elo) ersetzt die Angabe | Phase 1: nur vertrauter Kreis; Elo ab Phase 3 |
| Prompt-Injection zwischen Modellen | Ergebnisse sind Daten; Begrenzer; strukturierte Ausgaben; Anweisungsmuster verwerfen | umgesetzt (Heuristik) |
| Untergeschobene Werkzeug-Aufrufe (ein Anbieter lässt den lokalen Agenten Befehle ausführen), Abfluss von Werkzeug-Ergebnissen | Werkzeugliste je Netz, ohne Liste keine bei Fremden und in Mitgliedsnetzen; nicht genannte Aufrufe werden verworfen, Verlauf mit anderen Werkzeugen geht nicht an eingeschränkte Agenten; nur Muster `single`, nur Modelle mit `tools: true`. Die Liste prüft nur Namen; Bestätigung riskanter Aufrufe bleibt Aufgabe des Agenten | umgesetzt |
| Replay | Nonce + Ablaufzeit + Signatur | umgesetzt |
| Identitätsfälschung | Absender = Gegenstelle der Verbindung, Signatur über Peer-ID-Schlüssel | umgesetzt |
| Überlastung des Anbieters | REM: Slots, Zeitfenster, GPU-Minuten, mittlere Auslastung, Temperatur, RAM-Reserve; max. 4 Streams pro Peer; 4 MiB pro Nachricht | umgesetzt |
| Eigenbedarf verdrängt | eigene Nutzung bricht Fremdjobs ab | umgesetzt |
| Zugriff auf Gateway von außen | Bindung nur an localhost, Prüfung von Absender und Host-Header | umgesetzt |
| Trittbrettfahrer | Kreditlimits, Tit-for-Tat, begrenztes Optimistic Unchoking | Quittungen ab Phase 1, Kredit ab Phase 4 |
| Sybil | Proof of Useful Work, erarbeitetes Vertrauen, Bürgschaften | Phase 1: nur vertrauter Kreis |
| Bewertungs-Kartelle | subjektives Vertrauen, objektive Prüfungen vor Meinungen | Phase 3 |
| Benchmark-Manipulation | Commit-Reveal, Rotation, Reputation der Einreicher | Phase 4 |
| Eclipse | viele Einstiegsquellen, Peers aus verschiedenen Netzen, vertraute Peers merken, Diversitätsfilter in Peer-Cache und Routing-Tabelle | Phase 2, umgesetzt ([P2P-Schicht](p2p-layer.md) § 2.1, § 8) |
| DHT-Vergiftung, Gossip-Spam | keine frei beschreibbaren DHT-Werte, Provider-Einträge nur für sich selbst, Fähigkeiten signiert direkt beim Peer, kein Gossip in Phase 2, Ratenlimits | Phase 2, umgesetzt ([P2P-Schicht](p2p-layer.md) § 4, § 5, § 8) |
| Missbrauch fremder Relays | im offenen Netz automatisch nur bei öffentlich erreichbaren Knoten und mit sparsamem Zeit-, Daten- und Verbindungsbudget, abschaltbar; Inhalt Ende-zu-Ende verschlüsselt, Metadaten sichtbar | Phase 2, umgesetzt ([P2P-Schicht](p2p-layer.md) § 6) |
| Verknüpfung privater und öffentlicher Teilnahme | eigener Schlüssel und eigener Host je Netz; private Netze nie im offenen Netz veröffentlicht | Phase 2, umgesetzt ([P2P-Schicht](p2p-layer.md) § 9) |
| Übernommener Helfer, ausgeschiedenes Mitglied, abgefangene Einladung in Gruppen | Mitgliedsnetze: Zertifikat je Knoten, Rolle `helper` ohne Aufträge, Widerruf einzeln, Einladung einmalig ([Mitgliedsnetze](membership.md) § 9) | umgesetzt, auf Hardware über zwei Standorte geprüft; im PSK-Netz Widerruf nur durch neuen Schlüssel für alle |
| Ausführung fremden Codes | Wasm-Sandbox ohne Netz/Dateien, unter Linux zusätzlich Container | Phase 4 (Sandbox-Audit) |

## Grundsätze

- Keine sicherheitskritische Entscheidung allein auf Basis von Peer-Stimmen.
- Fremde Aufträge haben keine Shell-, Datei-, Browser- oder Netzwerkrechte.
- Fehlende oder veraltete Messwerte führen zu „keine neuen Jobs“, nicht zu „alles erlaubt“.
