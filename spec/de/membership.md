# Mitgliedsnetze (Zertifikate statt Netzschlüssel)

[English](../membership.md) · **Deutsch**

Status: In Umsetzung (§ 11). Zertifikate, Befehle, Zulassung, Rollen, Einladungen, Sperrlisten und Verlängerung laufen. Im Prozess getestet, auf echter Hardware über zwei Standorte und einen Helfer im Internet bestanden (§ 11), Dauerbetrieb steht aus. Beantwortet die offene Frage 1 aus der [P2P-Schicht](p2p-layer.md) § 12.

Ein privates Netz mit PSK passt für die eigenen Geräte, aber nicht für Gruppen und Firmen: Wer den Schlüssel hat, ist Mitglied. Einzelne lassen sich nicht entfernen, und ein gemeinsamer Helfer kennt den Schlüssel und wäre vollwertiges Mitglied. Ein **Mitgliedsnetz** ersetzt den gemeinsamen Schlüssel durch ein Zertifikat je Knoten.

**Ziele**

1. Mitglieder einzeln aufnehmen und entfernen, ohne die übrigen anzufassen.
2. Ein Helfer im Internet kann das Netz tragen, ohne Aufträge lesen oder annehmen zu dürfen. Wird er übernommen, ist nur er zu widerrufen.
3. Kein Dienst außerhalb der Gruppe wird gebraucht: keine Zertifizierungsstelle und kein zentraler Server.
4. QUIC und damit besseres Hole Punching wie im offenen Netz.

**Keine Ziele:** Ein Mitglied, das einen Auftrag verarbeitet, kann dessen Inhalt lesen und weitergeben; das verhindert auch ein Zertifikat nicht ([Freigabeklassen](privacy-classes.md)). Die Teilnahme an sich bleibt für Fremde nicht vollständig unsichtbar (§ 8).

PSK-Netze bleiben bestehen. Dies ist eine dritte Netzart, keine Änderung an `pnet`.

## 1. Vergleich der Netzarten

| | PSK-Netz | Mitgliedsnetz | Offenes Netz |
|---|---|---|---|
| Zweck | eigene Geräte | Gruppe, Firma | beliebige fremde Knoten |
| Zugang | gemeinsamer Schlüssel | Zertifikat je Knoten | frei |
| Mitglied entfernen | neuer Schlüssel für alle | einzeln widerrufen | entfällt |
| Vertrauen | jeder Teilnehmer | nur Rolle `member` und `issuer` (§ 3) | nur `trusted_peers` |
| Helfer kann Aufträge lesen | ja, er ist Mitglied | nein, Rolle `helper` | nein, nur Relay |
| Für Fremde sichtbar | nur Rauschen | ein libp2p-Knoten, bei bloßem Anwählen nicht welche Gruppe (§ 8) | alles Öffentliche |
| Transporte | TCP | QUIC, TCP | QUIC, TCP |
| DHT | eigene | keine (§ 6) | gemeinsam |
| Wer verwaltet | niemand | Aussteller | niemand |

Empfehlung: Eigene Geräte bleiben beim PSK, denn das braucht keine Verwaltung und zeigt Fremden am wenigsten. Sobald mehrere Personen beteiligt sind, ist das Mitgliedsnetz die passende Wahl.

## 2. Netzkennung und Wurzelschlüssel

`swarmainet network create` erzeugt einen Ed25519-**Wurzelschlüssel**. Die Kennung leitet sich daraus ab:

```
network_id = "m-" + erste 16 Hex-Zeichen von SHA-256("swarmainet/member-network/v1\n" + root_pubkey)
```

Die Kennung ist damit selbstbeglaubigend: Es braucht keine Vergabestelle, und niemand kann unbemerkt ein zweites Netz gleicher Kennung mit anderer Wurzel betreiben. Die Prüfung vergleicht immer den vollständigen Schlüssel, nicht nur die Kennung.

Der Wurzelschlüssel SOLL offline liegen. Er wird nur gebraucht, um Aussteller zu ernennen oder zu widerrufen. Geht er verloren, laufen die Aussteller bis zum Ablauf ihrer Zertifikate weiter, danach muss ein neues Netz aufgebaut werden. Wird er gestohlen, gilt das Netz als kompromittiert und muss neu aufgebaut werden.

