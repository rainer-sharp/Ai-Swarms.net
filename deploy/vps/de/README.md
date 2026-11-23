# Startknoten und Demo-Netz auf einem VPS

[English](../README.md) · **Deutsch**

Ein kleiner VPS mit fester IPv4 und IPv6 (z. B. netcup VPS nano: 2 vCPU, 2 GB RAM) trägt mehrere getrennte Helfer, je in einem eigenen Container:

| Container | Netz | Port | Gateway | Name | Aufgabe |
|---|---|---|---|---|---|
| `seed` | offenes Netz (`swarmainet`, ohne Schlüssel) | 4712/tcp + 4712/udp (QUIC) | 7712 | `seed.ai-swarms.net` | Startknoten: Einstieg über den DNS-Seed, DHT-Server, Relay. Er wird mit dem Wachstum des Netzes entbehrlich ([spec/de/p2p-layer.md](../../../spec/de/p2p-layer.md) § 6.1). |
| `private` | der eigene private Schwarm (Kennung und Schlüssel per Einladung) | 4711/tcp | 7711 | `helper.ai-swarms.net` | Helfer, über den sich die Standorte des eigenen Schwarms verbinden |
| `demo` (optional) | Gruppennetz (Mitgliedsnetz `m-…`, kein Netzschlüssel) | 4713/tcp + 4713/udp (QUIC) | 7713 | `demo.ai-swarms.net` | Helfer einer Gruppe: trägt das Netz, sieht keine Aufträge |

Getrennte Container statt eines Knotens mit mehreren Netzen, damit jeder Helfer unabhängig eingerichtet, neu aufgesetzt oder entfernt werden kann und jeder Netzschlüssel nur in seinem eigenen Datenverzeichnis liegt. Alle nutzen das Host-Netz und brauchen deshalb verschiedene Ports und Gateway-Ports.

> Der VPS kennt den Schlüssel des eigenen privaten Schwarms und ist dort vertrautes Mitglied. Für den eigenen Schwarm auf dem eigenen Server ist das vertretbar, solange er gut abgesichert ist. In einem **Gruppennetz** hält er dagegen nur ein Zertifikat mit Rolle `helper`: Er bekommt keine Aufträge und sieht nur, wer online ist und wie viel übertragen wird ([Mitgliedsnetze](../../../spec/de/membership.md)).

## 1. VPS bestellen und einrichten

1. Beim Bestellen als Betriebssystem **Debian 13** wählen und einen SSH-Schlüssel hinterlegen.
2. Anmelden und aktualisieren:
   ```sh
   ssh root@<IPv4 des VPS>
   apt update && apt full-upgrade -y
   apt install -y unattended-upgrades git ufw
   ```
3. Einen eigenen Benutzer anlegen, SSH nur mit Schlüssel und ohne root:
   ```sh
   adduser admin && usermod -aG sudo admin
   mkdir -p /home/admin/.ssh && cp ~/.ssh/authorized_keys /home/admin/.ssh/
   chown -R admin:admin /home/admin/.ssh && chmod 700 /home/admin/.ssh
   printf 'PasswordAuthentication no\nPermitRootLogin no\n' > /etc/ssh/sshd_config.d/10-haerten.conf
   systemctl reload ssh
   ```
   Vor dem Abmelden in einem zweiten Terminal prüfen, dass `ssh admin@<IPv4>` funktioniert.
4. **Firewall:** nur SSH und die beiden Netzports öffnen.
   ```sh
   ufw allow 22/tcp
   ufw allow 4711/tcp      # private
   ufw allow 4712/tcp      # seed
   ufw allow 4712/udp
   ufw allow 4713/tcp      # demo, nur wenn genutzt
   ufw allow 4713/udp
   ufw enable
   ```
   Bietet der Anbieter zusätzlich eine eigene Firewall an (bei netcup im Server Control Panel), dort dieselben Ports freigeben oder sie ausgeschaltet lassen.
5. **Docker:**
   ```sh
   sudo apt install -y docker.io docker-compose docker-buildx
   sudo usermod -aG docker admin          # danach neu anmelden
   ```

