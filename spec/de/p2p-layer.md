# P2P-Schicht (Phase 2)

[English](../p2p-layer.md) · **Deutsch**

Status: umgesetzt, Gate für den privaten Schwarm am 29.10.2026 auf echter Hardware erfüllt (§ 14); das offene Netz am 18.11.2026 über zwei Standorte (§ 15). Umgesetzt: Peer-Cache (§ 2.1), Peer-Austausch (§ 2.4), Erreichbarkeit (§ 3, Portfreigabe per UPnP/NAT-PMP ohne PCP), DHT (§ 4), Fähigkeitsverweise (§ 5), Relay (§ 6), Helfer- und Brückenknoten (§ 6.1), Missbrauchsschutz (§ 8), Diagnose (§ 10), Einladung (§ 2.2), Tracker (§ 2.3), mehrere Netze gleichzeitig (§ 9). Ergänzt [Protokoll v0.1](protocol-v0.1.md) um Netzeintritt, Erreichbarkeit und Auffinden von Peers über das LAN hinaus. Die Nachrichten, der Envelope und der Ablauf eines Teilauftrags bleiben unverändert. Die Schlüsselwörter MUSS, SOLL und KANN sind normativ.

Grundsatz aus dem Gesamtkonzept: DHT und Relay liefern nur Kontakte, Adressen und Verweise, **keine Steuerung und kein Vertrauen**. Ob ein Peer vertraut ist und was er sehen darf, entscheiden weiterhin PSK, `trusted_peers` und die [Freigabeklassen](privacy-classes.md).

**Leitbild für das offene Netz: selbstverwaltend wie BitTorrent.** Kein Knoten und kein Betreiber ist unverzichtbar. Nach dem ersten Kontakt trägt sich das Netz selbst:
- Jeder öffentlich erreichbare Knoten wird automatisch DHT-Server.
- Peers tauschen Kontakte untereinander aus (Abschnitt 2.4).
- Jeder merkt sich funktionierende Peers (Abschnitt 2.1).

Bootstrap-Knoten, Tracker und DNS-Seeds sind nur Starthilfe, so wie Tracker und `router.bittorrent.com` bei BitTorrent. Jeder kann einen betreiben, und keiner hat Sonderrechte. Es gibt keine Registrierung und keine zentrale Liste der Teilnehmer.

## 1. Zwei Netzarten

Dieselbe Software unterstützt zwei Arten von Netzen. Ein Knoten kann an mehreren Netzen gleichzeitig teilnehmen (Abschnitt 9).

| | Privates Netz | Offenes Netz |
|---|---|---|
| Zweck | eigene Geräte oder ein geschlossener Kreis, auch über mehrere Standorte | beliebige fremde Knoten |
| Zugang | gemeinsamer PSK (libp2p `pnet`) | frei |
| Vertrauen | jeder Teilnehmer ist vertraut | nur Schlüssel aus `trusted_peers` |
| Freigabeklassen | `trusted`, `reduced`, `public` | `public`; `trusted`/`reduced` nur an `trusted_peers` |
| Transporte | nur TCP (Einschränkung von `pnet`) | QUIC, TCP, optional WebTransport/WebSocket |
| DHT | eigene, nur Mitglieder | gemeinsame, offen |
| Einstieg | Einladung, eigene Helferknoten, Tracker | Bootstrap-Liste, Tracker, DNS-Seeds, Einladung, danach Peer-Austausch und DHT |
| Relay | nur Relays desselben Netzes | Relays beliebiger Teilnehmer (Opt-in) |
| Größe | klein (bis etwa 64 Knoten), Vollvermaschung | groß, Teilvermaschung |

**Netz-Kennung (`network_id`).** Jedes Netz hat eine Kennung aus `[a-z0-9-]{1,32}`. Das offene Netz heißt `swarmainet`. Ein privates Netz MUSS eine andere Kennung verwenden, empfohlen: `p-` und 8 zufällige Hex-Zeichen, die `swarmainet invite` erzeugt. Die Kennung trennt mDNS-Dienst, DHT und Peer-Cache. Sie ist **kein Geheimnis**; der Schutz eines privaten Netzes liegt allein im PSK.

### 1.1 Folgen des PSK

`pnet` verschlüsselt jede Verbindung vor dem Handshake mit dem Netzschlüssel. Knoten ohne Schlüssel können keine Verbindung aufbauen und sehen nur Rauschen. Daraus folgt:

1. **Nur TCP.** QUIC und WebTransport sind mit `pnet` nicht nutzbar. Hole Punching geht über TCP (Simultaneous Open) und klappt seltener als über QUIC.
2. **Keine fremde Infrastruktur.** Öffentliche Bootstrap-Knoten, DHT-Server und Relays sind nicht erreichbar. Das private Netz trägt sich mit eigenen Helferknoten (Abschnitt 6.1). Ein Helfer braucht keine GPU und ist Einstieg, DHT-Server und Relay zugleich.
3. **Was wofür gebraucht wird:**

| Aufbau | nötig |
|---|---|
| ein Standort (LAN) | nichts Zusätzliches; mDNS, alle Knoten direkt verbunden |
| mehrere Standorte, Variante A: **Brückenknoten** | ein Helfer an einer Stelle, die beide Seiten **ausgehend** erreicht, z. B. eine VM mit VPN-Verbindung zum anderen Standort. Er muss selbst nicht erreichbar sein, kein Port wird geöffnet. |
| mehrere Standorte, Variante B: **Helfer im Internet** | ein Helfer auf einem Server mit öffentlicher Adresse (oder hinter einer Portfreigabe). Alle Standorte verbinden sich ausgehend zu ihm. Derselbe Server KANN zusätzlich Helfer des offenen Netzes sein (eigener Host und Port, Abschnitt 9). |

   Beide Varianten lassen sich kombinieren. Zwei Helfer an verschiedenen Stellen machen das Netz unabhängig vom Ausfall eines einzelnen.
4. **Ohne Helfer** finden sich nur Knoten im selben LAN (mDNS) und Knoten, die per IPv6 oder Portfreigabe direkt erreichbar sind. `swarmainet status` MUSS diesen Fall als Diagnose melden (Abschnitt 10).

## 2. Netzeintritt

Ein Knoten versucht beim Start parallel bzw. in dieser Reihenfolge, Kontakt herzustellen:

1. **Peer-Cache:** zuletzt erfolgreich verbundene Peers (Abschnitt 2.1). Ab dem zweiten Start die Hauptquelle.
2. **mDNS** im eigenen LAN (wie Phase 1, Dienstname `swarmainet-<network_id>`).
3. **Einladung:** beim ersten Beitritt importiert (Abschnitt 2.2).
4. **Bootstrap-Liste** (`bootstrap`): Multiadressen mit fester Peer-ID. Für das offene Netz liefert die Software eine Liste mehrerer unabhängiger Betreiber mit; für private Netze ist sie die eigene Einstiegsliste.
5. **Tracker** (`trackers`): HTTPS-Adressbriefkästen (Abschnitt 2.3).
6. **DNS-Seeds** (`dns_seeds`): `dnsaddr`-TXT-Einträge unter `_dnsaddr.<domain>` der Form `dnsaddr=/ip4/…/tcp/4711/p2p/<peer-id>`. Sie liefern nur Adressen; die Peer-ID in jedem Eintrag MUSS beim Verbinden geprüft werden. DNS ist ein Hinweis und nie ein Vertrauensnachweis. Im offenen Netz gilt ohne Angabe der eingebaute Seed `seed.ai-swarms.net`; `[none]` schaltet ab, eine eigene Liste ersetzt ihn. Die Angabe gilt auch für weitere Netze (§ 9), soweit sie keine eigene haben; Mitgliedsnetze fragen nie einen Seed. Angewählt werden bis zu 8 zufällig gewählte Einträge, solange der Knoten einen Einstieg braucht (siehe unten). Über den Seed lassen sich Einstiegsknoten austauschen, ohne die Software neu auszuliefern: Ein abgeschalteter Startknoten wird einfach aus dem DNS genommen, neue Helfer kommen hinzu.
7. **Statische Peers** (`static_peers`, wie Phase 1).

**Wann Einstiegsknoten angewählt werden.** Bootstrap-Liste und DNS-Seeds werden jede Minute angewählt, solange (a) kein Peer verbunden ist, (b) die DHT-Routing-Tabelle leer ist oder (c) ein Knoten ohne öffentliche Erreichbarkeit und ohne eigenes Relay keinen Relay-Platz hat. Bereits verbundene Einstiegsknoten werden übersprungen. Fehlgeschlagene Versuche werden nur protokolliert, solange gar kein Peer verbunden ist. Grund für (b) und (c): Zwei Knoten hinter NAT, die nach dem Ausfall des Startknotens nur noch einander kennen, bilden sonst eine Insel, die neue Knoten über den zurückgekehrten Startknoten nicht finden (§ 15).

Im laufenden Betrieb ergänzen **Peer-Austausch** (Abschnitt 2.4) und DHT die Kontakte, ohne dass eine der Starthilfen noch gebraucht wird.

Die Verbindung zu einer Adresse mit `/p2p/<peer-id>` MUSS abgebrochen werden, wenn sich die Gegenstelle mit einem anderen Schlüssel ausweist (das erledigt der libp2p-Handshake).

Danach füllt der Knoten seine DHT-Routing-Tabelle (Abschnitt 4), veröffentlicht nur bei `provide: true` seine Verweise (Abschnitt 5) und hält Verbindungen nach den Regeln aus Abschnitt 7.

Sind alle Einstiegsquellen ausgefallen, arbeiten bekannte Knoten über den Peer-Cache weiter; nur neue Knoten brauchen dann eine Einladung.

### 2.1 Peer-Cache

- Ablage: `<data_dir>/peers-<network_id>.json`, nur lokal. Inhalt je Peer: Peer-ID, zuletzt funktionierende Adressen, Zeitpunkt des letzten Erfolgs, Verbindungsart (direkt, Hole Punching, Relay).
- Höchstens 128 Einträge. Einträge ohne Erfolg seit 30 Tagen fallen heraus.
- Gegen Eclipse SOLL der Cache Vielfalt erhalten: höchstens 8 Einträge pro IPv4-/16 bzw. IPv6-/32 und pro Relay. `trusted_peers` und Bootstrap-Knoten werden bevorzugt behalten.
- Beim Start werden höchstens 16 Einträge gleichzeitig angewählt, vertraute Peers zuerst. Die übrigen Einträge stehen im privaten Netz für den Neuaufbau bereit (Abschnitt 7).
- Gespeichert wird jede Minute und beim Beenden; die Datei wird ersetzt, nicht überschrieben (erst temporäre Datei, dann umbenennen).

### 2.2 Einladung

`swarmainet invite [--with-key] [--qr]` erzeugt einen Link und optional einen QR-Code im Terminal (Fehlerkorrektur L, Rand zwei Module, schwarz auf weiß unabhängig vom Farbschema):

```
swarmainet://join?v=1&net=<network_id>&peer=<multiaddr mit /p2p/...>[&peer=...][&tracker=<https-url>][&psk=<base64url>]
```

- `peer` (ein- oder mehrmals): öffentlich erreichbare Adressen des Einladenden oder seiner Einstiegsknoten. Lokale Adressen nur, wenn ausdrücklich gewünscht (`--lan`).
- `psk` nur für private Netze und nur mit `--with-key`. **Wer den Link hat, ist Mitglied.** Der Link MUSS deshalb über einen vertraulichen Kanal weitergegeben werden. Die Software zeigt dazu eine Warnung an und speichert den Link nicht.
- `swarmainet join <link>` legt das Netz in der Konfiguration an, schreibt den Schlüssel nach `<data_dir>/<network_id>.key` (Modus 0600) und übernimmt die Adressen als Bootstrap-Liste (`node.bootstrap`: angewählt, solange der Knoten einen Einstieg braucht, § 2). Ohne Schlüssel im Link kann die Schlüsseldatei mit `-key DATEI` angegeben werden. Ein bestehender Schlüssel mit anderem Inhalt wird nicht überschrieben.
- `swarmainet invite` nimmt die Adressen des laufenden Knotens ohne Loopback, im offenen Netz nur öffentliche (mit `-lan` auch lokale); `-peer` ergänzt weitere, etwa die eines Brückenknotens.
- Eine Einladung ohne PSK enthält nur öffentliche Kontaktdaten und darf offen geteilt werden.

Ein PSK lässt sich nicht widerrufen. Scheidet ein Mitglied aus, bleibt nur ein neuer Schlüssel für alle (`swarmainet psk` + neue Einladungen). Mitglieder einzeln aufzunehmen und zu entfernen, ist eine offene Frage (Abschnitt 12).

### 2.3 Tracker (HTTPS-Briefkasten)

Ein Tracker ist ein HTTP-Endpunkt, bei dem Knoten ihre aktuellen Adressen hinterlegen und die Adressen anderer abholen. Die Schnittstelle ist bewusst einfach, damit Tracker unabhängig voneinander betrieben werden können. Die Referenz ist Teil der Software (`swarmainet tracker`, ein kleiner HTTP-Dienst ohne libp2p) und läuft z. B. auf einem Helferknoten hinter einem TLS-Reverse-Proxy. Der Tracker überwindet kein NAT. Er ersetzt DynDNS und feste Bootstrap-Adressen und hilft beim ersten Kontakt.

**Schnittstelle** (nur HTTPS):