## 3. Zertifikate

Die Kette hat höchstens zwei Stufen: Wurzel → Aussteller → Knoten. Kleine Gruppen KÖNNEN direkt mit der Wurzel ausstellen.

```json
{
  "v": 1,
  "network_id": "m-3fa9c1d27be04a68",
  "serial": "<16 zufällige Bytes, base64url>",
  "subject": "<peer-id des Knotens im Netz>",
  "role": "member",
  "name": "notebook-2",
  "issued_at": "2026-11-08T10:00:00Z",
  "expires_at": "2026-12-08T10:00:00Z",
  "issuer": "<peer-id des Ausstellers oder root>",
  "sig": "<ed25519, base64url>"
}
```

Signiert wird `"swarmainet/member-cert/v1\n" + JSON(Zertifikat ohne sig)`. Die Kennung im signierten Inhalt verhindert, dass ein Zertifikat in einem anderen Netz gilt.

**Signierte Bytes:** JSON in UTF-8 ohne Leerraum, Felder in der Reihenfolge des Beispiels, leere optionale Felder (`name`, `reason`, `serial` in Sperrlisten) entfallen, Zeiten als RFC 3339 in UTC. `<`, `>` und `&` werden nicht maskiert. Geprüft wird über die aus den Feldern neu gebildeten Bytes. Testvektoren für andere Implementierungen: `internal/membership/testdata/vectors.json`.

| Rolle | Aufträge annehmen und erteilen (`trusted`, `reduced`) | Andere aufnehmen | Relay, Peer-Austausch | Laufzeit (Vorgabe) |
|---|---|---|---|---|
| `issuer` | ja | ja, nur `member` und `helper` | ja | 365 Tage |
| `member` | ja | nein | ja | 30 Tage |
| `helper` | **nein** | nein | ja | 30 Tage |

**Regeln**

- `issuer` DARF nur von der Wurzel ausgestellt werden. Ein Zertifikat DARF nicht länger gelten als das seines Ausstellers. Aussteller-Zertifikate verlängert die Wurzel von Hand, weil sie offline liegt.
- `subject` MUSS der Peer-ID der Gegenstelle entsprechen. libp2p weist den Besitz des Schlüssels beim Verbindungsaufbau nach, ein zusätzlicher Nachweis entfällt.
- Der private Schlüssel eines Knotens verlässt das Gerät nie. Ausgestellt wird nur auf seine Peer-ID.
- Jedes Netz hat wie in [§ 9 der P2P-Schicht](p2p-layer.md) eine eigene Identität. `identity: shared` ist hier nicht erlaubt, sonst wäre die Mitgliedschaft mit anderen Netzen verknüpfbar.
- Uhrzeiten werden mit 5 Minuten Toleranz geprüft. Knoten ohne verlässliche Uhr (etwa ein Raspberry Pi ohne RTC kurz nach dem Start) MÜSSEN mit der Prüfung warten, bis die Zeit synchronisiert ist, und das in `swarmainet status` anzeigen.

**Verlängerung:** Ab der Hälfte der Laufzeit bittet ein Knoten erreichbare Aussteller über `/swarmainet/member-renew/0.1` um ein neues Zertifikat, nur Peers mit Rolle `issuer`. Der Aussteller stellt es aus, wenn das bisherige gültig und nicht widerrufen ist; Rolle und Name stammen aus dem geprüften Zertifikat, nicht aus einer Anfrage. Der Knoten prüft das Ergebnis gegen die Wurzel und übernimmt es nur mit gleicher Rolle und späterem Ablauf, danach nennt er es allen Zugelassenen. Zertifikate gelten höchstens 365 Tage. Aussteller-Zertifikate verlängert die Wurzel von Hand. Die kurze Laufzeit begrenzt den Schaden, falls eine Sperrliste einen Knoten nicht erreicht. Der Preis ist, dass mindestens ein Aussteller innerhalb von 15 Tagen online sein muss, und zwar gleichzeitig mit dem Knoten.

## 4. Aufnahme in das Netz

**Mit Einladung:**

