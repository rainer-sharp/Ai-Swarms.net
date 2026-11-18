# Selbstorganisierte Kooperation (Phase 3)

[English](../cooperation.md) · **Deutsch**

Status: **Entwurf**, noch nicht umgesetzt. Grundlage: Gesamtkonzept, Abschnitte „Fähigkeiten und Kompetenzmessung“, „Rollen und Kooperationsmuster“, „Auftragsprotokoll“ und „Auswahl und Regelung“. Ergänzt [Protokoll v0.1](protocol-v0.1.md) und baut auf der [P2P-Schicht](p2p-layer.md) auf. Die Schlüsselwörter MUSS, SOLL und KANN sind normativ.

Bisher entscheidet der Orchestrator nach festen Regeln: Er nimmt das Muster aus der Konfiguration oder der Anfrage und den Prüfer nach Familie und `strength` (eine Selbstauskunft). Phase 3 ersetzt das durch **gemessene Erfahrung**:

- Ein **Kompetenzprofil** hält fest, wie gut jeder Agent in jeder Fähigkeit tatsächlich ist (§ 2).
- Ein **Bandit** wählt je Aufgabenkategorie das Rezept, also Muster und Besetzung der Rollen (§ 3).
- **PI-Regler** bestimmen, wie viel Aufwand eingesetzt wird, etwa wie viele Prüfungen (§ 4).
- Neue **Muster** (Kaskade, Zerlegen und Verteilen über einen Task-Graph) erweitern die Auswahl (§ 5, § 6).
- Ein **Lease** macht die Koordination eines Auftrags ausfallsicher (§ 7).

**Gate Phase 3** ist die Kernhypothese aus dem [Messplan](measurement-plan.md): Der Schwarm liefert bei gleichem Budget eine höhere geprüfte Qualität als das beste Einzelmodell, oder er erkennt zuverlässig, dass ein Einzelmodell genügt. Wird das verfehlt, wird das Konzept vor Phase 4 überarbeitet (§ 10).

Grundsätze, die für alles Folgende gelten:

- **Lokales Urteil:** Jeder Knoten führt sein eigenes Profil aus eigenen Erfahrungen. Es gibt keinen globalen Kompetenzwert.
- **Evidenz vor Meinung:** Objektiv prüfbare Signale (Tests, exakte Lösungen) wiegen mehr als Urteile anderer Modelle, und diese mehr als Selbstauskünfte.
- **Harte Grenzen bleiben hart:** Freigabeklasse, Ressourcen-Policy anderer Knoten, Budgets und Delegationstiefe kann kein Bandit und kein Regler überstimmen.
- **Ersteller und Prüfer aus verschiedenen Modellfamilien**, wie bisher.

## 1. Begriffe

| Begriff | Bedeutung |
|---|---|
| Agent | ein angebotenes Modell auf einem Knoten, Schlüssel `(peer_id, agent_id)` |
| Fähigkeit (skill) | Eintrag der Taxonomie, z. B. `code.python`, `reasoning.math`, `general` |
| Aufgabenkategorie | grobe Klasse für Bandit und Regler: `code`, `math`, `facts`, `vision`, `planning`, `general`; aus der Fähigkeit abgeleitet |
| Rezept | Muster plus Besetzung der Rollen, beschrieben über Familien bzw. Kompetenzstufen, nicht über einzelne Agenten (§ 3.1) |
| Signal | Messung der Qualität eines Ergebnisses: Test, exakte Lösung, Prüferurteil, Nutzerfeedback |
| Aufwand | Zahl der beteiligten Modelle und Prüfrunden eines Auftrags |

## 2. Kompetenzprofil

### 2.1 Modell

Jeder Knoten führt für jeden Agenten, mit dem er arbeitet, je Fähigkeit eine Bewertung nach **Glicko-2**: Wert `r`, Unsicherheit `RD` und Volatilität `σ`. Neue Agenten beginnen mit hoher Unsicherheit; ihr Startwert kann aus `strength` abgeleitet werden (`r₀ = 1500 + 50·(strength−5)`), damit Selbstauskünfte nur den Anfang prägen und durch Messungen schnell überschrieben werden.

