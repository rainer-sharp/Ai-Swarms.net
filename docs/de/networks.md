# Netzarten

[English](../networks.md) · **Deutsch**

AI-Swarms.net kennt drei Arten von Netzen. Ein Knoten kann mehreren gleichzeitig angehören.

| | Privates Netz | Mitgliedsnetz | Offenes Netz |
|---|---|---|---|
| Für | eigene Geräte | Gruppen, Vereine, Firmen | alle |
| Zugang | gemeinsamer Schlüssel (PSK) | Zertifikat je Knoten | frei |
| Einzelne entfernen | nein, nur neuer Schlüssel für alle | ja, per Sperrliste in Sekunden | – |
| Vertrauen | alle Mitglieder | zugelassene Mitglieder, nicht Helfer | niemand außer `trusted_peers` |
| Werkzeuge ohne Liste | alle | keine | keine |
| Über Standorte hinweg | mit eigenem Helfer | mit eigenem Helfer, der nichts mitliest | über fremde Relays und DHT |
| Erprobt auf Hardware | ja | ja, zwei Standorte | ja, zwei Standorte |
| Netz-Kennung | frei, z. B. `p-3fa91c07` | `m-…` aus dem Wurzelschlüssel | `swarmainet` |

## Privates Netz: eigene Geräte