| Aufruf | Wirkung |
|---|---|
| `POST <url>` mit `{"net": …, "id": …, "record": …}` | legt den Eintrag `id` im Netz `net` an oder ersetzt ihn |
| `GET <url>?net=<net>` | liefert `{"records": [...], "your_ip": "…"}`, höchstens 64 Einträge, zufällig ausgewählt |

Der Tracker speichert Einträge höchstens 1 h (Knoten erneuern alle 20 min), höchstens 2 KiB je Eintrag und 256 Einträge je Netz. Er SOLL Anfragen pro IP begrenzen (Referenz: 60 je Minute, höchstens 1024 Netze und 16384 Einträge insgesamt, nur im Speicher; hinter einem Reverse-Proxy zählt der letzte Eintrag in `X-Forwarded-For`, weil alle davor vom Absender stammen können). `your_ip` ist die Absenderadresse der Anfrage und nur ein Hinweis.

Knoten hinterlegen ihren Eintrag beim Start und alle 20 min, sofern sie anwählbare Adressen haben. Abgeholt wird beim Start und alle 20 min, solange weniger als 8 Peers verbunden sind, ohne jeden Peer jede Minute. Knoten akzeptieren Tracker nur über HTTPS (`http` nur für localhost, etwa hinter einem lokalen Reverse-Proxy). `swarmainet tracker` lauscht standardmäßig auf `127.0.0.1:7720` für einen TLS-Reverse-Proxy, mit `-tls-cert`/`-tls-key` selbst per HTTPS; `-trust-proxy` übernimmt die Absender-IP aus `X-Forwarded-For`.

**Offenes Netz:** `net = "swarmainet"`, `id` = Peer-ID, `record` im Klartext:

```json
{ "v": 1, "peer_id": "<peer-id>", "addrs": ["/ip4/…/tcp/4711", "…"],
  "expires_at": "…", "nonce": "<128 bit hex>", "signature": "<base64>" }
```

Signiert wird mit dem Schlüssel der Peer-ID über `"swarmainet/tracker/v1\n" + net + "\n" + peer_id + "\n" + expires_at + "\n" + nonce + "\n" + addrs` (Adressen durch `\n` getrennt). Knoten MÜSSEN Signatur und Ablaufzeit prüfen. Der Tracker KANN sie prüfen, muss es aber nicht. Es gelten die Adressregeln aus Abschnitt 3, private Adressen gehören also nicht hinein.

**Privates Netz:** Der Tracker darf weder die Netz-Kennung noch Mitglieder oder Adressen erfahren.
- `k = HKDF-SHA256(psk, info = "swarmainet/tracker/v1")`
- `net` = hex der ersten 16 Bytes von `HMAC-SHA256(k, "net")`
- `id` = hex der ersten 16 Bytes von `HMAC-SHA256(k, peer_id)`
- `record` = base64 von `nonce ‖ XChaCha20-Poly1305(k, nonce, Eintrag wie oben)`

Für den Tracker sind das undurchsichtige Daten. Wer den PSK nicht kennt, kann keine gültigen Einträge erzeugen. Er kann den Tracker nur mit Müll füllen, den Knoten beim Entschlüsseln verwerfen.

Ein Knoten KANN mehrere Tracker nutzen. Fällt einer aus, blockiert das nichts. Ein Tracker sieht die IP-Adressen der abfragenden Knoten. Das ist im offenen Netz ohnehin öffentlich und im privaten Netz der Preis für den einfachen Betrieb.

### 2.4 Peer-Austausch

Protokoll `/swarmainet/pex/0.1`, angelehnt an PEX bei BitTorrent: Knoten nennen einander Peers, mit denen sie gerade verbunden sind. So wächst die Kenntnis des Netzes ohne Einstiegsknoten.

- Abruf nach dem Verbindungsaufbau und danach alle 10 min. Eine Zeile JSON pro Richtung: `{"peers": [{"peer_id": "…", "addrs": ["…"]}]}`, höchstens 32 Peers, nur solche, mit denen in den letzten 30 min eine direkte oder Relay-Verbindung bestand.
- Nicht weitergegeben werden: im offenen Netz `trusted_peers` (sonst würde der Vertrauenskreis sichtbar) und private Adressen, außerdem Peers eines anderen Netzes. Im privaten Netz sind ohnehin alle Mitglieder vertraut und werden alle genannt.
- Im privaten Netz nennt ein Knoten auch sich selbst mit seinen eigenen Adressen, einschließlich der privaten. Identify verwirft private Adressen, wenn die Gegenstelle öffentlich aussieht, also auch über ein Relay im Internet; zwei Mitglieder im selben LAN fänden den direkten Weg sonst nicht.
- Ein Relay nennt für jeden Peer zusätzlich die Circuit-Adresse über sich selbst (Relay-Teil: die Adresse der Verbindung zum Anfragenden). Peers hinter NAT haben oft keine andere Adresse, und libp2p übernimmt Circuit-Adressen über den Empfänger nicht aus Identify.
- Wer neu hinzukommt, fragt seine ersten Peers sofort nach dem Identify und verbindet sich mit den genannten. Weil Verbindungen in beide Richtungen gelten, lernen die übrigen den Neuen dabei kennen, ohne erneut fragen zu müssen.
- Empfangene Peers sind Hinweise. Pro Antwort werden höchstens 8 neue Verbindungen versucht. Es gelten die Vielfaltsregeln aus Abschnitt 2.1. In den Peer-Cache kommt ein Peer erst nach einer erfolgreichen Verbindung.

## 3. Erreichbarkeit

Verbindungswege, automatisch in dieser Reihenfolge:

1. **Direkt** über QUIC/TCP, bevorzugt per öffentlicher IPv6-Adresse.
2. **Im LAN direkt**, gefunden per mDNS.
3. **Hole Punching (DCUtR)** über eine zunächst per Relay aufgebaute Verbindung.
4. **Relay** als Rückfall, mit Budget (Abschnitt 6).
5. Klappt nichts: klar „nicht verbunden“ melden und die Diagnose anbieten.

Bausteine:

- **AutoNAT v2** stellt fest, ob die eigenen Adressen von außen erreichbar sind (Ergebnis: `public`, `private`, `unknown`), und zwar je Adresse. Der Status fasst das je Adressfamilie zusammen (`reachability_by_family`): Hinter DS-Lite oder CGNAT ist IPv4 nie erreichbar, IPv6 mit Freigabe in der Router-Firewall schon; ein Hinweis erklärt das. Im privaten Netz prüfen das nur Mitglieder.
- **Identify/Observed Addresses** liefert die von außen gesehene Adresse.
- **Portfreigabe per UPnP/NAT-PMP/PCP** nur nach Opt-in (`port_mapping: true`, Standard aus, auch je Netz), weil sie Router-Einstellungen ändert. Umgesetzt mit dem NAT-Manager von libp2p (UPnP IGD und NAT-PMP; PCP fehlt): Er gibt die Listen-Ports frei, fordert Freigaben mit 1 Minute Laufzeit an und erneuert sie alle 20 s, sodass sie nach dem Ende des Knotens beim Router auslaufen. Die freigegebene öffentliche Adresse wird bekannt gegeben und von AutoNAT bestätigt. Der Status zeigt, ob ein Router gefunden wurde und welche Adressen freigegeben sind; nach einer Minute ohne Router oder ohne Freigabe erscheint ein Hinweis (bei der FritzBox muss für das Gerät „Selbstständige Portfreigaben erlauben“ gesetzt sein).
- **Adressen veröffentlichen:** Ein Knoten veröffentlicht nur Adressen, deren Erreichbarkeit bestätigt ist, und Relay-Adressen (`/p2p-circuit`). Im offenen Netz MÜSSEN private Adressen (RFC 1918, ULA `fc00::/7`, Link-Local, Loopback) aus Identify-Antworten und DHT-Einträgen entfernt werden, auch als Relay-Teil von Circuit-Adressen. Hinter einer Portfreigabe sieht ein Knoten seine öffentliche Adresse nicht; mit `announce` gibt er zusätzliche Adressen bekannt, z. B. `/dns4/swarm.example.de/tcp/4711` (Anleitung: `deploy/raspberry-pi/`). Loopback-Adressen gibt ein Knoten in keinem Netz weiter (Identify, Peer-Austausch, Tracker). Nur für Testnetze hebt `announce_private: true` beides auf. Interne IPs sind sonst öffentlich sichtbar, was die Capability-Regel aus dem Protokoll (§ 4.1) unterlaufen würde. Im LAN genügt mDNS. Im privaten Netz dürfen private Adressen veröffentlicht werden.

## 4. DHT

Kademlia nach libp2p-Spezifikation, mit eigenem Protokollpräfix je Netz:

| Netz | Protokoll-ID |
|---|---|
| offen | `/swarmainet/kad/1.0.0` |
| privat | `/swarmainet/<network_id>/kad/1.0.0` |

Das Präfix trennt den Schwarm von anderen libp2p-Netzen (z. B. IPFS) und private Netze untereinander. Bei privaten Netzen trennt es zusätzlich zum PSK.

- **Server- oder Client-Modus:** Wie bei BitTorrent wird jeder Knoten, für den AutoNAT `public` meldet, **automatisch** DHT-Server (`dht_server: auto`, Standard). Mit `dht_server: true` ist er es immer, mit `false` nie. Im privaten Netz SOLL `true` gesetzt werden, wenn der Knoten für die Mitglieder erreichbar ist oder sie von sich aus erreicht, auch ohne öffentliche Erreichbarkeit (Brückenknoten, Abschnitt 6.1). Nicht erreichbare Knoten sind Clients: Sie fragen, speichern aber nichts für andere.
- **Zweck 1, Peer-Routing:** Adressen zu einer bekannten Peer-ID finden. Damit erreicht ein Knoten seine `trusted_peers` an anderen Standorten, ohne deren IP zu kennen.
- **Zweck 2, Fähigkeitsverweise:** Abschnitt 5.
- Der Knoten SOLL keine beliebigen Werte (`PUT_VALUE`) annehmen. Gespeichert werden nur Provider-Einträge und Peer-Adressen. Damit gibt es keinen frei beschreibbaren Speicher, den Fremde füllen können.
- Validator-Namensraum `/swarmainet/` ist reserviert für spätere signierte Einträge (z. B. Benchmark-Pool, Phase 4).

## 5. Fähigkeiten finden

Die Capability-Anzeige (Protokoll § 4.1) bleibt die einzige Quelle für Fähigkeiten. Sie wird weiterhin direkt beim Peer abgerufen und von ihm signiert (`/swarmainet/capability/0.1`). Die DHT enthält nur **Verweise** „Peer X bietet Fähigkeit Y an“.

### 5.1 Schlüssel

Für jede angebotene Fähigkeit (`skills`, z. B. `code.python`) und jede Modalität (`modality.image` usw.) veröffentlicht ein Anbieter einen Provider-Eintrag unter

```
key(tag, shard) = CIDv1(raw, sha2-256("swarmainet/skill/v1\n" + network_id + "\n" + tag + "\n" + shard))
```

mit `shard` als Dezimalzahl in `0 … S-1`, `S = 4`. Jeder Anbieter wählt **einen** Shard je Tag zufällig und behält ihn, solange er läuft. So verteilt sich ein beliebter Tag auf vier DHT-Bereiche, und kein einzelner Knoten wird zum Engpass (Gesamtkonzept: „eigenes Indexschema über der DHT“). Suchende fragen alle `S` Shards ab.

Zusätzlich gibt es einen Sammelschlüssel `tag = "*"` für „bietet überhaupt etwas an“. Private Netze nutzen ihn als Mitgliederliste.

### 5.2 Ablauf

- Ein Anbieter veröffentlicht nur bei `provide: true` und nur Tags von Modellen mit `offer: true`. Neu veröffentlicht wird alle 12 h und sofort bei Änderung; schlägt das fehl (etwa mit noch leerer Routing-Tabelle), nach 5 s, 10 s … bis 1 min erneut. Die Gültigkeit auf den DHT-Servern ist die libp2p-Vorgabe von 48 h.
- Ein Koordinator sucht bei Bedarf nach dem benötigten Tag: im offenen Netz bei Aufträgen der Klasse `public`, wenn weniger als 2 verbundene Agenten die Fähigkeit anbieten, höchstens 5 s lang. Er verbindet sich mit höchstens 8 neuen Anbietern pro Suche, ruft deren Anzeige ab und übernimmt sie in seinen Katalog (TTL 5 min wie bisher).
- Die Auswahl der Agenten bleibt beim Orchestrator. Die DHT-Antwort ist eine ungeprüfte Kandidatenliste. Ein Anbieter, dessen Anzeige den Tag nicht enthält, wird für diesen Tag 1 h ignoriert.

**Sicherheit:** In Kademlia veröffentlicht ein Peer Provider-Einträge nur für sich selbst; die Gegenstelle MUSS prüfen, dass Provider und Absender übereinstimmen. Gefälschte Verweise können also nur auf den Fälscher selbst zeigen, nicht auf Dritte. Was dann übrig bleibt (viele Sybil-Identitäten unter einem Tag), begrenzen Abschnitt 8 und die Bewertung ab Phase 3.

### 5.3 Gossip

Fähigkeitsanzeigen werden in Phase 2 **nicht** per Gossip verbreitet. Abruf auf Anfrage plus DHT-Verweise genügen und bieten weniger Fläche für Spam. GossipSub bleibt eine Option für Phase 3, falls Suchlatenz oder Aktualität nicht reichen.