Glicko-2 statt einfachem Elo, weil es die Unsicherheit mitführt. Der Bandit (§ 3) braucht genau diese, um zwischen „schlecht“ und „unbekannt“ zu unterscheiden.

Ablage: `<data_dir>/profile.json`, nur lokal, je Netz getrennt (Agenten verschiedener Netze sind verschiedene Identitäten). Werte von Agenten, die 90 Tage nicht beobachtet wurden, verlieren an Sicherheit (RD wächst wie in Glicko-2 vorgesehen).

### 2.2 Signale und Gewichte

| Rang | Signal | Wirkung | Herkunft |
|---|---|---|---|
| 1 | **objektiv**: Unit-Tests bestanden, exakte Lösung getroffen | Partie gegen eine feste Referenz (Sieg/Niederlage) mit vollem Gewicht | Benchmark (`swarmainet bench`), Canary-Aufgaben (§ 2.3), später Code-Ausführung in der Sandbox (Phase 4) |
| 2 | **Canary**: Aufgabe mit bekannter Lösung, unauffällig eingestreut | wie objektiv | § 2.3 |
| 3 | **Prüferurteil** eines Agenten anderer Familie (`ok` / `issues`) | Partie mit halbem Gewicht; nur wenn der Prüfer selbst eine ausreichende Bewertung in der Fähigkeit hat | jeder Auftrag mit Prüfer |
| 4 | **Nutzerfeedback** zum Endergebnis (gut / schlecht) | Partie mit Gewicht ¼ für alle beteiligten Ersteller | Gateway, § 8 |
| – | Selbstauskunft (`strength`, `confidence`) | nur Startwert, nie ein Signal | Fähigkeitsanzeige |

Paarvergleiche („A ist besser als B“) aus parallelen Mustern werden als Partie zwischen den beiden Agenten gewertet, wenn ein Signal der Ränge 1–3 sie entscheidet.

**Gegen Verzerrung:**
- Ein Prüfer, dessen Mängelbefunde sich oft als Fehlalarm erweisen (objektives Signal widerspricht), verliert Gewicht als Prüfer. Dafür führt das Profil je Agent zusätzlich eine **Prüferbewertung** (Trefferquote, Fehlalarmquote), getrennt von seiner Bewertung als Ersteller. Benchmark Runde 1 hat gezeigt, wie nötig das ist.
- Prüfer geben ihr Urteil ab, bevor sie andere Urteile sehen (wie bisher).
- Ein Agent bewertet nie sich selbst; Prüfer derselben Familie zählen nicht.

### 2.3 Canary-Aufgaben

Ein Knoten KANN fremde Agenten gelegentlich mit Aufgaben bekannter Lösung prüfen, etwa aus dem Benchmark-Satz. Canaries sind öffentliche Aufgaben (Freigabeklasse `public`) und sehen aus wie gewöhnliche Teilaufträge.

- Höchstens 5 % der Teilaufträge an einen Agenten, bei neuen Agenten in der **Neulingsphase** mehr (bis zur RD-Schwelle).
- Canaries verbrauchen fremde Rechenzeit: Sie zählen in der Bilanz (Quittungen) wie normale Aufträge, und ein Knoten SOLL sie nur an Peers schicken, für die er auch selbst rechnet oder die er vertraut.
- Der gemeinsame Benchmark-Pool mit Commit-Reveal ist Phase 4; in Phase 3 reicht ein lokaler Satz.

### 2.4 Teilen von Bewertungen

In Phase 3 bleibt das Profil lokal. `TRUST_ATTESTATION` (selektiv geteilte Erfahrungen, gewichtet mit dem eigenen Vertrauen in den Absender) ist vorgesehen, aber erst umzusetzen, wenn Manipulationstests (§ 10, Stufe 4) zeigen, dass lokales Lernen allein zu langsam ist.