Alle Geräte mit demselben Schlüssel bilden ein abgeschlossenes Netz und gelten als vertraut. Wer den Schlüssel nicht hat, kommt nicht einmal durch den Verbindungsaufbau. `swarmainet init` richtet ein solches Netz standardmäßig ein, mit zufälliger Kennung `p-…` und frischem Schlüssel; weitere Geräte kommen per [Einladung](#einladungen) dazu.

Von Hand, etwa für eine bestehende Konfiguration:

```sh
swarmainet psk ~/.swarmainet/swarm.key
```

Die Datei auf alle Geräte kopieren und unter `node.network_key_file` eintragen; eine eigene Netz-Kennung (`node.network_id`, z. B. `p-3fa91c07`) trennt das Netz von anderen. Einfacher geht es per [Einladung](#einladungen). Private Netze laufen nur über TCP, weil libp2p den Schlüssel für QUIC nicht unterstützt.

Alternativ lassen sich einzelne Geräte ohne gemeinsames Netz vertrauen: ihre Peer-IDs unter `node.trusted_peers` eintragen.

### Einladungen

```sh
swarmainet invite -with-key [-peer <adresse>] [-net <netz>] [-qr]   # auf einem laufenden Knoten
swarmainet join '<link>'                                      # auf dem neuen Gerät
```

`invite` nimmt die Adressen des laufenden Knotens (ohne Loopback; im offenen Netz nur öffentliche, mit `-lan` auch lokale), mit `-peer` weitere, etwa die eines Helfers, dazu die konfigurierten Tracker. `-with-key` bettet den Netzschlüssel ein: **Wer den Link hat, ist Mitglied.** Den Link deshalb nur vertraulich weitergeben. Ohne Schlüssel im Link gibt man ihn bei `join -key DATEI` an. Mit `-qr` erscheint der Link zusätzlich als QR-Code im Terminal, etwa um ihn mit dem Telefon abzulesen; mit Schlüssel gilt für den Code dasselbe wie für den Link: nicht fotografieren, nach dem Übertragen vom Telefon löschen.

`join` legt die Konfiguration bei Bedarf an, speichert den Schlüssel als `~/.swarmainet/<network_id>.key` (Rechte 0600), trägt Adressen unter `node.bootstrap` und Tracker unter `node.trackers` ein und erhält die Kommentare in der Datei. Gehört der Knoten schon zu einem Netz, kommt das neue unter `node.networks` mit dem nächsten freien Port dazu. Das von `init` angelegte private Netz wird stattdessen ersetzt, solange der Knoten noch nie lief und keine Einstiegsadressen hat.

### Mehrere Standorte

Ein privates Netz kann wegen des Schlüssels keine fremden Relays nutzen. Liegen die Geräte an verschiedenen Standorten hinter NAT, braucht es **einen eigenen Helferknoten**, den alle Seiten erreichen:

- **Helfer im Internet:** ein Helferknoten auf einem Server mit öffentlicher Adresse, z. B. per [Docker](#helferknoten-mit-docker). Alle Standorte verbinden sich ausgehend zu ihm. Anleitung für einen VPS mit Firewall und DNS: [`deploy/vps/`](../../deploy/vps/de/README.md).
- **Helfer hinter einer Portfreigabe:** etwa ein Raspberry Pi hinter einer FritzBox und einem zweiten Router, vom Heimnetz abgeschottet, erreichbar über einen DNS-Namen. Schritt-für-Schritt-Anleitung: [`deploy/raspberry-pi/`](../../deploy/raspberry-pi/de/README.md).
- **Brückenknoten:** ein Knoten an einer Stelle, die beide Seiten **von sich aus** erreichen, z. B. eine VM mit VPN zum anderen Standort. Er muss selbst nicht erreichbar sein, kein Port wird geöffnet. Dort `node.helper: true` (nur Relay und DHT-Server, ohne Modelle) oder `node.relay_service: true` (zusätzlich zu eigenen Modellen) setzen und unter `node.static_peers` je mindestens einen Knoten beider Seiten eintragen.

Die übrigen Knoten reservieren beim Helfer automatisch einen Relay-Platz, finden die andere Seite über den Peer-Austausch und erreichen sie über ihn. Sobald ein direkter Weg möglich ist, ersetzt der Knoten die Relay-Verbindung durch eine direkte. Abgelaufene Relay-Verbindungen (Grenze 10 min bzw. 8 MiB) baut jeder Knoten von selbst neu auf. Ein einzelner Helfer ist ein Single Point of Failure zwischen den Standorten; ein zweiter behebt das.

## Mitgliedsnetz: Gruppen und Firmen

Ein Mitgliedsnetz ersetzt den gemeinsamen Schlüssel durch ein **Zertifikat je Knoten**. Einzelne lassen sich aufnehmen und entfernen, ohne die übrigen anzufassen, und ein Helfer im Internet trägt das Netz, ohne Aufträge zu sehen oder anzunehmen. Es braucht keine Zertifizierungsstelle und keinen zentralen Server. Spezifikation: [`spec/de/membership.md`](../../spec/de/membership.md).

```sh
swarmainet network create                      # Wurzelschlüssel, danach offline lagern
swarmainet member request -net m-…             # auf dem künftigen Aussteller
swarmainet member issue -net m-… -role issuer <peer-id>
swarmainet member install -root <schlüssel> DATEI

swarmainet run                                 # der Aussteller muss für Einladungen laufen
swarmainet member invite -net m-… [-qr]        # Link: einmalig, höchstens 24 Stunden
swarmainet join '<link>'                       # auf dem neuen Knoten
```

Unter Ubuntu nimmt [`deploy/linux/gruppe-beitreten.sh`](../../deploy/linux/de/README.md#knoten-für-ein-gruppennetz-einrichten) einem neuen Mitglied alle Schritte nach dem `git clone` ab, einschließlich der Installation von Go.

**Rollen:** `issuer` nimmt auf, `member` gibt und bekommt Aufträge, `helper` trägt nur das Netz. Einen Helfer lädt man mit `member invite -role helper` ein. Ein Einladungslink gilt nur einmal und nennt den Aussteller; das Einmal-Token geht nur an ihn, nicht an den Helfer.

**Entfernen:** `swarmainet member revoke -net m-… <peer-id|name>` beim Aussteller. Der laufende Knoten verteilt die Sperrliste binnen Sekunden, alle trennen den Gesperrten, und laufende Aufträge von ihm brechen ab. Wer gerade nicht erreichbar ist, erfährt es bei der nächsten Verbindung. Die Wurzel sperrt vom Wechseldatenträger aus mit `member revoke -root-key DATEI …`, danach auf einem laufenden Knoten `member import-crl`.

**Laufzeit:** Mitglieder und Helfer gelten 30 Tage und verlängern sich ab der Hälfte selbst, wenn ein Aussteller erreichbar ist. Ein Aussteller muss also mindestens alle 15 Tage online sein, und zwar gleichzeitig mit dem Mitglied. Aussteller-Zertifikate (365 Tage) erneuert die Wurzel von Hand; `swarmainet status` warnt 7 Tage vorher.

**Grenzen:** Mitgliedsnetze laufen ohne DHT, Tracker und DNS-Seeds und sind für bis zu etwa 64 Knoten gedacht. Sie brauchen eine richtig gehende Uhr, weil Zertifikate ablaufen.

## Offenes Netz

Ohne Netzschlüssel und ohne Mitgliedschaft ist ein Knoten im offenen Netz (`network_id: swarmainet`); so richtet ihn `swarmainet init -open` ein, ebenso der Beitritt per Einladung ins offene Netz. Dort ist kein fremder Peer vertraut:

- Aufträge mit `X-SwarmAiNet-Privacy: public` dürfen an fremde Knoten gehen, `trusted` und `reduced` nur an `trusted_peers`.
- Fremde Aufträge nimmt ein Knoten nur mit `resources.accept_public_tasks: true` an.
- Fremde Modelle sehen nur Werkzeuge aus `allowed_tools`, ohne Liste keine.
- Fehlen für einen `public`-Auftrag passende Anbieter, sucht der Koordinator sie über die DHT.

**Schutz im offenen Netz:**

- **AutoNAT** prüft die eigene Erreichbarkeit (`reachability` im Status, je Adressfamilie unter `reachability_by_family`). Nicht erreichbare Knoten reservieren Relay-Plätze, erreichbare werden automatisch DHT-Server.
- **Keine internen Adressen** (LAN, Loopback) in Identify, DHT, Peer-Austausch und Tracker. Für ein Testnetz im LAN hebt `node.announce_private: true` das auf.
- **Ratenlimits** für fremde Peers: 10 Aufträge je Minute und Peer, 60 insgesamt; je 6 Fähigkeits- und Peer-Austausch-Abrufe je Minute und Peer.
- **Diversitätsfilter** der DHT-Routing-Tabelle gegen Eclipse-Angriffe.
- **Relay automatisch:** Öffentlich erreichbare Knoten leiten Verbindungen anderer weiter (`node.relay_service: auto`, mit sparsamen Grenzen, abschaltbar mit `false`). So trägt sich das Netz wie bei BitTorrent selbst, auch wenn der Startknoten wegfällt.
- **Einstieg über DNS-Seeds:** Ein neuer Knoten ohne Peers fragt den eingebauten Seed `seed.ai-swarms.net` (TXT-Einträge unter `_dnsaddr.seed.ai-swarms.net`) nach Einstiegsknoten; danach tragen Peer-Cache, Peer-Austausch und DHT. Er fragt erneut, wenn seine DHT-Routing-Tabelle leer wird oder er hinter NAT keinen Relay-Platz hat, etwa nach einem Ausfall des Startknotens. Eigene Seeds mit `node.dns_seeds`, abschalten mit `[none]`.
- **Portfreigabe per UPnP/NAT-PMP** (`node.port_mapping: true`, Opt-in) macht einen Knoten hinter einem Heimrouter erreichbar, ohne den Router von Hand einzustellen. Die Freigaben laufen nach dem Ende des Knotens binnen einer Minute aus. Bei der FritzBox muss für das Gerät „Selbstständige Portfreigaben erlauben“ gesetzt sein; damit kann allerdings jedes Programm auf diesem Gerät Ports öffnen. Bei DS-Lite und CGNAT hilft es nicht.

## Mehrere Netze gleichzeitig

Weitere Netze stehen unter `node.networks`, je mit eigenem Port, eigener DHT und standardmäßig eigener Peer-ID (`identity: separate`), damit die Teilnahme an einem privaten Netz im offenen nicht erkennbar ist. Backends, Ressourcenmanager und Quittungen sind gemeinsam; `provide`, `accept_public_tasks`, `trackers`, `allowed_tools`, `dns_seeds` u. a. gelten je Netz und übernehmen ohne Angabe die Werte des ersten. Aufträge der Klassen `trusted` und `reduced` gehen nur an Peers privater Netze, Mitgliedsnetze oder an `trusted_peers`; `public` darf in jedes Netz. Beispiel in [`config.example.yaml`](../../config.example.yaml); `ping` und `invite` wählen das Netz mit `-net`.

## Wie Knoten sich finden

Ein Knoten findet andere über Peer-Cache, mDNS, Einladung, `bootstrap`-Adressen und Tracker; danach tragen Peer-Austausch (`node.pex`) und eine Kademlia-DHT je Netz (nicht in Mitgliedsnetzen). Verbunden wird direkt, per Hole Punching oder über ein Relay. Kein Knoten braucht dafür einen offenen Port, solange es irgendwo einen erreichbaren Helfer gibt. Spezifikation: [`spec/de/p2p-layer.md`](../../spec/de/p2p-layer.md).

### IPv6 und DS-Lite

Knoten lauschen standardmäßig auch auf IPv6 (`/ip6/::/tcp/4711`, im offenen Netz zusätzlich QUIC). Öffentliche IPv6-Adressen werden bekannt gegeben, ULA- und Link-Local-Adressen nur im privaten Netz.

**Hinter DS-Lite oder CGNAT** teilt sich der Anschluss eine IPv4-Adresse mit anderen; per IPv4 ist ein Knoten dann von außen nie erreichbar, auch nicht mit Portfreigabe oder UPnP. Er funktioniert trotzdem: Ausgehend erreicht er Helfer, Relays und DHT über IPv4 und IPv6, und über einen Relay-Platz ist er für alle erreichbar. Zwischen zwei IPv6-fähigen Standorten gelingt Hole Punching meist, weil IPv6 keine Ports umschreibt; auf Hardware mit QUIC bestätigt. Ohne Relay erreichbar wird ein Knoten hinter DS-Lite nur über IPv6, mit einer IPv6-Freigabe für Port 4711 in der Firewall des Routers. `swarmainet status` zeigt das unter `reachability_by_family` (z. B. `ip4: private`, `ip6: public`), und `hints` erklärt es.

**Datenschutz:** Bildet das Betriebssystem die IPv6-Adresse aus der MAC-Adresse (EUI-64, erkennbar an `ff:fe` in der Mitte), ist sie im offenen Netz eine dauerhafte Gerätekennung. Unter NetworkManager hilft `ipv6.addr-gen-mode stable-privacy`.

### Tracker

`swarmainet tracker` betreibt einen Adressbriefkasten für Knoten ohne feste Adressen. Standard ist `127.0.0.1:7720` hinter einem TLS-Reverse-Proxy (`-trust-proxy` übernimmt dann die Absender-IP); mit `-tls-cert`/`-tls-key` lauscht er selbst per HTTPS. Einträge liegen nur im Speicher und laufen nach einer Stunde ab. Knoten mit `node.trackers: [https://…]` hinterlegen dort alle 20 Minuten ihre Adressen und holen die anderer ab, solange sie wenige Peers haben. Im privaten Netz ist alles verschlüsselt: Der Tracker sieht weder Netz-Kennung noch Mitglieder oder Adressen. Knoten akzeptieren Tracker nur per HTTPS (`http` nur für localhost).

## Helferknoten mit Docker

Ein Helferknoten trägt das Netz (Einstieg, DHT-Server, Relay), braucht keine GPU und eignet sich für einen Server mit öffentlicher Adresse. Vorlage: [`deploy/docker/`](../../deploy/docker/compose.yaml).

```sh
cd deploy/docker
mkdir data && cp helper.yaml data/config.yaml     # Netz-Kennung eintragen
cp ~/.swarmainet/swarm.key data/swarm.key         # Schlüssel des privaten Netzes
sudo chown -R 65532:65532 data                    # Container läuft als nonroot
docker compose up -d --build
docker compose exec helper swarmainet status -config /data/config.yaml
docker compose exec helper swarmainet invite -config /data/config.yaml -with-key
```

Der Container nutzt das Host-Netz (`network_mode: host`), damit Erreichbarkeit und IPv6 stimmen; eingehend muss nur 4711/tcp offen sein (offenes Netz zusätzlich 4712/tcp+udp, siehe Vorlage). Optional läuft ein Tracker mit: `docker compose --profile tracker up -d`. Statt eines Verzeichnisses geht auch ein benanntes Volume. Das Image allein: `docker build -t swarmainet .`

Für den Helfer eines Mitgliedsnetzes siehe [`deploy/vps/`](../../deploy/vps/de/README.md), Abschnitt zum Gruppennetz.