```
swarmainet://join?v=2&net=<network_id>&root=<root_pubkey>&issuer=<peer-id>&peer=<multiaddr>[&peer=...]&token=<base64url>
```

1. `swarmainet member invite [-role member|helper] [-name N] [-ttl 24h]` erzeugt auf einem Aussteller ein zufälliges Token (32 Bytes) und merkt sich nur dessen Hash.
2. `swarmainet join <link>` erzeugt lokal die Identität für das Netz und verbindet sich mit den genannten Adressen.
3. Der neue Knoten sendet das Token **nur an die Peer-ID `issuer`**, die libp2p beim Verbindungsaufbau nachweist. Weitere Adressen im Link, etwa die eines Helfers, sind nur Einstieg; ein übernommener Helfer darf das Token nicht erfahren, sonst könnte er die Einladung selbst einlösen. Der Aussteller prüft, ob das Token gültig und unbenutzt ist, und stellt das Zertifikat auf die Peer-ID des neuen Knotens aus.

Ein Token gilt **einmal** und höchstens 24 Stunden. Im Gegensatz zum PSK-Link verrät es keinen Netzschlüssel und öffnet nur einen Platz. Wird ein Link abgefangen, sieht der Aussteller eine zusätzliche Aufnahme, und der eingeladene Knoten scheitert sichtbar mit „Einladung bereits benutzt“. Der Eindringling lässt sich dann einzeln widerrufen. Der Link SOLL trotzdem vertraulich weitergegeben werden.

Das Einlösen läuft über `/swarmainet/member-join/0.1`, vor der Zulassung. Das Protokoll besteht auf jedem Knoten eines Mitgliedsnetzes, damit die Protokollliste nicht verrät, wer ausstellt; ein Knoten ohne Ausstellerrechte weist jede Einladung ab. Eine Einladung gibt es nur für `member` und `helper`, Aussteller ernennt die Wurzel per Datei. Fragt dieselbe Identität erneut, bekommt sie ein neues Zertifikat, falls die erste Antwort verloren ging. Vom Token liegt nur der Hash im Buch des Ausstellers.

**Aussteller hinter NAT:** Ein Neuer erreicht ihn nur über einen Helfer, ist dort aber noch nicht zugelassen. Das Relay leitet einen nicht zugelassenen Knoten deshalb zu Peers mit Rolle `issuer` durch, zu anderen nicht. Ein Knoten, der Einladungen über einen Helfer annehmen soll, braucht also ein Aussteller-Zertifikat, auch wenn er den Wurzelschlüssel hat.

Wer `join` ausführt, MUSS den Schlüssel `root` aus dem Link gegen `net` prüfen und jedes vom Aussteller gelieferte Zertifikat gegen `root`. Ein gefälschter Link kann so nur in ein anderes Netz führen, aber kein Zertifikat für das echte vortäuschen. Der Link selbst muss deshalb aus einer vertrauenswürdigen Quelle stammen.

**Ohne laufenden Aussteller:** `swarmainet member request -net <id>` gibt die Peer-ID aus, `swarmainet member issue -net <id> <peer-id>` erzeugt auf dem Gerät mit dem Ausstellerschlüssel die Zertifikatsdatei, `swarmainet member install -root <root_pubkey> <datei>` prüft sie und übernimmt sie. Der Wurzelschlüssel wird dabei ausdrücklich angegeben, weil der in der Datei nichts beglaubigt.

## 5. Zulassung auf einer Verbindung

Fremde können eine Verbindung aufbauen, weil es keinen PSK davor gibt. Die Trennung liegt deshalb im Protokoll:

1. Nach dem Verbindungsaufbau tauschen beide über `/swarmainet/member/0.1` ihre Zertifikatskette und die Folgenummern ihrer Sperrlisten aus, zuerst die anwählende Seite (§ 8).
2. Geprüft werden: Kette bis zur Wurzel, Kennung, `subject` gleich Peer-ID, Laufzeit, Sperrliste, Rollenregeln.
3. Erst danach gilt der Peer als **zugelassen**, mit seiner Rolle.