## 3. Auswahl: Bandit über Rezepte

### 3.1 Rezepte

Ein Rezept beschreibt Muster und Rollen abstrakt:

```
single(spec=bester)
draft_verify(spec=bester, verifier=andere_familie)
parallel(n=3, families=verschieden) + synth(lokal)
parallel_verify(n=3) + synth + verifier
cascade(stufe1=günstig, stufe2=bester, schwelle=θ)
decompose(planner=lokal) + Teilaufgaben je eigenes Rezept
```

Erst beim Ausführen werden die Rollen mit konkreten Agenten besetzt: der Ersteller nach Profil (§ 2), der Prüfer nach Prüferbewertung und Familienvielfalt. So bleibt der Rezeptraum klein (Größenordnung 10 je Kategorie), auch wenn es viele Agenten gibt.

### 3.2 Auswahl

1. **Harte Filter:** Modalität, Freigabeklasse, verfügbare Agenten und Familien, Budget und Deadline der Anfrage. Rezepte, die sich damit nicht besetzen lassen, fallen weg.
2. **Thompson Sampling je Aufgabenkategorie:** Für jedes Rezept führt der Knoten eine Beta-Verteilung über die Erfolgsquote (Signal der Ränge 1–4, gewichtet wie § 2.2) und den mittleren Aufwand in Tokens. Gewählt wird das **günstigste Rezept, dessen gezogene Erfolgsquote den Sollwert `q*` der Kategorie erreicht**; erreicht keines den Sollwert, das mit der höchsten gezogenen Quote.
3. **Exploration:** Ein kleiner Anteil (Standard 5 %) geht an Rezepte bzw. Besetzungen mit neuen Agenten, damit sie überhaupt Bewertungen bekommen.
4. **Vorgabe des Nutzers gewinnt:** Fordert die Anfrage ein Muster ausdrücklich an (`swarmainet/<muster>` oder Header), wird es genommen; der Bandit lernt trotzdem aus dem Ergebnis.

Die bisherige regelbasierte Auswahl wird zum **Vorwissen** (Prior) des Bandits und zum Rückfall, solange zu wenig Daten vorliegen.

### 3.3 Abgrenzung

Der Bandit wählt diskret, *womit* gearbeitet wird. Wie viel Aufwand innerhalb eines Rezepts (Prüfrunden, Zahl paralleler Modelle) bestimmt der Regler (§ 4). Ein PID-Regler für die Auswahl ist ausdrücklich nicht vorgesehen.

## 4. Regelung des Aufwands

### 4.1 Regelkreise

| Regelkreis | Ist | Soll | Stellgröße |
|---|---|---|---|
| Qualität (außen) | gefilterte Erfolgsquote je Kategorie | `q*`, z. B. 0,9 | Aufwandsbudget: Zahl der Modelle, Prüfrunden |
| Kaskade | Fehlerquote der günstigen Stufe | z. B. ≤ 5 % | Konfidenzschwelle `θ` für die Eskalation |
| Latenz | p90-Antwortzeit je Kategorie | z. B. 20 s | Parallelität, Fallback-Timeout |
| Eigene Last | Schwarmlast auf der eigenen Hardware | strengste Grenze der Ressourcen-Policy | angenommene Jobs, Slots (wie bisher im REM) |

### 4.2 Regelgesetz und Regeln

PI-Regler je Kategorie:

```
u_k = u_0 + K_p·e_k + K_i·Σ e_j·Δt,   e_k = q* − q̄_k
```

