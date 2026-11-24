# SwarmAiNet Protokoll v0.1

[English](../protocol-v0.1.md) · **Deutsch**

Status: Entwurf, Phase 0/1. Referenzimplementierung: `internal/protocol`, `internal/p2p`.
Die Schlüsselwörter MUSS, SOLL und KANN sind normativ.

## 1. Identität

- Jeder Knoten hat ein lokal erzeugtes Ed25519-Schlüsselpaar. Die Peer-ID ist die libp2p-Peer-ID dieses Schlüssels und bleibt bei IP-Wechsel gleich.
- Es gibt keine Accounts und keine zentrale Registrierung.

## 2. Transport

| Protokoll-ID | Zweck |
|---|---|
| `/swarmainet/capability/0.1` | Abruf der signierten Fähigkeitsanzeige eines Peers |
| `/swarmainet/task/0.1` | Ein Teilauftrag pro Stream: Angebot, Annahme, Ergebnis, Quittung |
| `/swarmainet/pex/0.1` | Peer-Austausch (Phase 2, siehe [P2P-Schicht](p2p-layer.md) § 2.4) |

- Rahmung: genau eine JSON-Nachricht (Envelope) pro Zeile, abgeschlossen mit `\n`, höchstens 4 MiB.
- mDNS-Dienstname: `swarmainet-<network_id>`. Private Schwärme nutzen zusätzlich einen libp2p-PSK (dann nur TCP).
- Inferenz-Backend und Verwaltungsport lauschen nur auf `localhost`; nach außen geht nur der P2P-Port.
- Ab Phase 2 (Entwurf, siehe [P2P-Schicht](p2p-layer.md)): Kademlia-DHT `/swarmainet/kad/1.0.0` (offenes Netz) bzw. `/swarmainet/<network_id>/kad/1.0.0` (privates Netz), AutoNAT v2, DCUtR und Circuit Relay v2. Die Protokolle oben MÜSSEN auch über Relay-Verbindungen (begrenzte Verbindungen) angeboten werden.

## 3. Envelope

```json
{
  "protocol": "/swarmainet/0.1",
  "type": "TASK_PROPOSE",
  "task_id": "<id>",
  "sender": "<peer-id>",
  "expires_at": "2026-10-14T12:05:00Z",
  "nonce": "<128 bit hex>",
  "payload": { },
  "signature": "<base64>"
}
```

**Signatur:** Ed25519 über

```
"swarmainet/sig/v0.1\n" + protocol + "\n" + type + "\n" + task_id + "\n" + sender + "\n"
+ expires_at (RFC 3339, UTC, Nanosekunden ohne Nullen) + "\n" + nonce + "\n" + hex(sha256(payload-bytes)) + "\n"
```

`payload-bytes` sind die Bytes der Payload exakt so, wie sie auf der Leitung stehen (kompaktes JSON).

Empfänger MÜSSEN verwerfen, wenn
1. `protocol` nicht passt,
2. `expires_at` erreicht ist,
3. die Signatur nicht zum Schlüssel in `sender` passt,
4. `sender` nicht der Gegenstelle der Verbindung entspricht,
5. das Paar (`sender`, `nonce`) bereits vor Ablauf gesehen wurde (Replay).

**Erweiterbarkeit:** Unbekannte Felder in der Payload MÜSSEN ignoriert werden. Neue Kopffelder gibt es nur mit neuer Protokollversion; Erweiterungen gehören in die Payload, damit die Signatur stabil bleibt.

## 4. Nachrichten