| Protokoll | ohne Zulassung | `helper` | `member`, `issuer` |
|---|---|---|---|
| Identify, Ping | ja | ja | ja |
| Relay (Hop) beim Helfer | nur zu `issuer`, ohne Reservierung (§ 4) | – | ja |
| Einladung einlösen | ja | ja | ja |
| Peer-Austausch | nein | ja | ja |
| Fähigkeitsanzeige | nein | **nein** | ja |
| Aufträge (`task`) | nein | **nein** | ja |

Jeder Handler dieser Protokolle MUSS die Zulassung selbst prüfen, bevor er liest oder antwortet; ein vergessener Handler wäre ein Leck. Die Prüfung gehört deshalb an eine einzige Stelle, durch die alle Handler registriert werden. Nicht zugelassene Peers werden nach 10 Sekunden getrennt und mit Ratenlimits belegt (§ 8 der P2P-Schicht).

Beide Seiten prüfen. Ein Koordinator gibt Aufträge nur an zugelassene `member` und `issuer`, ein Anbieter nimmt sie nur von diesen an. Die Freigabeklassen gelten unverändert: `trusted` und `reduced` nur an diese Rollen, `public` wird in Mitgliedsnetzen wie im PSK-Netz behandelt, und `local` verlässt den Knoten nie.

**Werkzeuge:** Im Mitgliedsnetz sehen Modelle nur die Werkzeuge aus `allowed_tools`, ohne Liste keine ([Freigabeklassen](privacy-classes.md), Regel 6). Mitglieder können andere Personen sein, und die Modellantwort löst beim Auftraggeber Befehle aus.

**Reihenfolge:** Die annehmende Seite vermerkt die Zulassung, bevor sie antwortet, weil der Peer gleich danach beim Relay reserviert. Eigene Folgeanfragen sendet sie erst, wenn der Peer den Stream geschlossen hat und damit seinerseits fertig ist.

## 6. Finden und Halten der Verbindungen

Mitgliedsnetze laufen **ohne DHT**. Sie sind klein (bis 64 Knoten, Vollvermaschung wie im privaten Netz, § 7 der P2P-Schicht). Peer-Austausch und Peer-Cache reichen dafür, und ein eigenes DHT-Protokoll würde die Netzkennung über Identify jedem Fremden zeigen (§ 8).

- **Einstieg:** Adressen aus der Einladung, Peer-Cache, mDNS mit einem Dienstnamen aus dem Hash der Kennung (im selben LAN ist dadurch erkennbar, wer zur selben Gruppe gehört). Helfer tauschen Peers nur mit zugelassenen Knoten aus.
- **Relay:** Knoten reservieren einen Platz nur bei zugelassenen Helfern. Fremde Relays sind technisch möglich, aber nicht vorgesehen: Sie sehen, wer mit wem wie viel austauscht.
- **Direkte Wege:** Wie im PSK-Netz nennt ein Knoten beim Peer-Austausch auch seine eigenen Adressen, einschließlich der privaten, aber nur gegenüber zugelassenen Peers. Relay-Verbindungen werden durch direkte ersetzt (§ 6 der P2P-Schicht).
- **Tracker:** In der ersten Fassung nicht vorgesehen. Die Versiegelung leitet sich heute aus dem PSK ab, den es hier nicht gibt (§ 12).

## 7. Widerruf

Eine **Sperrliste** je Aussteller, signiert und mit steigender Folgenummer:

```json
{
  "v": 1, "network_id": "m-3fa9c1d27be04a68", "issuer": "<peer-id oder root>",
  "seq": 17, "issued_at": "2026-11-08T10:00:00Z",
  "revoked": [{ "serial": "...", "subject": "<peer-id>", "revoked_at": "...", "reason": "gerät verloren" }],
  "sig": "<ed25519, base64url>",
  "issuer_cert": { "...": "Zertifikat des Ausstellers, fehlt bei der Wurzel" }
}
```

Signiert wird `"swarmainet/member-crl/v1\n" + JSON(Liste ohne sig und issuer_cert)`. `issuer_cert` weist den Aussteller nach und wird für sich gegen die Wurzel geprüft. Ohne dieses Feld könnte niemand die Liste eines Ausstellers prüfen, den er nicht schon kennt.