- **PI statt PID:** Die Qualitätsmessung ist verrauscht und kommt mit Totzeit.
- **Glättung:** `q̄` als EWMA (Standard α = 0,1) über die Signale der Kategorie.
- **Totband:** Innerhalb von ±0,02 um den Sollwert wird nicht nachgestellt.
- **Anti-Windup:** In der Begrenzung (minimaler bzw. maximaler Aufwand, keine Peers verfügbar) wird nicht weiter integriert.
- **Ganzzahlige Stellgröße:** `u` wird auf ganze Zahlen von Modellen bzw. Prüfrunden abgebildet, mit Hysterese und einer Abklingzeit (Standard 20 Aufträge) zwischen zwei Änderungen.
- **Gegen Schwingungen im Netz:** kleine Verstärkungen, Zufallsjitter, Absenken multiplikativ und Erhöhen additiv (AIMD).
- Der Regler kann **die Ressourcen-Policy anderer Knoten nie überstimmen**; „keine Kapazität“ ist eine Begrenzung, kein Regelfehler.

Sollwerte und Reglerparameter je Kategorie sind konfigurierbar (`cooperation.targets`). Ihre Kalibrierung ist eine offene Frage (§ 12).

## 5. Muster: Kaskade

Neues Muster `cascade`: Zuerst löst ein günstiges Modell (kleinste erwartete Kosten bei ausreichender Bewertung). Ist das Ergebnis unsicher, eskaliert der Koordinator an das beste verfügbare Modell. „Unsicher“ heißt in dieser Reihenfolge:

1. ein Prüfer anderer Familie meldet Mängel, oder
2. bei `reasoning.math` weicht das vom Prüfer selbst berechnete Ergebnis ab, oder
3. die Selbstauskunft `confidence` liegt unter `θ` (schwächstes Kriterium, nur in Kombination).

`θ` stellt der Kaskaden-Regler (§ 4.1). Die Kaskade ist laut Konzept der künftige Standard für den Großteil der Anfragen, weil sie Aufwand nur dort einsetzt, wo er nötig ist.

## 6. Muster: Zerlegen und Verteilen (Task-Graph)

### 6.1 Graph

Für mehrteilige Aufgaben erstellt ein **Planer** (bevorzugt lokal, weil er das Gesamtbild sieht) einen gerichteten, kreisfreien Graphen von Teilaufgaben:

```json
{
  "nodes": [
    { "id": "a", "skill": "code.python", "prompt": "…", "deps": [] },
    { "id": "b", "skill": "code.review", "prompt": "…", "deps": ["a"] },
    { "id": "c", "skill": "general",     "prompt": "…", "deps": ["a", "b"] }
  ],
  "synthesis": "…"
}
```

- Der Koordinator prüft den Graphen: kreisfrei, höchstens `max_subtasks` Knoten, nur bekannte Fähigkeiten, Prompts innerhalb der Größenbegrenzung. Ungültige Graphen fallen auf ein einfacheres Rezept zurück.
- Teilaufgaben ohne offene Abhängigkeiten laufen parallel, jede mit eigenem, vom Bandit gewähltem Rezept.
- Ergebnisse von Vorgängern gehen als **Daten** in die Prompts der Nachfolger, in Begrenzern wie bisher (Freigabeklassen Regel 5).
- Alle Teilaufgaben teilen sich das Budget des Auftrags (Tokens, Deadline, Teilaufträge) und erben Freigabeklasse und Delegationstiefe.
- Ein **Synthesizer** (bevorzugt lokal) führt die Ergebnisse am Ende zusammen.

### 6.2 Fehler

Scheitert eine Teilaufgabe nach den üblichen Ausweichversuchen, werden ihre Nachfolger nicht ausgeführt. Der Synthesizer erhält, was vorliegt, und nennt die Lücke unter `limitations`.

## 7. Lease-Koordinator

### 7.1 Zweck

Heute koordiniert der Knoten, an dessen Gateway die Anfrage kommt, und das Ergebnis geht synchron an den Aufrufer zurück. Fällt er aus, ist der Auftrag verloren. Das ist für kurze Anfragen tragbar, nicht aber für lange Aufträge (Task-Graph, viele Prüfungen) oder für Knoten ohne eigene Modelle, die die Koordination abgeben wollen, wie das Notebook im Hardwaretest.