| Typ | Richtung | Payload |
|---|---|---|
| `CAPABILITY_ANNOUNCE` | Anbieter → beliebig | `Capability` |
| `TASK_PROPOSE` | Koordinator → Anbieter | `ProposePayload` |
| `TASK_BID` | Anbieter → Koordinator | `BidPayload` (ab Phase 3) |
| `TASK_ACCEPT` | Anbieter → Koordinator | `subtask_id`, `lease_expiry` |
| `TASK_REJECT` | Anbieter → Koordinator | `subtask_id`, `reason` (allgemein formuliert) |
| `TASK_PROGRESS` | Anbieter → Koordinator | Status oder Token-Stream (`ProgressPayload`, § 4.2) |
| `TASK_RESULT` | Anbieter → Koordinator | `Result` |
| `VERIFY_REQUEST` | Koordinator → Prüfer | ab Phase 3, bis dahin `TASK_PROPOSE` mit `role: verifier` |
| `TASK_CANCEL` | beide | `subtask_id`, `reason` |
| `TASK_RECEIPT` | beide | `Receipt` (erst vom Koordinator, dann gegengezeichnet zurück) |
| `TRUST_ATTESTATION` | beliebig | ab Phase 3 |

`TASK_REJECT` ist gegenüber dem Gesamtkonzept ergänzt: Eine Ablehnung ist ein eigener Endzustand und braucht eine eigene Nachricht.

### 4.1 Capability

```json
{
  "peer_id": "<peer-id>",
  "expires_at": "2026-10-14T12:05:00Z",
  "agents": [{ "agent_id": "ollama/qwen2.5-coder:7b", "model_family": "qwen",
               "skills": ["code.python", "code.review"], "modalities": ["text"], "context_max": 32768,
               "strength": 5, "tools": true }],
  "availability": { "slots_now": 1, "windows": [] },
  "policy": { "accepts": ["trusted"], "retention": "no-prompt-storage" }
}
```

`strength` (optional, 1–10, fehlt = unbekannt) ist eine Selbsteinschätzung des Anbieters. Koordinatoren nutzen sie nur zur Reihung gleich geeigneter Agenten, etwa bei der Wahl des Prüfers; sie ist nicht überprüft (siehe Bedrohungsmodell).

`tools` (optional, fehlt = false) zeigt an, dass das Modell Werkzeug-Aufrufe im OpenAI-Format beherrscht (Konfiguration `models[].tools: true`).

Die Anzeige enthält keine Prompts, internen IPs, Hardware- oder Energiedaten und nicht den Grund der Verfügbarkeit. `peer_id` MUSS dem Envelope-Absender entsprechen. TTL: 5 Minuten, Abruf beim Verbindungsaufbau und jede Minute. Reißt die letzte Verbindung zu einem Peer ab, bleibt seine Anzeige noch 30 Sekunden gültig (danach wird sie verworfen, auch vor Ablauf der TTL), und vertraute Peers werden neu angewählt: von der Seite mit der kleineren Peer-ID sofort, von der anderen erst nach 2 bis 3 Sekunden und nur, wenn bis dahin keine Verbindung besteht. Wählten beide zugleich, entstünde über TCP mit Port-Wiederverwendung ein Simultaneous Open, bei dem der Handshake auf beiden Seiten scheitert. Ein Teilauftrag, der auf die Wählsperre von libp2p trifft (dial backoff), hebt sie für diesen Peer auf und wählt einmal neu.

### 4.2 ProposePayload

```json
{
  "subtask_id": "<id>", "role": "specialist|verifier|synthesizer|planner|router|tool",
  "skill": "code.python", "agent_id": "<agent>",
  "messages": [{ "role": "user", "content": "..." }],
  "limits": { "deadline": "...", "max_depth": 1, "depth": 1, "max_subtasks": 8,
              "max_tokens": 2048, "max_bytes": 0, "privacy_class": "trusted" },
  "json_schema": "<optional>",
  "tools": [ ... ], "tool_choice": "auto"
}
```

`messages[]` folgt dem OpenAI-Format einschließlich `tool_calls` (Assistent), `tool_call_id` und `name` (Rolle `tool`). `tools` und `tool_choice` (optional) sind die Werkzeugbeschreibungen der aufrufenden Anwendung und werden unverändert an das Modell gegeben. Der Anbieter führt keine Werkzeuge aus; er liefert nur die Aufrufe zurück, ausgeführt werden sie beim Auftraggeber. Welche Werkzeuge ein Agent sehen darf, entscheidet deshalb der Auftraggeber ([Freigabeklassen](privacy-classes.md), Regel 6). Ein Anbieter MUSS einen Auftrag mit `tools` ablehnen, wenn der gewählte Agent `tools` nicht anzeigt (`agent beherrscht keine werkzeuge`).

