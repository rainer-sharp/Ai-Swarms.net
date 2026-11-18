# Benchmark Runde 1: Ergebnis

[English](../report-r1.md) · **Deutsch**

Lauf vom 17.10.2026, 17:39–21:28 Uhr, auf dem Testaufbau aus [`spec/de/benchmark.md`](../../spec/de/benchmark.md) (Koordinator VM, vLLM auf `.94`, llama.cpp und Ollama auf `.95`). Aufgabensatz `tasks-r1.jsonl` (SHA-256 `ad80b86d…2cc841`), Plan `plan-r1.yaml`, Rohdaten `results-r1.jsonl` (800 Ergebnisse, 1 Fehler).

Erzeugt mit `./swarmainet bench report bench/results-r1.jsonl`; Referenz ist `einzel-flash`.

## Bewertung

1. **Deckeneffekt.** flash scheitert allein nur an HumanEval/124. Alle Unterschiede liegen bei ±1–3 Aufgaben, kein McNemar-Test ist signifikant (kleinstes p = 0,625). Runde 1 zeigt: Der Schwarm schadet kaum, kostet aber das 3- bis 8-Fache an Tokens.
2. **Der Vorsprung von `parallel_verify` (40/40 Code) ist Zufall:** Er beruht allein auf HumanEval/124, das auch qwen27b, gpt-oss, `schwarm-single` und `sc3-flash` lösen.
3. **Self-Consistency ist bei gleicher Qualität günstiger:** `sc3-flash` erreicht 100 % mit etwa einem Drittel der Tokens von `parallel_verify`.
4. **`parallel` ohne Prüfer verschlechtert:** scheitert an HumanEval/116, /132 und /140, die flash allein löst. Die Synthese zweier Lösungen macht richtigen Code falsch; der Prüfer in `parallel_verify` fängt das ab.
5. **HumanEval/132** ist die einzige schwierige Aufgabe: qwen27b, gpt-oss, llama einzeln sowie `draft-verify`, `parallel` und `parallel-verify-llama` scheitern, flash, `sc3-flash`, `schwarm-single` und `parallel_verify` lösen sie.
6. **Prüfer vermutlich zu kritisch:** In `draft-verify` 17-mal „issues“, davon 14 bei Mathe-Entwürfen von gpt-oss, das allein alle 40 Matheaufgaben löst. Nicht belegbar, weil die Zwischentexte nicht gespeichert werden.
7. **Ollama-Modellwechsel** (llama3.1 als dritte Familie) heben die Median-Latenz von 24 auf 39 s ohne Qualitätsgewinn.
8. **Fehler der Einzelmodelle überlappen kaum** – Voraussetzung für einen Schwarm-Vorteil, mit diesen Aufgaben aber nicht ausnutzbar.
9. **Verteilung:** Bei `schwarm-single` landen alle Matheaufgaben bei gpt-oss, weil nur dieses Modell den Skill `reasoning.math` anbietet (Knoten-Konfiguration).

## Folgerungen für Runde 2

- Schwerere Aufgaben, bei denen das beste Einzelmodell etwa 50–70 % löst (z. B. HumanEval+ aus EvalPlus, LiveCodeBench, MATH Level 5), etwa 80 je Art.
- Zwischenergebnisse (Entwurf, Prüferbefund, Synthese) mitschreiben, um Trefferquote und Fehlalarme des Prüfers zu messen.
- `reasoning.math` auch bei den Qwen-Modellen anbieten.
- Prüfer bei Mathe nur bei nachweisbarem Rechen- oder Logikfehler „issues“ melden lassen.

## Bericht

### code