Phase 3 führt deshalb **asynchrone Aufträge** und einen **Lease** ein.

### 7.2 Ablauf

1. Der Auftraggeber schickt den Auftrag an einen **Koordinator** (er selbst oder ein vertrauter Peer, der Koordination anbietet) und erhält eine **Job-ID**.
2. Der Koordinator hält einen **Lease** mit Ablaufzeit (Standard 60 s) und erneuert ihn, solange er arbeitet.
3. Er legt **Prüfpunkte** ab: Graph, erledigte Teilergebnisse, verbrauchtes Budget. Sie gehen an genau einen **Ersatzkoordinator**, den der Auftraggeber benennt oder der Koordinator unter den vertrauten Peers wählt. Der Prüfpunkt enthält Prompts und Teilergebnisse; der Ersatz MUSS deshalb dieselbe Freigabeklasse erfüllen wie der Auftrag (`trusted` nur an vertraute Peers, `local` nie).
4. Läuft der Lease ab, ohne erneuert zu werden, **übernimmt der Ersatz**: Er erhöht die Lease-Epoche, macht beim letzten Prüfpunkt weiter und erneuert den Lease selbst.
5. Der Auftraggeber fragt das Ergebnis über die Job-ID ab, beim Koordinator oder beim Ersatz.

### 7.3 Doppelte Ausführung

- Jeder Lease hat eine **Epoche**. Teilaufträge tragen sie in der Payload; Anbieter lehnen Teilaufträge einer älteren Epoche ab, sobald sie eine neuere gesehen haben.
- Teilaufträge sind über `(task_id, subtask_id)` idempotent; bereits quittierte Teilergebnisse werden nicht neu angefordert (Quittungen sind schon idempotent, Protokoll § 4.4).
- Ein Koordinator, der nach einer Unterbrechung zurückkehrt und eine neuere Epoche sieht, bricht ab (`TASK_CANCEL` an seine offenen Teilaufträge).

## 8. Protokoll und Schnittstellen

Neue Nachrichten und Felder (Versionierung wie bisher: neue optionale Payload-Felder sind kompatibel):

| Nachricht / Feld | Zweck |
|---|---|
| `TASK_BID` (bisher „ab Phase 3“) | optionales Gebot vor der Annahme: freie Kapazität, erwartete Laufzeit; der Koordinator kann mehrere Anbieter anfragen und den günstigsten nehmen |
| `VERIFY_REQUEST` (bisher „ab Phase 3“) | eigene Nachricht für Prüfungen statt `TASK_PROPOSE` mit `role: verifier` |
| Protokoll `/swarmainet/job/0.1` | asynchrone Aufträge: `JOB_SUBMIT`, `JOB_STATUS`, `JOB_RESULT` |
| Protokoll `/swarmainet/lease/0.1` | `LEASE_CHECKPOINT` (Koordinator → Ersatz), `LEASE_RENEW`, `LEASE_TAKEOVER` |
| `ProposePayload.lease_epoch` | Epoche des Koordinators (§ 7.3) |
| `TRUST_ATTESTATION` | weiterhin zurückgestellt (§ 2.4) |

Gateway:

- `POST /v1/feedback` mit `{ "task_id": …, "good": true|false }`: Nutzerfeedback (§ 2.2, Rang 4).
- Modellname `swarmainet` wählt künftig über den Bandit; `swarmainet/<muster>` erzwingt weiterhin ein Muster.
- `GET /api/profile`: Kompetenzprofil und Reglerzustand je Kategorie (Diagnose, „Regelungsstabilität“ im Messplan).

Konfiguration (Entwurf):

```yaml
cooperation:
  selection: bandit              # bandit | rules (bisheriges Verhalten)
  exploration: 0.05
  targets:                       # Sollwerte je Kategorie
    code:  { quality: 0.9, latency_p90_s: 60 }
    math:  { quality: 0.95 }
    general: { quality: 0.85, latency_p90_s: 20 }
  canary_share: 0.05
  coordinate_for_others: false   # Koordination für vertraute Peers anbieten (§ 7)
```

