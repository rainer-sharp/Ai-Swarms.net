# Konfiguration

[English](../configuration.md) · **Deutsch**

Die Konfiguration liegt in `~/.swarmainet/config.yaml` (anlegen mit `swarmainet init`, andere Datei mit `-config PFAD`). Ein vollständiges, kommentiertes Beispiel mit allen Abschnitten ist [`config.example.yaml`](../../config.example.yaml):

| Abschnitt | Inhalt |
|---|---|
| `node` | Adressen, Gateway, Netz, Vertrauen, Erreichbarkeit (Tabelle unten) |
| `backends` | Inferenz-Server und ihre Modelle: `family`, `skills`, `context`, `strength`, `tools`, `offer` |
| `resources` | Ressourcenmanager: parallele Aufträge, GPU-Grenzen, Temperatur, RAM-Reserve, Zeitfenster, GPU-Minuten, Energie (PV-Überschuss, Strompreis über Home Assistant) |
| `tasks` | Standardmuster, Standard-Freigabeklasse, Token-Grenze, Frist, Teilaufträge; `pseudonymize_terms`: eigene Begriffe (Namen, Projekte, Kunden), die bei `reduced` für fremde Knoten immer ersetzt werden ([Nutzung](usage.md#optionale-header)) |
| `cooperation` | Wie das Muster gewählt wird, wenn eine Anfrage keines nennt (Tabelle unten) |

Geheimnisse gehören nicht in die Datei: Der Home-Assistant-Token kommt aus einer Umgebungsvariablen (`token_env`), Netzschlüssel stehen in eigenen Dateien mit Rechten 0600.

## Wahl des Musters (`cooperation.…`)

| Schlüssel | Standard | Bedeutung |
|---|---|---|
| `selection` | `rules` | `rules`: es gilt `tasks.pattern`; `bandit`: der Knoten wählt das Muster je Aufgabenkategorie nach seinem Kompetenzprofil ([Kooperation](../../spec/de/cooperation.md) § 3). Eine Anfrage mit `swarmainet/<muster>` bekommt immer dieses Muster |
| `exploration` | `0.05` | Anteil zufälliger Wahl unter den besetzbaren Mustern (0 bis 0,5) |
| `targets` | code 0,9, math 0,95, sonst 0,85 | Sollwerte je Kategorie, z. B. `code: { quality: 0.9 }`; `latency_p90_s` ist für Stufe 3 vorgesehen |

## Netz-Schlüssel (`node.…`)

| Schlüssel | Standard | Bedeutung |
|---|---|---|
| `listen` | 4711 TCP/QUIC, IPv4 und IPv6 | Listen-Adressen; `[]` heißt nur ausgehend, erreichbar über Relays |
| `gateway` | `127.0.0.1:7711` | OpenAI-API und Status-Seite, nur localhost |
| `network_id` | `swarmainet` | Netz-Kennung; `swarmainet` ist das offene Netz |
| `network_key_file` | – | Schlüsseldatei eines privaten Netzes (PSK) |
| `membership` | – | macht ein Netz zum Mitgliedsnetz: `root`, optional `cert_file` |
| `mdns` | `true` | andere Knoten im LAN automatisch finden |
| `static_peers` | `[]` | dauerhaft verbundene Adressen |
| `bootstrap` | `[]` | Einstiegsadressen, angewählt, solange der Knoten keine Peers, keine DHT-Partner oder hinter NAT keinen Relay-Platz hat (`join` trägt sie ein) |
| `trackers` | `[]` | HTTPS-Adressbriefkästen |
| `trusted_peers` | `[]` | Peer-IDs, denen vertrauliche Aufträge und alle Werkzeuge zugehen dürfen |
| `provide` | `false` | eigene Modelle im Netz anbieten |
| `allowed_tools` | PSK alle, sonst keine | Werkzeuge, die Modelle dieses Netzes sehen dürfen |
| `pex` | `true` | Peer-Austausch mit verbundenen Knoten |
| `relay_service` | `auto` | Verbindungen anderer weiterleiten: `auto` im offenen Netz bei öffentlicher Erreichbarkeit, `true` immer (Helfer), `false` nie |
| `relay_limits` | – | Grenzen des Relays: `reservations`, `circuits_per_peer`, `data_mib`, `duration_s` |
| `helper` | `false` | nur Netz tragen (Relay, DHT-Server), keine Modelle, keine Aufträge |
| `dht_server` | `auto` | `auto`: offenes Netz bei öffentlicher Erreichbarkeit, privat mit eigener Listen-Adresse oder als Relay; `true`/`false` erzwingen |
| `dns_seeds` | offen: `seed.ai-swarms.net` | Domains mit `_dnsaddr`-Einträgen für den Einstieg; `[none]` schaltet ab. Gilt auch für die Netze unter `networks`, soweit sie keine eigenen angeben; Mitgliedsnetze nutzen keine |
| `announce` | `[]` | zusätzlich bekannt gegebene Adressen ohne `/p2p`, z. B. `/dns4/swarm.example.de/tcp/4711` hinter einer Portfreigabe |
| `announce_private` | `false` | interne Adressen auch im offenen Netz veröffentlichen (nur Testnetze) |
| `port_mapping` | `false` | Portfreigabe per UPnP/NAT-PMP beim Router anfordern; Status unter `networks[].port_mapping` |
| `networks` | – | weitere Netze, je mit eigenem Port und `identity: separate` (Standard) oder `shared`; siehe [Netzarten](networks.md#mehrere-netze-gleichzeitig) |

## Dateien unter `~/.swarmainet/`

| Datei | Inhalt |
|---|---|
| `config.yaml` | Konfiguration |
| `identity.key` | privater Schlüssel des Knotens, bestimmt die Peer-ID |
| `*.key` | Netzschlüssel privater Netze |
| `peers-<network_id>.json` | zuletzt erfolgreich verbundene Peers |
| `profile.json` | Kompetenzprofil der Agenten (siehe [Nutzung](usage.md#rückmeldungen-und-kompetenzprofil)) |
| Dateien je Mitgliedsnetz | Zertifikat, Sperrlisten, beim Aussteller dessen Schlüssel und das Register offener Einladungen |

Schlüsseldateien nie weitergeben und nicht in ein Repository legen. Wer `identity.key` hat, tritt unter Ihrer Peer-ID auf.