## 6. Relay

Circuit Relay v2 (`/libp2p/circuit/relay/0.2.0/hop` bzw. `/stop`).

- **Relay nutzen** (Client) ist Standard. Ein Knoten mit AutoNAT `private` reserviert bei bis zu 2 Relays seines Netzes einen Platz und veröffentlicht die Relay-Adressen.
- **Relay anbieten** (`relay_service`):
  - `auto` (Standard): im **offenen Netz** automatisch, sobald AutoNAT den Knoten als öffentlich erreichbar meldet, mit sparsameren Grenzen (siehe Tabelle); im privaten Netz aus. Das weicht vom ursprünglichen Opt-in des Gesamtkonzepts ab: Ohne Relays bei erreichbaren Knoten zerfällt das offene Netz in Inseln, sobald der Startknoten wegfällt, denn Knoten hinter NAT erreichen einander nur über ein Relay (wie bei BitTorrent tragen die erreichbaren Teilnehmer das Netz). Inhalte bleiben Ende-zu-Ende verschlüsselt, und die Grenzen beschränken Bandbreite und Verbindungen.
  - `true`: immer, auch ohne öffentliche Erreichbarkeit, z. B. Helfer- und Brückenknoten. Im privaten Netz zählt nur, dass die Mitglieder mit dem Relay verbunden sind, gleich wer die Verbindung aufgebaut hat.
  - `false`: nie.

  Standardgrenzen, einzeln überschreibbar mit `relay_limits` (`reservations`, `circuits_per_peer`, `data_mib`, `duration_s`):

| Grenze | Standard |
|---|---|
| Reservierungen gesamt | 32, bei `auto` 16 |
| Reservierungen pro IP | 4, bei `auto` 2 |
| Dauer einer Reservierung | 1 h |
| Verbindungen pro Peer | 2, im privaten Netz 16 |
| Dauer einer Relay-Verbindung | 10 min |
| Daten pro Relay-Verbindung | 8 MiB |

  Im privaten Netz entfallen die Grenzen je IP, weil über einen Brückenknoten jedes Mitglied der einen Seite jedes der anderen erreicht. Die libp2p-Standardgrenze von 128 KiB je Verbindung reicht für Aufträge nicht, 8 MiB decken eine Nachricht mit maximal 4 MiB in jede Richtung.
- Relay-Verbindungen enden nach Ablauf der Grenzen. Im privaten Netz baut jeder Knoten Verbindungen zu bekannten Mitgliedern jede Minute neu auf, bei Fehlschlägen mit Backoff bis 15 min (Abschnitt 7).
- Besteht zu einem vertrauten Peer nur eine Relay-Verbindung, versucht der Knoten nach Identify und danach jede Minute eine direkte Verbindung über die bekannten Adressen, unabhängig von DCUtR. Die begrenzte Verbindung läuft dann aus. Fremde Peers im offenen Netz bleiben bei DCUtR.
- Über ein Relay laufende Verbindungen sind *begrenzt*. Die Protokolle `capability` und `task` MÜSSEN sie zulassen. Ein Koordinator SOLL aber zuerst Hole Punching abwarten (bis 5 s) und Aufträge mit großem Kontext bevorzugt an direkt verbundene Anbieter geben.
- Ende-zu-Ende-Verschlüsselung (Noise/TLS) läuft durch das Relay hindurch. Ein fremdes Relay sieht nur, wer mit wem wie viel austauscht, nicht den Inhalt. Diese Metadaten sind im offenen Netz bewusst in Kauf genommen. Im privaten Netz sind alle Relays Mitglieder.
- Im offenen Netz wird das Relay-Anbieten ab Phase 4 über Quittungen vergütet. Bis dahin ist es freiwillig.

### 6.1 Helferknoten

Ein Helferknoten ist ein Knoten ohne Backends, der nur das Netz trägt: Einstieg, DHT-Server und Relay. Er braucht keine GPU, wenig CPU und etwa 100 MB RAM. `node.helper: true` setzt `dht_server: true` und `relay_service: true` und verlangt keine `backends`. Ein Knoten mit Modellen KANN dieselben Aufgaben zusätzlich übernehmen.

- **Brückenknoten** (nur privates Netz): ein Helfer, der nicht erreichbar sein muss, aber beide Seiten von sich aus erreicht, z. B. eine VM hinter dem NAT des Hypervisors mit VPN-Verbindung zu einem anderen Standort.
  - Er wählt die Knoten beider Seiten über `static_peers`/`bootstrap` an. mDNS reicht nicht durch NAT oder VPN.
  - Über diese Verbindungen reservieren die Mitglieder Relay-Plätze und erreichen die andere Seite.
  - Der Brückenknoten MUSS die selbst aufgebauten Verbindungen schützen (Connection Manager) und nach einem Abbruch neu aufbauen, weil die Gegenseite ihn nicht anwählen kann.
  - Hole Punching hilft hier nicht, weil die Seiten keinen gemeinsamen direkten Weg haben. Verkehr zwischen den Seiten läuft dauerhaft über das Relay.
  - Gehört der Tunnel zu einem fremden Netz, z. B. einem Firmennetz, leitet das Relay Verkehr in dieses Netz. Das muss mit dessen Betreiber abgestimmt sein.
- **Helfer im Internet:** auf einem Server mit öffentlicher Adresse. Er kann gleichzeitig das private Netz und, als eigener Host, das offene Netz tragen (Abschnitt 9). So kann er dem offenen Netz am Anfang als Starthilfe dienen, bis andere Helfer da sind.

