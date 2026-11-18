# Messplan: Einzelmodell gegen Schwarm

[English](../measurement-plan.md) · **Deutsch**

Kernhypothese: Der Schwarm liefert **bei gleichem Budget** eine höhere geprüfte Qualität als der beste Einzelknoten und als ein fester Multi-Modell-Ablauf – oder erkennt zuverlässig, dass ein Einzelmodell genügt. Das ist das Gate von Phase 3; wird es verfehlt, wird das Konzept vor Phase 4 überarbeitet.

## Aufgabensätze

| Kategorie | Umfang | Bewertung |
|---|---|---|
| Code | 30–100 | Unit-Tests in Sandbox |
| Mathematik | 30–100 | exakte Lösung |
| Faktenprüfung | 30–100 | Ground Truth, blind bewertet |
| Multimodale Analyse | 30–100 | Ground Truth (ab Vision-Agenten) |
| Offene Planung | 30–100 | blinder Paarvergleich durch Modelle anderer Familien + Mensch |

Aufgaben werden versioniert und vor dem Lauf per Hash festgelegt (Commit-Reveal), damit kein Ablauf nachträglich angepasst wird.

## Versuchsstufen

1. Drei heterogene Modelle lösen alle Aufgaben einzeln (Baseline: bestes Einzelmodell pro Kategorie).
2. Schwarm mit festem Ablauf: Entwurf + Prüfer + Synthese (`swarmainet/draft_verify_synthesize`). Vergleichsvarianten: unabhängige Lösungen mit Synthese (`swarmainet/parallel`) und zusätzlich mit Prüfung der Synthese (`swarmainet/parallel_verify`).
3. PI-Regler passt die Zahl der Prüfungen dynamisch an.
4. Mehrere unabhängige Koordinatoren im P2P-Netz, mit Ausfalltests und simulierten manipulierten Bewertungen.

## Budget-Gleichheit

Budget = GPU-Sekunden und Tokens über alle beteiligten Modelle. Ein Einzelmodell darf das gleiche Budget für Self-Consistency (mehrfach lösen, Mehrheit/Selbstprüfung) nutzen; sonst wäre der Vergleich unfair.

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