## 2. Vorlage holen

```sh
git clone https://github.com/rainer-sharp/Ai-Swarms.net.git
cd Ai-Swarms.net/deploy/vps
mkdir data-open data-private data-demo
cp open.yaml data-open/config.yaml
cp private.yaml data-private/config.yaml
cp demo.yaml data-demo/config.yaml
```

Die Container laufen als Benutzer `nonroot` (UID 65532); er muss in die Datenverzeichnisse schreiben dürfen:

```sh
sudo chown -R 65532:65532 data-open data-private data-demo
```

Statt `git clone` geht auch: Image auf einem anderen Rechner bauen und übertragen (`docker save swarmainet:latest | gzip | ssh vps 'gunzip | docker load'`), dann nur die Dateien aus `deploy/vps/` kopieren und `docker compose up -d --no-build …` verwenden. Der VPS braucht so keinen Zugriff auf das Repository.

## 3. Namen eintragen

In den Konfigurationen stehen die Namen `seed.ai-swarms.net`, `helper.ai-swarms.net` und `demo.ai-swarms.net` unter `announce`. Wer andere Namen nimmt, ändert sie dort (`sudoedit data-open/config.yaml`).

## 4. Startknoten starten

```sh
SWARMAINET_VERSION=$(git describe --tags --always) docker compose build seed
docker compose up -d seed
docker compose exec seed swarmainet status -config /data/config.yaml | grep -m1 peer_id
```

## 5. Helfer des eigenen privaten Schwarms

Auf einem Knoten des Schwarms eine Einladung erzeugen (`swarmainet invite -with-key`) und auf dem VPS damit beitreten. Der Link enthält den Netzschlüssel: nicht in Dateien liegen lassen, die gesichert oder geteilt werden.

```sh
docker compose run --rm --no-deps private join -config /data/config.yaml '<link>'
docker compose up -d private
docker compose exec private swarmainet status -config /data/config.yaml | grep -m1 peer_id
```

Danach auf allen Knoten des Schwarms den Helfer eintragen:

```yaml
node:
  static_peers:
    - /dns/helper.ai-swarms.net/tcp/4711/p2p/<Peer-ID von private>
```

## 5a. Helfer eines Gruppennetzes (optional)

Das Netz entsteht auf einem Rechner der Gruppe, nicht auf dem VPS. Der Wurzelschlüssel gehört nie hierher.

```sh
# Rechner der Gruppe
swarmainet network create                # nennt Kennung m-… und Wurzelschlüssel

# VPS: Kennung und Wurzelschlüssel in data-demo/config.yaml eintragen, dann
docker compose --profile demo run --rm --no-deps demo member request -dir /data -net m-…

# Rechner der Gruppe: auf die genannte Peer-ID ausstellen, Datei auf den VPS kopieren
swarmainet member issue -net m-… -role helper -name vps-helfer -out helper.cert <peer-id>

# VPS: Datei für den Container bereitstellen, übernehmen, starten
sudo install -o 65532 -g 65532 -m 600 helper.cert data-demo/helper.cert
docker compose --profile demo run --rm --no-deps demo member install -dir /data -root <wurzelschlüssel> /data/helper.cert
docker compose --profile demo up -d --no-build --no-deps demo
```

Danach auf dem Aussteller `node.static_peers` auf den Helfer setzen. Die Einladung für alle weiteren Knoten nennt ihn dann von selbst: `swarmainet member invite -net m-…`.

## 6. DNS-Einträge

Beim Domain-Anbieter, mit der festen IPv4 und IPv6 des VPS:

```
seed.ai-swarms.net.            300  IN  A     <IPv4 des VPS>
seed.ai-swarms.net.            300  IN  AAAA  <IPv6 des VPS>
helper.ai-swarms.net.          300  IN  A     <IPv4 des VPS>
helper.ai-swarms.net.          300  IN  AAAA  <IPv6 des VPS>
demo.ai-swarms.net.            300  IN  A     <IPv4 des VPS>   (nur mit Demo)
demo.ai-swarms.net.            300  IN  AAAA  <IPv6 des VPS>   (nur mit Demo)
_dnsaddr.seed.ai-swarms.net.   300  IN  TXT   "dnsaddr=/dns/seed.ai-swarms.net/tcp/4712/p2p/<Peer-ID von seed>"
_dnsaddr.seed.ai-swarms.net.   300  IN  TXT   "dnsaddr=/dns/seed.ai-swarms.net/udp/4712/quic-v1/p2p/<Peer-ID von seed>"
```