- Jeder kann Helferknoten betreiben. Ins offene Netz kommt er über Peer-Austausch und DHT von selbst, nicht über eine Liste. Die mitgelieferte Bootstrap-Liste ist nur eine Auswahl verlässlicher Helfer.
- **Docker:** Es gibt ein Image mit dem Datenverzeichnis als Volume (`Dockerfile`, Vorlage und Compose-Datei unter `deploy/docker/`, optional mit Tracker). Empfohlen ist `network_mode: host`, weil AutoNAT, beobachtete Adressen und IPv6 nur so stimmen. Mit Bridge-Netz sind nur die Portweiterleitungen `4711/tcp` und `4711/udp` möglich, praktisch nur IPv4.
- **Hinter einem Heimrouter** (z. B. FritzBox): Portfreigabe `4711/tcp` (privates Netz) bzw. `4711/tcp` und `4711/udp` (offenes Netz) auf den Docker-Host, für IPv4 und, wenn vorhanden, für die IPv6-Adresse des Hosts. Die Bootstrap-Einträge SOLLEN einen DNS-Namen (`/dns/…`, z. B. MyFRITZ! oder DynDNS) statt einer IP enthalten.
- **Umzug:** Wird der Helfer auf einen anderen Server verlegt, bleibt seine Peer-ID gleich, wenn der Schlüssel mitkommt. Dann genügt es, den DNS-Namen umzustellen. Bestehende Knoten finden ihn ohnehin über Peer-Cache, Tracker und DHT wieder.
- Ein Helfer für ein privates und das offene Netz sind zwei Hosts mit zwei Ports (Abschnitt 9).
- **Helfer als Rolle:** Helfer ist jedes Mitglied, das für die anderen erreichbar ist (oder sie von sich aus erreicht) und `relay_service: true` hat, nicht ein bestimmtes Gerät. Die Abhängigkeit vom Helfer hängt von der Netzart ab:
  - **Privates Netz über mehrere Standorte:** Fällt das einzige solche Mitglied aus, laufen die Standorte für sich weiter (LAN, mDNS). Direkte Verbindungen zwischen den Standorten, die per Hole Punching entstanden sind, bleiben bestehen, bis sie abreißen; Relay-Verbindungen enden spätestens an ihrer Zeitgrenze. **Neue Verbindungen zwischen den Standorten entstehen nicht mehr**: Gespeicherte Adressen zeigen auf Geräte hinter NAT, und Hole Punching braucht zum Verabreden ein Relay. Ein Helfer ist entbehrlich, sobald ein anderes erreichbares Mitglied `relay_service: true` hat; zwei an verschiedenen Orten beseitigen den Single Point of Failure.
  - **Offenes Netz:** Helfer sind Starthilfe wie die Bootstrap-Knoten bei BitTorrent. Sind genug Knoten öffentlich erreichbar und bieten Relay an, tragen Peer-Cache, Peer-Austausch und DHT das Netz ohne sie; neue Knoten steigen über eine Einladung eines beliebigen Mitglieds oder einen Tracker ein. Anders als bei BitTorrent sind Relay und Portfreigabe hier Opt-in, und ein Koordinator muss einen *bestimmten* Anbieter erreichen statt irgendeinen Peer. Das Netz braucht deshalb Freiwillige mit `relay_service`; Portfreigabe per UPnP (`port_mapping`, Abschnitt 3) erhöht die Zahl erreichbarer Knoten.

## 7. Verbindungen halten

| | Privates Netz | Offenes Netz |
|---|---|---|
| Ziel | Vollvermaschung: zu jedem Mitglied aus Peer-Cache, mDNS und Sammelschlüssel eine Verbindung | Untergrenze 32, Obergrenze 96 Verbindungen (Connection Manager) |
| Geschützt | alle Mitglieder bis 64 | `trusted_peers`, Bootstrap-Knoten, Peers mit laufendem Auftrag |
| Neuversuch | alle 60 s, mit Backoff bis 15 min | nur bei Bedarf (Suche, vertraute Peers alle 5 min) |

Über 64 Mitgliedern verhält sich ein privates Netz wie ein offenes, bleibt aber geschlossen.

## 8. Missbrauchsschutz

Ergänzt das [Bedrohungsmodell](threat-model.md):

- **libp2p-Ressourcenmanager** mit festen Grenzen für Verbindungen, Streams und Speicher, pro Peer und gesamt. Zusätzlich gelten weiterhin höchstens 4 Auftrags-Streams pro Peer und 4 MiB pro Nachricht.
- **Ratenlimit für fremde Aufträge** (nur offenes Netz, nicht vertraute Peers): 10 `TASK_PROPOSE` pro Minute und Peer, 60 pro Minute gesamt. Danach folgt `TASK_REJECT` mit `reason: "busy"`, ohne Prüfung durch den Ressourcenmanager (REM).
- **Ratenlimit für Capability-Abrufe und Peer-Austausch** (ebenso): je 6 pro Minute und Peer, darüber wird der Stream zurückgesetzt.
- **Eclipse:** mehrere unabhängige Bootstrap-Betreiber; Vielfalt im Peer-Cache (Abschnitt 2.1); vertraute Peers immer gemerkt und geschützt; im offenen Netz Routing-Tabelle mit Diversitätsfilter von libp2p (höchstens 3 Peers je IP-Gruppe und Bucket, 20 je Gruppe in der Tabelle; Gruppe: IPv4-/16, IPv6 nach ASN). Peers hinter demselben Relay zählen zur Gruppe des Relays.
- **DHT-Vergiftung:** keine frei beschreibbaren Werte (Abschnitt 4); Provider-Einträge nur für sich selbst; Fähigkeiten immer signiert direkt beim Peer.
- **Tracker und Peer-Austausch:** Einträge sind signiert bzw. im privaten Netz verschlüsselt. Peer-Austausch liefert nur Hinweise mit Verbindungsbegrenzung. Ein Tracker oder Peer kann Kontakte verschweigen (Eclipse-Versuch), deshalb SOLLEN mehrere Quellen gleichzeitig genutzt werden.
- **Trennung der Netze:** Ein privates Netz ist im offenen Netz nicht sichtbar. Weder seine Kennung noch seine Mitglieder dürfen dort veröffentlicht werden (siehe auch Abschnitt 9).

## 9. Mehrere Netze gleichzeitig

Ein Knoten kann gleichzeitig Mitglied eines oder mehrerer privater Netze und des offenen Netzes sein, z. B. als Heimknoten im privaten Netz, der überschüssige Leistung zusätzlich öffentlich anbietet.

- Pro Netz läuft ein **eigener libp2p-Host** mit eigenem Port, eigener DHT, eigenem Peer-Cache und eigenem mDNS-Dienst. Das ist nötig, weil `pnet` für alle Verbindungen eines Hosts gilt.
- **Identität:** Jedes Netz SOLL einen eigenen Schlüssel erhalten (`<data_dir>/identity-<network_id>.key`). Sonst wäre im offenen Netz anhand der Peer-ID erkennbar, welcher Knoten auch Mitglied eines bestimmten privaten Netzes ist. Wer die Verknüpfung in Kauf nimmt, KANN mit `identity: shared` denselben Schlüssel verwenden.
- **Gemeinsam** sind Orchestrator, Backends, Ressourcenmanager und Quittungsspeicher. Fremdaufträge aus allen Netzen teilen sich dieselben Slots; Eigenbedarf verdrängt weiterhin alles.
- **Zuordnung beim Koordinieren:** Der Orchestrator sieht Agenten aus allen Netzen mit Kennzeichnung des Netzes. Ein Auftrag der Klasse `trusted` oder `reduced` geht nur an Peers eines privaten Netzes oder an `trusted_peers`. Ein Auftrag der Klasse `public` darf in jedes Netz. Ein Auftrag wird nie von einem Netz in ein anderes weitergereicht; Teilaufträge eines Anbieters bleiben im Netz, aus dem der Auftrag kam.
- Das Gateway (`127.0.0.1:7711`) bleibt einmalig und nur an localhost gebunden.
- `accept_public_tasks` prüft die P2P-Schicht je Netz; der Ressourcenmanager lässt öffentliche Aufträge zu, sobald ein Netz sie annimmt. Quittungen liegen in einem gemeinsamen Speicher, der Status fasst sie über alle eigenen Peer-IDs zusammen.
- `swarmainet join` ersetzt das erste Netz nur, wenn es das unveränderte offene Netz der Grundkonfiguration oder dasselbe Netz ist; sonst trägt es ein weiteres Netz mit dem nächsten freien Port ein.