- `swarmainet member revoke <peer-id|name> [-reason …]` erhöht `seq` und legt die Liste als Datei ab. Der laufende Knoten liest sie binnen Sekunden, übernimmt sie und sendet sie über `/swarmainet/member-crl/0.1` an alle zugelassenen Peers, die beide Seiten ihre Listen austauschen lassen. Eine Liste der Wurzel, deren Schlüssel offline liegt, spielt `member import-crl` ein.
- Damit eine verlorene Weitergabe nicht dauerhaft fehlt, gleicht jeder Knoten jede Minute mit einem zufälligen Zugelassenen ab.
- Gespeichert werden die Listen in `<data_dir>/crlstore-<network_id>.json`. Sonst ließe sich ein Widerruf nach einem Neustart mit einer älteren Liste zurückspielen. Eine Datei, deren Prüfung fehlschlägt, verhindert den Start dieses Netzes, statt still weniger zu sperren.
- Wer eine neuere Liste erhält, prüft sie, speichert sie, gibt sie weiter und nimmt betroffenen Peers **sofort** die Zulassung. Der Gesperrte bekommt die Liste noch zugestellt (höchstens 2 Sekunden), damit er den Grund erfährt, und wird dann getrennt; laufende Aufträge brechen dabei ab. Er zeigt das in `swarmainet status`, lässt niemanden mehr zu und startet in diesem Netz nicht mehr. Beim Zulassen werden die Folgenummern verglichen (§ 5), so holen auch zeitweise getrennte Knoten auf.
- Gesperrt wird die Peer-ID, nicht nur die Seriennummer, damit ein zweites Zertifikat desselben Knotens nicht weiter gilt. Einträge entfallen aus der Liste nach 365 Tagen, weil kein Zertifikat länger gilt; im Buch des Ausstellers bleiben sie, damit er nie neu ausstellt. Ein anderer Aussteller könnte einem so alten Eintrag wieder ausstellen, deshalb gilt: Ein gesperrter Knoten verwendet eine neue Identität, und der Aussteller prüft, wen er aufnimmt.
- Aussteller sperrt **nur die Wurzel**. Der Eintrag eines Ausstellers gilt für `member` und `helper`, nicht für `issuer`, sonst könnte ein übernommener Aussteller die anderen und damit die Verwaltung lahmlegen.
- Die Wurzel kann Aussteller widerrufen. Damit werden **alle** von diesem ausgestellten Zertifikate ungültig. Eine Ausnahme für ältere Zertifikate gibt es nicht, denn ein übernommener Aussteller könnte `issued_at` beliebig zurückdatieren. Die Betroffenen brauchen eine neue Einladung von einem anderen Aussteller, deshalb SOLLEN Firmen zwei Aussteller ernennen. Die Sperrlisten eines widerrufenen Ausstellers gelten nicht mehr, damit auch Sperren aus der Zeit der Übernahme enden. Der Preis: Berechtigte Sperren dieses Ausstellers für Knoten eines anderen Ausstellers sind neu auszusprechen.
- Eine Sperrliste mit niedrigerer Folgenummer als die gespeicherte wird verworfen, gegen Rückspielen.

**Grenze:** Ein Knoten, den keine Sperrliste erreicht, vertraut dem Widerrufenen bis zum Ablauf des Zertifikats, also höchstens 30 Tage. Sind alle online, erreicht die Liste im vollvermaschten Netz alle in Sekunden.

## 8. Was Fremde sehen

Ohne PSK antwortet der Knoten jedem auf einen Verbindungsversuch. Das ist der wichtigste Nachteil gegenüber dem PSK-Netz.

