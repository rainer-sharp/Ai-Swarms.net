# Stand der Entwicklung

[English](../status.md) · **Deutsch**

SwarmAiNet ist ein **Forschungsprototyp**. Die Netzschicht ist weiter entwickelt als die Zusammenarbeit der Modelle: Die Auswahl folgt bisher festen Regeln, und ein Qualitätsvorteil des Schwarms gegenüber dem besten Einzelmodell ist noch nicht nachgewiesen (siehe [Benchmark](#benchmark)).

## Phasen

| Phase | Inhalt | Stand |
|---|---|---|
| 0 · Spezifikation | Protokoll, Freigabeklassen, Bedrohungsmodell, Messplan | Entwurf v0.1 in [`spec/`](../../spec/de/README.md) |
| 1 · Lokaler Schwarm | mDNS, statische Peers, OpenAI-Backends, Ressourcenmanager, Muster, Lastverteilung, Modellstärke, Quittungen | **abgeschlossen**, auf echter Hardware getestet: 3 Geräte, 3 Modellfamilien, Ollama, llama.cpp und vLLM |
| 2 · P2P-Schicht | DHT, Einstiegsquellen, AutoNAT, Hole Punching, Relay; private, Mitglieds- und offene Netze, auch gleichzeitig | **privates Netz, Mitgliedsnetz und offenes Netz auf Hardware über zwei Standorte bestätigt** |
| 3 · Selbstorganisierte Kooperation | Kompetenzprofil, Bandit, PI-Regler, Task-Graph, Lease-Koordinator | Spezifikation im Entwurf: [`spec/de/cooperation.md`](../../spec/de/cooperation.md); **Stufen 1 und 2 umgesetzt** (Kompetenzprofil, Bandit über Muster als Opt-in, Kaskade) |
| 4 · Offenes Testnetz | Ein-Klick-Client, Kredit, Sandbox, Benchmark-Pool | offen |

## Erprobung auf Hardware

- **Lokaler Schwarm:** *„Zwei Modelle je Auftrag, Peer-Ausfall blockiert nicht“* – abgedeckt durch `internal/node/node_test.go` und im Betrieb mit drei Geräten bestätigt, einschließlich Ausweichen bei Absagen und Backend-Fehlern.
- **Privates Netz über Standorte:** Ein Notebook im Mobilfunknetz (CGNAT) und eine VM hinter einer FritzBox ohne Portfreigabe fanden sich über einen Helfer an einem anderen Anschluss. Ein Auftrag lief über das Relay, und nach einem Ausfall des Helfers verbanden sich alle Knoten von selbst wieder. Einzelheiten: [`spec/de/p2p-layer.md`](../../spec/de/p2p-layer.md) § 14.
- **Mitgliedsnetz über Standorte:** Aussteller im Heimnetz, Helfer auf einem VPS, neues Mitglied am Mobilfunk-Router. Beitritt per Einladung und Auftrag über das Relay gelangen; ein gesperrtes Mitglied verlor seine Zulassung nach 0,4 s. Einzelheiten: [`spec/de/membership.md`](../../spec/de/membership.md) § 11.
- **Offenes Netz über Standorte:** VM hinter einer FritzBox und Notebook im Mobilfunknetz fanden sich allein über den DNS-Seed und einen Startknoten auf einem VPS, ohne Einladung. Ein öffentlicher Auftrag lief, ein vertraulicher blieb auf dem Notebook. Hole Punching über IPv6/QUIC gelang, die Verbindung lief direkt. Fremde Modelle sahen nur die mit `allowed_tools` freigegebenen Werkzeuge. Einzelheiten: [`spec/de/p2p-layer.md`](../../spec/de/p2p-layer.md) § 15.
- **Werkzeuge:** Open WebUI mit Open Terminal über den Schwarm, Werkzeug-Aufrufe und Streaming.

## Benchmark

Runde 1 (80 Aufgaben aus HumanEval und GSM8K, [Bericht](../../bench/de/report-r1.md)): Der Schwarm schadet kaum, kostet aber das 3- bis 8-Fache an Tokens. Die Aufgaben waren für das beste Einzelmodell zu leicht, kein Unterschied ist signifikant. Runde 2 mit schwereren Aufgaben und unterschiedlicheren Modellen steht aus.

## Bekannte Lücken

**Zusammenarbeit der Modelle**

- Der Router ist eine Stichwort-Heuristik, kein Modell; Bandit und Kompetenzprofil folgen in Phase 3.
- Freigabeklasse `reduced` ohne Pseudonymisierung; sie wird bis dahin wie `trusted` behandelt.
- Kein Token-Streaming: Bei `stream: true` kommt die geprüfte Antwort als ein Block.
- Bilder in Anfragen werden nicht unterstützt. Audio nur als Transkription (`/v1/audio/transcriptions`, höchstens 2,75 MiB je Anfrage, nur bis `trusted`). Auf Hardware getestet: eine Aufnahme von 57 Minuten in sechs Stücken zu 10 Minuten von der VM über einen Knoten mit Nemotron-Transcribe in 122 s. Längere Aufnahmen muss der Aufrufer noch selbst teilen, ohne Behandlung von Überlappungen.

**Ressourcen**

- `vram_budget_gib` und `extra_power_max_w` werden noch nicht durchgesetzt; GPU-Messung nur über `nvidia-smi` (ROCm und macOS folgen).
- Energiequelle nur Home Assistant (REST, lesend); MQTT und Modbus/SunSpec folgen.
- Pause und Sofort-Stopp gelten bis zum Neustart.

**Netz**

- Noch nicht auf Hardware getestet: der Brückenknoten über VPN, Windows und macOS als Plattform, Mitgliedsnetze im Dauerbetrieb über Wochen.
- Hole Punching gelang im Hardwaretest über IPv6/QUIC, über IPv4 hinter Mobilfunk-CGNAT und über TCP nicht; dort laufen Verbindungen zwischen Standorten dauerhaft über einen Helfer.
- Portfreigabe nur per UPnP und NAT-PMP für IPv4, nicht per PCP; IPv6-Freigaben müssen im Router von Hand gesetzt werden. `port_mapping` ist nur mit einer NAT-Attrappe getestet.
- WebTransport/WebSocket über Port 443 fehlen; in Netzen, die nur Web-Verkehr erlauben, kommt ein Knoten nicht hinaus.
- `invite` braucht einen laufenden Knoten. Den Link liest ein Telefon mit `-qr` vom Bildschirm ab; auf ein anderes Gerät ohne Kamera muss er weiter auf anderem Weg (z. B. per Mail an sich selbst).
- Mitglieder eines privaten Netzes (PSK) lassen sich nicht einzeln entfernen; dafür gibt es Mitgliedsnetze.
- Der eingebaute DNS-Seed `seed.ai-swarms.net` nennt bisher nur einen Startknoten; das Konzept sieht mehrere unabhängige Betreiber vor.

**Verteilung**

- Keine fertigen Programmdateien; gebaut wird aus den Quellen.
- Protokoll in Version 0.1, inkompatible Änderungen sind möglich.