## 10. Diagnose

`swarmainet status` zeigt je Netz:

- Netzart, Kennung, eigene Peer-ID, Anzahl Peers (direkt / per Hole Punching / per Relay)
- AutoNAT-Ergebnis, veröffentlichte Adressen, reservierte Relays
- DHT-Modus und Größe der Routing-Tabelle
- **Hinweise in Klartext**, z. B. „Privates Netz ohne öffentlich erreichbaren Knoten: nur LAN-Peers erreichbar“, „Alle Bootstrap-Knoten nicht erreichbar, nur Peer-Cache“, „IPv6 vorhanden, Port 4711 eingehend gesperrt?“

`swarmainet ping <peer-id>` sucht die Adressen über die DHT, versucht die Verbindung und meldet den Weg (direkt, Hole Punching, Relay) oder die Stelle, an der es scheitert.

## 11. Konfiguration (Entwurf)

Die bisherigen Schlüssel unter `node` beschreiben weiterhin das **erste Netz**, damit bestehende Konfigurationen gültig bleiben. Weitere Netze kommen unter `node.networks` hinzu:

```yaml
node:
  network_id: p-3fa91c07          # erstes Netz: privat, weil network_key_file gesetzt
  network_key_file: ~/.swarmainet/p-3fa91c07.key
  listen: [/ip4/0.0.0.0/tcp/4711, /ip6/::/tcp/4711]
  bootstrap:                      # neu: eigene Einstiegsknoten (z. B. VPS)
    - /dns4/swarm.example.org/tcp/4711/p2p/12D3Koo…
  announce: [/dns4/swarm.example.de/tcp/4711]   # neu: zusätzlich bekannt gegebene Adressen
  port_mapping: false             # neu: UPnP/NAT-PMP/PCP, Opt-in
  relay_service: auto             # neu: auto | true | false (Abschnitt 6)
  relay_limits: {}                # neu: Grenzen des Relays überschreiben
  dht_server: auto                # neu: auto | true | false
  trackers:                       # neu: HTTPS-Briefkästen (Abschnitt 2.3)
    - https://example.org/swarmainet/tracker
  pex: true                       # neu: Peer-Austausch (Abschnitt 2.4)
  helper: false                   # neu: nur Netz tragen, ohne Backends (Abschnitt 6.1)
  networks:                       # neu: weitere Netze
    - network_id: swarmainet      # offenes Netz
      listen: [/ip4/0.0.0.0/udp/4712/quic-v1, /ip4/0.0.0.0/tcp/4712]
      dns_seeds: [seed.ai-swarms.net]  # ohne Angabe der eingebaute Seed; [none] schaltet ab
      trackers: default
      identity: separate          # separate | shared
      provide: true
      accept_public_tasks: true
```

`provide`, `accept_public_tasks` und `trusted_peers` gelten je Netz. Ohne Angabe gelten die Werte aus `node` bzw. `resources`.

## 12. Offene Fragen

1. **Mitgliedschaft ohne gemeinsamen Schlüssel:** Ein PSK lässt sich nicht einzeln widerrufen und erzwingt TCP. Entwurf einer dritten Netzart mit Zertifikaten je Knoten: [Mitgliedsnetze](membership.md).
2. **Mitgelieferte Starthilfen** für das offene Netz: Am Anfang gibt es nur eigene Helfer und Tracker. Nach welchen Regeln werden Helfer anderer Betreiber in die mitgelieferte Liste aufgenommen (Laufzeit, Erreichbarkeit, Unabhängigkeit)? Das Netz selbst braucht die Liste nach dem ersten Kontakt nicht (Leitbild).
3. **Shard-Zahl `S`:** 4 ist geschätzt. Überprüfen, sobald es Messwerte zur Verteilung gibt.
4. **WebTransport/WebSocket über 443** für Firmennetze: in Phase 2 nur vorgesehen, Umsetzung bei Bedarf.

## 13. Gate Phase 2

*„Knoten an verschiedenen Standorten hinter NAT finden sich ohne feste Adressen; Ausfall eines Einstiegsknotens oder Trackers blockiert bestehende Knoten nicht; privates und offenes Netz bleiben getrennt.“*

Prüfung:

1. **Im Prozess** (Go-Tests, simuliertes Netz): Einstieg über Bootstrap und DHT, Fähigkeitssuche über Shards, Relay-Rückfall mit Grenzen, Peer-Cache nach Neustart, kein Kontakt zwischen PSK- und offenem Host, Ratenlimits.
2. **Auf echter Hardware:**
   - privates Netz über zwei Standorte, **Variante A:** Brückenknoten in einer Windows-VM (NAT des Hypervisors, VPN zum zweiten Standort), ohne geöffneten Port. Prüft zugleich Windows als Plattform.
   - privates Netz über zwei Standorte, **Variante B:** Helfer auf einem Server im Internet, sobald verfügbar; derselbe Server als erster Helfer des offenen Testnetzes. Optional ein Tracker (`swarmainet tracker`) auf demselben Server.
   - Abschalten von Helfer und Tracker: Bereits verbundene Knoten finden sich über Peer-Cache, Peer-Austausch und DHT weiter. Bei Variante A mit nur einem Brückenknoten arbeiten beide Standorte getrennt weiter.
   - ein offenes Testnetz aus denselben Geräten mit getrennten Identitäten
   - Messung: Anteil direkt, per Hole Punching und per Relay verbundener Peers, Zeit bis zur ersten Verbindung, Auftrag über Relay erfolgreich

## 14. Ergebnis auf Hardware (29.10.2026)

**Aufbau** (entspricht Variante B aus § 13 mit einem Helfer hinter einer Portfreigabe statt auf einem Server; Anleitung `deploy/raspberry-pi/`):

| Knoten | Anschluss | erreichbar von außen | Rolle |
|---|---|---|---|
| Raspberry Pi 4 | andere FritzBox, öffentliche IPv4, Portfreigabe 4711/tcp, Name per CNAME auf MyFRITZ! | ja | Helfer (Relay, DHT-Server), keine Modelle |
| VM | FritzBox zu Hause, keine Freigabe | nein | Anbieter (2 Modelle auf einem KI-Server im Heimnetz) |
| Notebook | Mobilfunk-Router mit O2-Karte (CGNAT) | nein | Koordinator ohne eigenes Modell |