- **Sichtbar für jeden, der verbindet:** Peer-ID, Tatsache eines libp2p-Knotens, Protokollliste aus Identify. Die Protokoll-IDs enthalten deshalb **keine Netzkennung**, `/swarmainet/member/0.1` ist für alle Mitgliedsnetze gleich. Die Liste verrät, dass der Knoten an irgendeinem Mitgliedsnetz teilnimmt, aber nicht an welchem.
- **Reihenfolge beim Austausch:** Die anwählende Seite sendet zuerst, und ein Knoten antwortet nur auf eine gültige Kette seines eigenen Netzes. Wer einen Knoten bloß anwählt, ruft so weder Kennung noch Name oder Rolle ab.
- **Grenze:** Wählt ein Mitglied selbst einen fremden Knoten an, sieht dieser dessen Zertifikat. Ein Knoten wählt deshalb nur Adressen aus Einladung, Peer-Cache, mDNS und dem Peer-Austausch zugelassener Peers an. Ein Eintrag von dort kann trotzdem auf einen Fremden zeigen oder veraltet sein, etwa nach einem Adresswechsel. Vollständig verhindert das diese Preisgabe nicht.
- **Helfer** sehen, wer online ist, von wo und wie viel übertragen wird, und die Namen in den Zertifikaten. `name` ist deshalb optional und SOLL keine Personennamen enthalten. Inhalte sehen sie nicht, die Ende-zu-Ende-Verschlüsselung läuft durch das Relay hindurch.
- **Angriffsfläche:** Fehler in den Handlern vor der Zulassung sind von außen erreichbar. Vor der Zulassung ist deshalb nur der Austausch aus § 5 offen, mit begrenzter Größe (64 KiB) und Ratenlimits.

## 9. Bedrohungen

| Angriff | Gegenmaßnahme | verbleibendes Risiko |
|---|---|---|
| Helfer übernommen | Rolle `helper` nimmt keine Aufträge an und bekommt keine; einzeln widerrufbar, andere unberührt | sieht Metadaten bis zum Widerruf |
| Gerät eines Mitglieds gestohlen | Widerruf, kurze Laufzeit; Schlüssel nur auf dem Gerät | Zugriff bis zum Widerruf; empfohlen: Festplattenverschlüsselung |
| Ausscheidendes Mitglied behält Kopie | Widerruf sperrt die Peer-ID | bereits gesehene Inhalte bleiben bekannt |
| Einladungslink abgefangen | Token einmalig, 24 h, Aufnahme sichtbar im Protokoll | Eindringling bis zum Widerruf; `member list` zeigt ihn |
| Aussteller übernommen | Wurzel widerruft ihn, nur zweistufige Kette, kann keine Aussteller ernennen | kann bis dahin Knoten aufnehmen und sperren, auch Mitglieder |
| Wurzelschlüssel gestohlen | Offline-Lagerung | Netz muss neu aufgebaut werden |
| Sperrliste unterdrückt (Trennung vom Netz) | Abgleich bei jeder Zulassung, kurze Laufzeit | bis zu 30 Tage in einem abgeschotteten Teil |
| Zertifikat in anderem Netz benutzt | Kennung ist signiert, Kette endet an der eigenen Wurzel | – |
| Verbindungen von Fremden als Last | Ratenlimits vor der Zulassung, Trennung nach 10 s | Port bleibt von außen erkennbar |
| Mitglied sammelt Aufträge oder liefert Falsches | keine technische Abwehr gegen Mitlesen; Prüfer anderer Familie wie bisher | wie bei `trusted` |

## 10. Konfiguration (Entwurf)

```yaml
node:
  networks:
    - network_id: m-3fa9c1d27be04a68
      membership:
        root: "<root_pubkey, base64url>"
        cert_file: ~/.swarmainet/member-m-3fa9c1d27be04a68.cert
        # issuer_key_file: nur auf Ausstellern
      listen: [/ip4/0.0.0.0/udp/4714/quic-v1, /ip4/0.0.0.0/tcp/4714]
      allowed_tools: [get_current_timestamp]   # fehlt die Liste: keine
```

`membership` und `network_key_file` schließen einander aus. Ohne gültiges Zertifikat startet das Netz nicht und nennt den Grund in `swarmainet status`; andere Netze desselben Knotens laufen weiter.

## 11. Umsetzung und Gate

1. ✅ `internal/membership`: Zertifikate, Ketten, Sperrlisten, sprachunabhängige [Testvektoren](../../internal/membership/testdata/de/README.md) (CC BY 4.0).
2. ✅ Befehle: `network create`, `member invite|issue|install|request|revoke|list`.
3. ✅ Knoten: Zulassung an einer Stelle (§ 5), Rollen statt `n.private`, Einladung, Werkzeuglisten. Tests im Prozess, einschließlich gezielt abgeschalteter Prüfungen.
4. ✅ Sperrlisten verteilen und speichern, Verlängerung. Tests im Prozess, auch mit böswilligem Aussteller und Gesperrtem.
5. ✅ VPS-Vorlage: Container `demo` ist Helfer eines Mitgliedsnetzes ohne Netzschlüssel.

