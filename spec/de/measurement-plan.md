# Messplan: Einzelmodell gegen Schwarm

[English](../measurement-plan.md) · **Deutsch**

Zwei Hypothesen, jede ein Grund für einen Schwarm:

1. **Qualität (Hypothese 1):** Der Schwarm liefert **bei gleichem Budget** eine höhere geprüfte Qualität als der beste Einzelknoten und als ein fester Multi-Modell-Ablauf – oder erkennt zuverlässig, dass ein Einzelmodell genügt.
2. **Spezialisten (Hypothese 2):** Ein Team **kleiner Spezialisten** erreicht bei gemischten, mehrteiligen Aufgaben die Qualität eines großen Generalisten mit deutlich weniger Hardware je Gerät, oder es löst Aufgaben, die keins seiner Mitglieder allein löst ([Abschnitt unten](#hypothese-2-team-kleiner-spezialisten)).

Das Gate von Phase 3 ist erfüllt, wenn Hypothese 1 oder Hypothese 2 zutrifft; werden beide verfehlt, wird das Konzept vor Phase 4 überarbeitet.

## Aufgabensätze

| Kategorie | Umfang | Bewertung |
|---|---|---|
| Code | 30–100 | Unit-Tests in Sandbox |
| Mathematik | 30–100 | exakte Lösung |
| Faktenprüfung | 30–100 | Ground Truth, blind bewertet |
| Multimodale Analyse | 30–100 | Ground Truth (ab Vision-Agenten) |
| Offene Planung | 30–100 | blinder Paarvergleich durch Modelle anderer Familien + Mensch |
| Gemischt, mehrteilig (Hypothese 2) | 30–100 | je Teil: Unit-Tests, exakter Wert oder Muster; gelöst, wenn alle Teile stimmen ([Benchmark](benchmark.md#gemischter-aufgabensatz-hypothese-2)) |

Aufgaben werden versioniert und vor dem Lauf per Hash festgelegt (Commit-Reveal), damit kein Ablauf nachträglich angepasst wird.

## Versuchsstufen

1. Drei heterogene Modelle lösen alle Aufgaben einzeln (Baseline: bestes Einzelmodell pro Kategorie).
2. Schwarm mit festem Ablauf: Entwurf + Prüfer + Synthese (`swarmainet/draft_verify_synthesize`). Vergleichsvarianten: unabhängige Lösungen mit Synthese (`swarmainet/parallel`) und zusätzlich mit Prüfung der Synthese (`swarmainet/parallel_verify`).
3. PI-Regler passt die Zahl der Prüfungen dynamisch an.
4. Mehrere unabhängige Koordinatoren im P2P-Netz, mit Ausfalltests und simulierten manipulierten Bewertungen.

## Budget-Gleichheit

Budget = GPU-Sekunden und Tokens über alle beteiligten Modelle. Ein Einzelmodell darf das gleiche Budget für Self-Consistency (mehrfach lösen, Mehrheit/Selbstprüfung) nutzen; sonst wäre der Vergleich unfair.

## Hypothese 2: Team kleiner Spezialisten

**Behauptung:** Bei gemischten, mehrteiligen Aufgaben (Code, Rechnung, Fakten, später Bilder und Audio in einer Anfrage) ist ein Team kleiner Spezialisten – je höchstens etwa 8 Milliarden Parameter und lauffähig auf einem Gerät mit höchstens 12 GB VRAM – **nicht schlechter** als ein großer Generalist (24 Milliarden Parameter oder mehr auf einem Gerät), oder es löst Aufgaben, die keins seiner Mitglieder allein löst.

**Aufbauten,** jeweils auf demselben Aufgabensatz:

| Aufbau | Ablauf |
|---|---|
| A · Team | die Spezialisten als Schwarm mit `swarmainet/decompose`: Der Planer zerlegt, jeder Teil geht an den passenden Spezialisten |
| B · Generalist | das große Modell allein (`swarmainet/single`, mit `X-SwarmAiNet-Agents` darauf beschränkt) |
| C · jeder Spezialist allein | jedes Mitglied allein auf allen Aufgaben, um die Abdeckung zu messen |

**Kennzahlen:** Erfolgsquote (alle Teile richtig) und Erfolgsquote je Teil; **Abdeckung** (Aufgaben, die A löst und kein Aufbau aus C vollständig); höchster VRAM-Bedarf des größten beteiligten Geräts und die Summe über alle Geräte; GPU-Sekunden, Energie, soweit gemessen, Latenz und Tokens.

**Statistik:** gepaarter Vergleich A gegen B auf denselben Aufgaben. Hypothese 2 trifft zu, wenn die untere Grenze des 95-%-Bootstrap-Intervalls der Differenz der Erfolgsquoten (A − B) über −5 Prozentpunkten liegt und A dabei höchstens die Hälfte des VRAM je Gerät braucht, oder wenn die Abdeckung signifikant ist (McNemar, A gegen den besten Aufbau aus C).

**Voraussetzungen:** der Aufgabensatz (Entwurf in [`bench/tasks-mix-draft.jsonl`](../../bench/tasks-mix-draft.jsonl)), Unterstützung mehrteiliger Aufgaben in `swarmainet bench`, Spezialisten aus mindestens drei Modellfamilien ohne Modellwechsel. Die Ergebnisse werden besser, sobald Router und Planer das Kompetenzprofil nutzen ([Kooperation](cooperation.md) § 10).

## Kennzahlen

| Kennzahl | Vergleich | Quelle im Knoten |
|---|---|---|
| Erfolgsquote | Schwarm vs. bestes Einzelmodell | Testergebnisse |
| Geprüfte Korrektheit | Anteil nachprüfbar belegter Aussagen | Evidenz im Ergebnisobjekt |
| Fehlerkorrelation | wiederholen unabhängige Modelle dieselben Fehler? | Einzellösungen aus Stufe 1 |
| Qualität pro Aufwand | Mehrwert pro GPU-Sekunde, Token, Joule, Euro | Quittungen (`duration_ms`, Tokens) |
| Latenz | Zeit bis zur brauchbaren Antwort | `steps[].duration_ms` |
| Ausfallsicherheit | Ergebnis trotz Peer-Abbruch | Ausfalltest, `steps[].status` |
| Vielfalt | bringt ein andersartiger Peer messbar etwas? | Ablation pro Familie |
| Regelungsstabilität | schwingt der Aufwand? | Reglerverlauf (Phase 3) |
| Datenschutz-Exposition | welche Daten gingen an welche Identitäten? | Quittungen + Freigabeklasse |
| Teilnahmehürde | Zeit und Schritte bis zur ersten Bereitstellung | Nutzertests |

## Statistik

Pro Kategorie gepaarter Vergleich (gleiche Aufgaben), McNemar-Test für Erfolgsquoten, Bootstrap-Konfidenzintervalle für Qualität pro Aufwand. Ein Mehrwert gilt erst, wenn das 95-%-Intervall den Nullpunkt nicht enthält.