Die beiden TXT-Einträge sind der **DNS-Seed**: Jeder neue Knoten im offenen Netz fragt `_dnsaddr.seed.ai-swarms.net` und findet so den Startknoten. Kommen später weitere öffentliche Helfer hinzu, bekommen sie hier eigene TXT-Einträge; fällt der VPS weg, werden seine entfernt. Die Software muss dafür nicht neu ausgeliefert werden.

> **Global Village (domreg):** Der Quick-Modus legt die **ganze Zone neu** an. Bestehende Einträge, etwa `helper` (CNAME auf MyFRITZ!) und die Hauptdomain, müssen dort mit angegeben werden, sonst sind sie weg. Besser im Expert-Modus nur die Zeilen oben hinzufügen. Ziele von CNAMEs brauchen den Punkt am Ende.

Prüfen:

```sh
dig +short A seed.ai-swarms.net
dig +short TXT _dnsaddr.seed.ai-swarms.net
```

## 7. Prüfen

- **Ports von außen** (von einem anderen Rechner): `nc -vz seed.ai-swarms.net 4712`, `nc -vz helper.ai-swarms.net 4711` und gegebenenfalls `nc -vz demo.ai-swarms.net 4713`.
- **Startknoten:** Ein Knoten im offenen Netz mit Standardkonfiguration (ohne `dns_seeds`) findet ihn von selbst. Direkt: `swarmainet ping /dns/seed.ai-swarms.net/tcp/4712/p2p/<Peer-ID von seed>`.
- **Status auf dem VPS:** `docker compose exec seed swarmainet status -config /data/config.yaml`, dort `relay_service: true`, `dht.mode: server`, nach einiger Zeit `reachability: public`.

## 8. Mitglieder aufnehmen und entfernen

Das geschieht auf dem Aussteller der Gruppe, nicht auf dem VPS: `swarmainet member invite` und `swarmainet member revoke` ([Netzarten](../../../docs/de/networks.md#mitgliedsnetz-gruppen-und-firmen)). Der Helfer bekommt die Sperrliste von selbst und trennt Gesperrte.

## 9. Betrieb

- **Protokolle:** `docker compose logs -f seed` bzw. `demo`
- **Aktualisieren:** `git pull && docker compose up -d --build`
- **Neustart nach Änderung einer Konfiguration:** `docker compose restart seed` bzw. `private` oder `demo`
- **Gruppennetz-Helfer beenden:** `docker compose rm -sf demo`, `sudo rm -rf data-demo`, DNS-Eintrag `demo` entfernen, auf dem Aussteller `member revoke`. Der Startknoten läuft unberührt weiter.
- **Sicherung:** Die Peer-IDs hängen an `data-open/identity.key`, `data-private/identity.key` und `data-demo/identity.key`. Mit ihnen kann ein neuer VPS dieselben Peer-IDs übernehmen, sodass der DNS-Seed gültig bleibt.

## 10. Sicherheit

- Von außen erreichbar sind nur SSH (mit Schlüssel) und die Netzports. Die Gateways lauschen nur auf `127.0.0.1`.
- Beide Helfer rechnen nicht und nehmen keine Aufträge an.
- Im offenen Netz sieht der Startknoten als Relay, wer mit wem wie viel austauscht, aber keine Inhalte (Ende-zu-Ende verschlüsselt).
- Der VPS kennt den Schlüssel des eigenen Schwarms und ist dort vertrautes Mitglied. Wird er übernommen, ist dieses Netz kompromittiert: Schlüssel neu erzeugen und neue Einladungen verteilen. Im Gruppennetz genügt es dagegen, seinen Helfer zu widerrufen.