**Gate:** *„Ein Mitglied lässt sich entfernen, ohne die anderen anzufassen; ein übernommener Helfer kann weder Aufträge lesen noch annehmen; ein Fremder, der einen Knoten anwählt, erfährt nicht, zu welchem Netz er gehört.“*

**Prüfung im Prozess:** Fremder ohne Zertifikat und mit Zertifikat eines anderen Netzes bekommt keine Anzeige, keinen Peer-Austausch und keinen Auftrag; `helper` bekommt und nimmt keine Aufträge; widerrufener Knoten wird getrennt, Aufträge brechen ab; abgelaufenes Zertifikat wird abgewiesen; Token wirkt nur einmal; ältere Sperrliste wird verworfen; zeitweise getrennter Knoten holt die Liste beim Wiederverbinden nach.

**Generalprobe am 08.11.2026** (VPS als `helper`, zwei Instanzen auf der VM, ohne eigene Ports, also nur über den Helfer): Einladung über das Relay in 0,1 s, Auftrag über das Relay erfolgreich, benutzter Link abgewiesen und beim Aussteller protokolliert, Helfer als Koordinator ohne Agent, Werkzeuge ohne Liste abgewiesen (400), Widerruf bei Aussteller und Gesperrtem nach 1,8 s wirksam (davon bis zu 3 s Abfrage der Datei), Helfer übernimmt die Liste, Gesperrter startet nicht mehr im Netz. Das private Netz lief unberührt weiter. Das ist noch kein zweiter Standort.

**Zwei Standorte am 08.11.2026:** Aussteller auf der VM im Heimnetz hinter NAT, VPS als `helper`, ein Notebook am Mobilfunk-Router an einem anderen Standort (öffentliche Adresse eines anderen Anschlusses). Beitritt per Einladung und Probeauftrag vom Notebook zum Modell auf der VM über das Relay erfolgreich, beidseitige Quittung beim Aussteller. Widerruf: 0,41 s bis zum Verlust der Zulassung beim Aussteller (Befehl bis Datei gelesen), 0,46 s bis zur Trennung, Helfer übernimmt die Liste nach 0,41 s. Dabei gefunden: Der Gesperrte wählte seine früheren Peers weiter neu an; behoben, aber nicht erneut auf Hardware geprüft. Nicht geprüft: direkter Weg zwischen den Standorten (kein Hole Punching erwartet hinter Mobilfunk), Verlängerung im laufenden Betrieb, Ablauf über Tage.

**Prüfung auf Hardware:** VPS als `helper`, Knoten an zwei Standorten, Auftrag über Relay und direkt über QUIC; Widerruf eines Knotens, gemessen bis zur Trennung; Helfer-Container mit Absicht als Koordinator missbraucht, muss abgewiesen werden.

## 12. Offene Fragen

1. **Laufzeit:** 30 Tage bei Mitgliedern sind geschätzt. Kürzer begrenzt den Schaden, verlangt aber öfter einen erreichbaren Aussteller.
2. **Mehr als 64 Mitglieder:** Vollvermaschung trägt nicht mehr. Dann bräuchte es eine DHT, ohne die Kennung über Identify zu zeigen.
3. **Tracker:** Ein Versiegelungsschlüssel, den alle Mitglieder kennen, wäre wieder ein gemeinsames Geheimnis, wenn auch ein schwaches: Er schützt nur Adressen. Alternative wäre eine Versiegelung je Empfänger.
4. **Feinere Rollen:** Etwa Mitglieder, die nur `reduced` sehen, oder Gruppen innerhalb einer Firma. Bewusst nicht in der ersten Fassung.
5. **Wechsel vom PSK:** Ein Werkzeug, das ein bestehendes PSK-Netz in ein Mitgliedsnetz überführt, oder einfach Neuaufbau mit Einladungen. Bei bis zu 64 Knoten reicht vermutlich der Neuaufbau.