| Lauf | gelöst | Quote | Fehler | Tokens/gelöst (95 %) | Median s | Δ zur Referenz (95 %) | nur Ref / nur Lauf | p (McNemar) |
|---|---|---|---|---|---|---|---|---|
| einzel-flash | 39/40 | 98 % | 0 | 472 [395; 566] | 4.9 | Referenz | – | – |
| einzel-qwen27b | 39/40 | 98 % | 0 | 413 [357; 476] | 5.8 | +0.0 pp [-7.5; +7.5] | 1 / 1 | 1.000 |
| einzel-llama31 | 25/40 | 62 % | 0 | 580 [446; 801] | 3.8 | -35.0 pp [-50.0; -20.0] | 14 / 0 | 0.000 |
| einzel-gptoss | 35/40 | 88 % | 1 | 994 [769; 1370] | 8.8 | -10.0 pp [-22.5; +0.0] | 5 / 1 | 0.219 |
| schwarm-single | 39/40 | 98 % | 0 | 429 [371; 492] | 6.0 | +0.0 pp [-7.5; +7.5] | 1 / 1 | 1.000 |
| schwarm-draft-verify | 39/40 | 98 % | 0 | 1776 [1455; 2206] | 11.9 | +0.0 pp [-7.5; +7.5] | 1 / 1 | 1.000 |
| schwarm-parallel | 37/40 | 92 % | 0 | 2293 [1967; 2701] | 15.9 | -5.0 pp [-15.0; +5.0] | 3 / 1 | 0.625 |
| schwarm-parallel-verify | 40/40 | 100 % | 0 | 3776 [3183; 4485] | 24.2 | +2.5 pp [+0.0; +7.5] | 0 / 1 | 1.000 |
| schwarm-parallel-verify-llama | 39/40 | 98 % | 0 | 4423 [3769; 5338] | 39.3 | +0.0 pp [-7.5; +7.5] | 1 / 1 | 1.000 |
| sc3-flash | 40/40 | 100 % | 0 | 1051 [791; 1340] | 13.1 | +2.5 pp [+0.0; +7.5] | 0 / 1 | 1.000 |

### math

| Lauf | gelöst | Quote | Fehler | Tokens/gelöst (95 %) | Median s | Δ zur Referenz (95 %) | nur Ref / nur Lauf | p (McNemar) |
|---|---|---|---|---|---|---|---|---|
| einzel-flash | 40/40 | 100 % | 0 | 484 [437; 533] | 8.5 | Referenz | – | – |
| einzel-qwen27b | 39/40 | 98 % | 0 | 599 [458; 860] | 12.1 | -2.5 pp [-7.5; +0.0] | 1 / 0 | 1.000 |
| einzel-llama31 | 36/40 | 90 % | 0 | 329 [293; 375] | 4.4 | -10.0 pp [-20.0; -2.5] | 4 / 0 | 0.125 |
| einzel-gptoss | 40/40 | 100 % | 0 | 498 [438; 561] | 4.9 | +0.0 pp [+0.0; +0.0] | 0 / 0 | 1.000 |
| schwarm-single | 40/40 | 100 % | 0 | 537 [454; 633] | 5.2 | +0.0 pp [+0.0; +0.0] | 0 / 0 | 1.000 |
| schwarm-draft-verify | 40/40 | 100 % | 0 | 1532 [1266; 1848] | 7.9 | +0.0 pp [+0.0; +0.0] | 0 / 0 | 1.000 |
| schwarm-parallel | 40/40 | 100 % | 0 | 2001 [1803; 2201] | 18.4 | +0.0 pp [+0.0; +0.0] | 0 / 0 | 1.000 |
| schwarm-parallel-verify | 40/40 | 100 % | 0 | 2933 [2662; 3207] | 21.9 | +0.0 pp [+0.0; +0.0] | 0 / 0 | 1.000 |
| schwarm-parallel-verify-llama | 40/40 | 100 % | 0 | 3502 [3230; 3782] | 37.0 | +0.0 pp [+0.0; +0.0] | 0 / 0 | 1.000 |
| sc3-flash | 40/40 | 100 % | 0 | 1558 [1399; 1724] | 26.5 | +0.0 pp [+0.0; +0.0] | 0 / 0 | 1.000 |

#### Fehlerüberlappung der Einzelmodelle, code (gemeinsame Fehlschläge / Fehlschläge von mindestens einem)

| | einzel-flash | einzel-qwen27b | einzel-llama31 | einzel-gptoss |
|---|---|---|---|---|
| einzel-flash | – | 0.00 | 0.07 | 0.00 |
| einzel-qwen27b | 0.00 | – | 0.07 | 0.20 |
| einzel-llama31 | 0.07 | 0.07 | – | 0.11 |
| einzel-gptoss | 0.00 | 0.20 | 0.11 | – |

#### Fehlerüberlappung der Einzelmodelle, math (gemeinsame Fehlschläge / Fehlschläge von mindestens einem)

| | einzel-flash | einzel-qwen27b | einzel-llama31 | einzel-gptoss |
|---|---|---|---|---|
| einzel-flash | – | 0.00 | 0.00 | – |
| einzel-qwen27b | 0.00 | – | 0.00 | 0.00 |
| einzel-llama31 | 0.00 | 0.00 | – | 0.00 |
| einzel-gptoss | – | 0.00 | 0.00 | – |