## 9. Datenschutz

- Profile, Rezeptstatistik und Reglerzustand sind lokal und enthalten keine Prompts, nur Kategorien, Agenten und Ergebnissignale.
- Prüfpunkte eines Auftrags (§ 7) enthalten Prompts und Teilergebnisse und unterliegen der Freigabeklasse des Auftrags.
- Canaries sind öffentliche Aufgaben ohne Bezug zu Nutzerdaten.

## 10. Gate und Messung

Das Gate ist die Kernhypothese (Messplan). Geprüft wird mit den Versuchsstufen des Messplans:

| Stufe | Inhalt | Voraussetzung |
|---|---|---|
| 1–2 | Einzelmodelle und feste Muster | Benchmark Runde 1 (erledigt, Deckeneffekt) |
| 3 | Bandit und PI-Regler gegen feste Muster und gegen das beste Einzelmodell mit Self-Consistency bei gleichem Budget | Umsetzung § 2–5, schwerere Aufgaben, **mindestens drei verschiedene Modellfamilien ohne Modellwechsel** |
| 4 | mehrere Koordinatoren, Ausfall eines Koordinators, simulierte manipulierte Bewertungen (ein Agent liefert absichtlich schlechte Ergebnisse oder Fehlalarme) | Umsetzung § 6–7 |

Kennzahlen zusätzlich zum Messplan: Konvergenz des Bandits (wie viele Aufträge bis zur stabilen Wahl), Regelungsstabilität (Schwingen des Aufwands), Anteil Exploration, Erkennungsdauer eines manipulierenden Agenten.

**Hinweis zum Stand:** Mit den derzeit verfügbaren Modellen (zwei Familien ohne Modellwechsel) ist Stufe 3 nicht aussagekräftig. Die Mechanismen lassen sich trotzdem bauen und mit Simulationen (künstliche Agenten mit bekannter Qualität) prüfen; die Messung des Gates folgt, sobald unterschiedlichere Modelle bereitstehen.

## 11. Umsetzung in Stufen

1. **Kompetenzprofil und Signale:** Glicko-2 je Agent und Fähigkeit, Prüferbewertung, Signale aus Benchmark, Prüferurteil und `POST /v1/feedback`; Anzeige über `GET /api/profile`. Prüfbar mit künstlichen Agenten bekannter Qualität.
2. **Bandit über Rezepte** mit der heutigen Auswahl als Vorwissen, und Muster **Kaskade**.
3. **PI-Regler** für den Prüfaufwand je Kategorie.
4. **Task-Graph**: Planer, Zerlegen und Verteilen, Synthese.
5. **Asynchrone Aufträge und Lease-Koordinator**, Ausfalltests.
6. **Canary-Aufgaben** und Manipulationstests.

Jede Stufe bleibt hinter `cooperation.selection: rules` abschaltbar, damit das bisherige Verhalten als Vergleich erhalten bleibt.

## 12. Offene Fragen

1. **Kalibrierung:** Wie werden Sollwerte und Reglerparameter je Kategorie festgelegt? Vorschlag: Startwerte aus dem Benchmark, dann über Stufe 3 justieren.
2. **Prüfer ohne Ground Truth:** Im Alltag fehlt meist ein objektives Signal. Wie stark darf das Prüferurteil allein das Profil verschieben, ohne dass sich Prüfer-Fehlurteile festsetzen?
3. **Rezeptraum:** Welche Rezepte gehören in den Startsatz je Kategorie, und wann wird ein neues Rezept aufgenommen?
4. **Koordination für andere:** Darf ein Knoten auch für nicht vertraute Peers koordinieren (offenes Netz)? Dann sieht er das Gesamtbild fremder Aufträge.
5. Aus dem Gesamtkonzept: Ab wann lohnt ein zusätzlicher Peer aus Qualitätsgründen und nicht nur wegen Kapazität?