Alle drei im privaten Schwarm (PSK), der Pi per `invite`/`join` aufgenommen, sein öffentlicher Name per `announce`.

**Ergebnisse:**

| Prüfung | Ergebnis |
|---|---|
| VM → Pi über das Internet | direkt, 21 ms; Relay-Platz reserviert |
| Notebook → Pi | direkt (ausgehend aus dem CGNAT) |
| VM ↔ Notebook | gefunden über Peer-Austausch, Verbindung über den Pi als Relay, 128 ms |
| Hole Punching VM ↔ Notebook | gelang nicht (nur TCP, Mobilfunk-CGNAT); Verbindung blieb über das Relay |
| Auftrag Notebook → VM | über das Relay ausgeführt (Muster `single`), beidseitig signierte Quittung |
| Ausfall des Pi | Verbindungen über ihn brechen ab, die Knoten laufen weiter |
| Pi wieder an | VM direkt verbunden und Relay-Platz neu binnen etwa einer Minute, Notebook 30 s später wieder über das Relay; ohne Neustart oder Eingriff |

Nicht gemessen wurde die Zeit bis zur ersten Verbindung beim Start. **Noch offen:** Brückenknoten über VPN (Variante A), Windows als Plattform, ein Tracker im Betrieb.

**Erfahrungen aus der Inbetriebnahme** (in die Anleitung eingeflossen): Der Raspberry Pi Imager aus den Ubuntu-Paketen (1.8) ist für das aktuelle Raspberry Pi OS zu alt, die Anpassungen gehen verloren; der Pi hat keine Uhr, frühe Zeitstempel nach dem Start sind falsch; nach einer Änderung der Konfiguration ist `systemctl restart` nötig, weil der Dienst nach dem Hochfahren schon läuft; der DNS-Anbieter verlangt beim CNAME-Ziel einen abschließenden Punkt.

## 15. Ergebnis offenes Netz auf Hardware (18.11.2026)

**Aufbau:** offenes Netz (`network_id: swarmainet`, ohne PSK), jeder Knoten als eigene Instanz mit eigener Identität neben den privaten Knoten derselben Geräte. Kein mDNS, keine Einladung, keine statischen Peers: Einstieg allein über den eingebauten DNS-Seed.

| Knoten | Anschluss | erreichbar von außen | Rolle |
|---|---|---|---|
| Startknoten | VPS, öffentliche IPv4 und IPv6, TCP und QUIC | ja | Relay (`auto`), DHT-Server, keine Modelle; im DNS-Seed eingetragen |
| VM | FritzBox zu Hause, keine Freigabe; IPv6 von außen abgewiesen | nein | Anbieter, `accept_public_tasks`, 2 Modelle auf einem KI-Server im Heimnetz, keine Werkzeuge freigegeben |
| Notebook | Mobilfunk-Router (CGNAT für IPv4, IPv6 mit Paketfilter) | nein | Koordinator ohne eigenes Modell |

**Ergebnisse:**

| Prüfung | Ergebnis |
|---|---|
| Einstieg über den DNS-Seed | VM nach 1 s verbunden, Relay-Platz sofort; Notebook ebenso |
| Fähigkeit finden | Notebook findet den Anbieter über die DHT |
| Auftrag `public` Notebook → VM | ausgeführt (Muster `single`), 2,7–10,6 s je nach Modell, Quittung gebucht |
| Auftrag `trusted` vom Notebook | verlässt das Notebook nicht („kein passender agent verfügbar“) |
| private Adressen | weder in Identify noch in der DHT veröffentlicht |
| Hole Punching VM ↔ Notebook | **gelungen** über IPv6/QUIC, direkte Verbindung, 46–48 ms. Gegenprobe: Ein frischer Knoten erreicht das Notebook unter derselben Adresse nicht, und die FritzBox weist eingehendes IPv6 zur VM ab (ICMPv6 „administratively prohibited“). Am 29.10. scheiterte es über TCP und IPv4. |
| Startknoten aus | bestehende direkte Verbindung hält, Aufträge laufen weiter |
| Notebook-Neustart bei ausgeschaltetem Startknoten | Verbindung trotzdem zustande gekommen, vermutlich durch gleichzeitiges Anwählen aus beiden Peer-Caches (IPv6 ohne NAT, feste Ports); nicht verlässlich |
| Startknoten wieder an | **Lücke:** VM und Notebook wählten ihn nicht wieder an, weil Einstiegsknoten nur ohne jeden Peer angewählt wurden. Beide blieben eine Insel ohne Relay-Platz und mit leerer DHT-Tabelle. Behoben mit der Regel in § 2 („Wann Einstiegsknoten angewählt werden“). |
| Fix auf Hardware | Insel aus drei Knoten ohne öffentliche Erreichbarkeit (VM, zweiter Knoten auf der VM, Notebook), Startknoten 40 min aus: Nach seiner Rückkehr hatten beide VM-Knoten nach 12 s wieder Relay-Platz und DHT-Partner, ohne Neustart oder Eingriff. |

**Werkzeugrechte** (Freigabeklassen, Regel 6): Auftraggeber war ein zweiter Knoten mit eigener Identität, für den der Anbieter ein Fremder ist; er fand ihn über Startknoten und DHT. Was das Modell beim Anbieter tatsächlich sah, zeichnete ein Proxy vor dem Inferenz-Server auf. Jede Anfrage enthielt `get_current_timestamp` und `run_command`.

| Fall | Ergebnis |
|---|---|
| ohne `allowed_tools` | abgelehnt (HTTP 400 „kein agent mit freigabe für diese werkzeuge“), beim Anbieter kommt nichts an |
| `allowed_tools: [get_current_timestamp]` | Modell sieht nur `get_current_timestamp` und ruft es auf |
| `run_command` per `tool_choice` erzwungen | vor dem Versand auf `auto` gesetzt; Modell sieht `run_command` nicht und ruft nichts auf |
| zweite Runde mit Ergebnis von `get_current_timestamp` | Antwort mit der Uhrzeit |
| Verlauf mit Aufruf und Ergebnis von `run_command` | abgelehnt, der Verlauf geht nicht an den Fremden |

Beobachtungen: Globale IPv6-Adressen werden veröffentlicht, bevor AutoNAT sie bestätigt; für das Hole Punching ist das nötig. `trusted`-Anfragen ohne vertraute Peers bekommen inzwischen eine Meldung mit Grund und HTTP 400 (nach dem Test umgesetzt). **Noch offen:** mehrere unabhängige Startknoten, Dauerbetrieb. Das Verwerfen von Aufrufen nicht gezeigter Werkzeuge war auf Hardware nicht zu provozieren, das Modell hielt sich an die gezeigte Liste; geprüft ist es im Prozess.
