# Aufbau

[English](../architecture.md) · **Deutsch**

Ein Knoten ist ein einzelnes Go-Programm. Es nimmt Anfragen über eine lokale OpenAI-kompatible API an, zerlegt sie nach einem Muster in Teilaufträge, wählt dafür eigene und entfernte Modelle aus und lässt Ergebnisse von einer anderen Modellfamilie prüfen. Entfernte Modelle erreicht es über libp2p; jeder Teilauftrag ist eine signierte Nachricht, und beide Seiten quittieren ihn.

```
Anwendung (Open WebUI, IDE, Agent)
        │  OpenAI-API, nur localhost
        ▼
   Gateway ──► Orchestrator ──► lokale Backends (Ollama, llama.cpp, vLLM …)
                    │
                    ▼
              P2P-Schicht ──► andere Knoten (direkt, Hole Punching oder Relay)
                    ▲
     Ressourcenmanager entscheidet, ob fremde Aufträge angenommen werden
```

| Modul | Paket |
|---|---|
| Gateway | `internal/gateway` – OpenAI-API, Status-API, Web-Oberfläche, nur localhost |
| Auswahl | `internal/orchestrator` (`candidates`, `Route`) – regelbasiert: Fähigkeit, bei Gleichstand Lastverteilung (freie Slots, laufende Teilaufträge, im Wechsel), dann Kontextgröße; Prüfer nach Modellstärke (`strength`), dann Kontextgröße, dann eigene vor entfernten |
| Orchestrierung | `internal/orchestrator` – Muster `single`, `parallel`, `draft_verify_synthesize`, `parallel_verify`; Werkzeugfreigaben je Netz |
| Vertrauen | `internal/receipt` – beidseitig signierte Quittungen; `internal/membership` – Zertifikate, Ketten, Sperrlisten, Einladungen |
| Ressourcen | `internal/rem` – Slots, Zeitfenster, GPU-Minuten, mittlere GPU-Auslastung, Temperatur, RAM-Reserve, PV-Hysterese, Eigenbedarf, Sofort-Stopp |
| P2P-Transport | `internal/p2p` – je Netz ein libp2p-Host (QUIC/TCP, PSK), mDNS, statische und Einstiegsadressen, Relay mit Reservierungen, Hole Punching, AutoNAT, Peer-Austausch, Peer-Cache, Kademlia-DHT, Zulassung in Mitgliedsnetzen, Tracker-Client, Ratenlimits, Diagnose |
| Netzeintritt | `internal/invite` – Einladungslinks und Schlüsseldateien; `internal/tracker` – Tracker-Einträge, Server und Client |
| Protokoll | `internal/protocol` – signierte Envelopes, Replay-Schutz, Zustandsautomat |
| Lokale Backends | `internal/backend`, `internal/node/local.go` |
| Benchmark | `internal/bench`, Daten in `bench/` |
| Befehle | `cmd/swarmainet` |

Die verbindliche Beschreibung von Protokoll, Netzschicht und Sicherheitsannahmen steht in der [Spezifikation](../../spec/de/README.md). Das [Gesamtkonzept](concept.md) beschreibt das Ziel einschließlich der späteren Phasen.