`limits.max_tokens` ist die Obergrenze für die Antwort. Ein Anbieter DARF sie senken, damit Eingabe und Antwort in den Kontext seines Modells passen. Verbraucht das Modell die Grenze ohne Antwort (typisch bei Reasoning-Modellen, deren Nachdenken mitzählt), SOLLTE der Anbieter mit dem Grund `token-limit erreicht` abbrechen; sonst meldet er einen Fehlschlag nur allgemein (`ausführung fehlgeschlagen`), ohne Inhalte.

`audio` (optional) trägt eine Aufnahme für eine Transkription: `{ "data": <base64>, "mime", "filename", "language", "prompt" }`, höchstens 2,75 MiB Audio (`MaxAudioBytes`), damit die Nachricht unter 4 MiB bleibt. Ein solcher Teilauftrag hat die Fähigkeit `audio.transcribe`, das Ergebnis ist die Abschrift in `result.content`. Stimmen sind personenbezogene Daten: Ein Koordinator DARF Audio nicht mit der Freigabeklasse `public` senden, und ein Anbieter MUSS es ablehnen (`audio nur im vertrauten kreis`), ebenso Audio an einen Agenten ohne `audio.transcribe` (`agent verarbeitet kein audio`) und leere oder zu große Aufnahmen (`aufnahme leer oder zu groß`). Der Anbieter gibt die Aufnahme an `/audio/transcriptions` seines Backends oder, wenn das Backend diese Schnittstelle nicht hat, an den Chat als `audio_url`. Längere Aufnahmen teilt der Koordinator in überlappende Stücke, je ein eigener Teilauftrag, und fügt die Abschriften selbst zusammen.

`images` (optional, in einem Element von `messages[]`) trägt eingebettete Bilder: `[{ "mime": "image/png", "data": <base64> }, …]`, Formate `image/png`, `image/jpeg`, `image/webp` und `image/gif`, zusammen höchstens 2,75 MiB je Teilauftrag (`MaxImageBytes`). Agenten, die Bilder verstehen, melden die Modalität `image`. Ein Koordinator schickt Bilder nur an solche Agenten und nie als `reduced` an Fremde, denn ein Bild lässt sich nicht pseudonymisieren ([Freigabeklassen](privacy-classes.md), Regel 10). Ein Anbieter MUSS Bilder ablehnen an einen Agenten ohne `image` (`agent verarbeitet keine bilder`), bei `reduced` von Fremden (`bilder bei reduced nur im vertrauten kreis`) und in unbekannten Formaten oder über der Grenze (`bilder zu groß oder in unbekanntem format`). Er gibt sie im OpenAI-Format als `image_url` mit data-URL an den Chat seines Backends. Anbieter mit älteren Versionen kennen das Feld nicht; sie melden nie `image` und bekommen deshalb nie Bilder.

`stream` (optional, fehlt = false) fordert Zwischenstände an. Der Anbieter schickt dann, während das Modell arbeitet, `TASK_PROGRESS` mit `{ "subtask_id", "status": "delta", "delta", "reasoning" }`: `delta` ist neuer Text der Antwort, `reasoning` neuer Text der Denkphase des Modells, beides seit dem vorigen Zwischenstand. Er SOLL sie bündeln (die Referenzimplementierung schickt höchstens alle 100 ms oder 16 KiB eine Nachricht). Die aneinandergereihten `delta`-Texte sind eine Vorschau; maßgeblich sind `TASK_RESULT`, seine Prüfsumme und die Quittung. Anbieter, die `stream` nicht kennen, schicken keine Zwischenstände, und der Koordinator wartet wie bisher auf das Ergebnis. Aufträge mit `audio` werden nicht gestreamt. Mit `tools` wird nur Text gestreamt: Werkzeug-Aufrufe MUSS der Anbieter aus `TASK_PROGRESS` heraushalten; sie kommen nur im Ergebnis, damit der Auftraggeber sie vor der Weitergabe prüfen kann (Regel 6 der [Freigabeklassen](privacy-classes.md)).

`depth` zählt die bisherigen Delegationen (beim ersten Empfänger 1). Ein Anbieter MUSS ablehnen, wenn `depth > max_depth`. Weiterdelegationen erben gleiche oder strengere Grenzen und verbrauchen dasselbe Budget.

### 4.3 Result

Wie im Gesamtkonzept: `task_id`, `subtask_id`, `producer_peer`, `role`, `result {status, summary, content}`, `evidence[]`, `confidence {value, kind}`, `limitations[]`, `usage`. Bei Aufträgen mit Werkzeugen enthält `result` zusätzlich `tool_calls[]` (`id`, `type: "function"`, `function {name, arguments}`); `content` darf dann leer sein. `confidence.kind = "self-reported"` ist keine kalibrierte Wahrscheinlichkeit; `evidence.digest` belegt Unverändertheit, nicht Richtigkeit.

### 4.4 Receipt

Felder: `task_id`, `subtask_id`, `coordinator`, `provider`, `role`, `agent_id`, `model_family`, `status`, `finished_at`, `duration_ms`, `prompt_tokens`, `completion_tokens`, `result_digest`, `coordinator_sig`, `provider_sig`.
Signiert wird `"swarmainet/receipt/v0.1\n" + JSON(Receipt ohne Signaturen)`. Der Anbieter zeichnet nur gegen, wenn Digest und Token-Zahlen seiner Leistung entsprechen. Quittungen sind idempotent über (`task_id`, `subtask_id`, `provider`).

## 5. Ablauf eines Teilauftrags

```
Koordinator                         Anbieter
  TASK_PROPOSE  ───────────────────▶  prüft Freigabeklasse, Vertrauen, Deadline, Tiefe, REM
               ◀───────────────────  TASK_ACCEPT (Lease) | TASK_REJECT
               ◀───────────────────  TASK_PROGRESS*
               ◀───────────────────  TASK_RESULT | TASK_CANCEL
  TASK_RECEIPT (signiert) ─────────▶ prüft, zeichnet gegen
               ◀───────────────────  TASK_RECEIPT (beidseitig)
```

Bricht der Koordinator ab, sendet er `TASK_CANCEL` und setzt den Stream zurück; der Anbieter bricht die Inferenz ab. Endet der Stream vor dem Ergebnis, gilt der Teilauftrag als abgebrochen.

## 6. Zustände

`CREATED → DISCOVERING → OFFERED → ACCEPTED → RUNNING → REVIEWING → COMPLETED`;
`OFFERED → REJECTED`; aus jedem nicht terminalen Zustand `CANCELLED`, `EXPIRED`, `FAILED`.
Ein Übergang in denselben Zustand ist erlaubt (idempotente Wiederholung).

## 7. Annahmeregeln des Anbieters (Phase 1)

| Freigabeklasse | vertrauter Peer | sonstiger Peer |
|---|---|---|
| `local` | ablehnen | ablehnen |
| `trusted` | annehmen* | ablehnen |
| `reduced` | annehmen* | nur mit `accept_public_tasks: true`*, vom Koordinator minimiert und pseudonymisiert ([Freigabeklassen](privacy-classes.md), Regel 9) |
| `public` | annehmen* | nur mit `accept_public_tasks: true`* |

\* sofern der Ressourcenmanager zustimmt. Lehnt er wegen fehlender Kapazität ab (`keine kapazität`) oder greift das Ratenlimit (`busy`), versucht der Koordinator es mit anderen Kandidaten und wartet, wenn alle ausgelastet sind, mit wachsendem Abstand (1 s bis 8 s, höchstens 2 Minuten bzw. bis zur Deadline) erneut. Vertraut ist ein Peer, dessen Schlüssel unter `trusted_peers` steht, jeder Teilnehmer eines privaten Schwarms (PSK) oder ein zugelassener Peer mit Rolle `member` oder `issuer` in einem [Mitgliedsnetz](membership.md).
