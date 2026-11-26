# Benchmark: erste Runde

[English](../benchmark.md) · **Deutsch**

Konkretisiert die Versuchsstufen 1 und 2 aus dem [Messplan](measurement-plan.md) für den Testaufbau aus Phase 1. Status: **durchgeführt** am 17.10.2026 (`swarmainet bench`, Aufgaben, Plan und Ergebnis in [`bench/`](../../bench/de/README.md)); Frage 1 am Ende ist offen.

## Ausgangslage

Drei Knoten (VM, Host, Notebook), aber nur zwei Rechenserver:

| Server | Backend | Modelle | Familie |
|---|---|---|---|
| `192.168.101.94` | vLLM | qwen3.8-flash-next (dauerhaft geladen) | qwen |
| `192.168.101.95` | llama.cpp (Router, lädt bei Bedarf) | Qwen3.8-27B Q4 | qwen |
| `192.168.101.95` | Ollama, **ein Modell zur Zeit** | gpt-oss:20b, llama3.1:8b, qwen2.5-coder:7b, qwen3-vl:8b | gpt-oss, llama, qwen |

Folgen für die Messung:

1. **Gleichzeitig heißt auf `.95` nicht parallel.** Treffen zwei Ollama-Modelle im selben Auftrag aufeinander, wartet eines, und Ollama lädt bei jedem Wechsel neu. Die Laufzeit misst dann vor allem das Laden.
2. **Die Knoten verteilen nur, gerechnet wird auf zwei GPUs.** Für den Benchmark genügt ein Koordinator (die VM).
3. **GPU-Sekunden sind nicht sauber messbar** (Ladezeiten, geteilte GPU auf `.95`). Primäres Budget sind daher **Tokens** (Prompt + Completion über alle Schritte). Die Laufzeit wird zusätzlich erfasst, getrennt nach „mit Modellwechsel“ und „warm“.

## Modellauswahl

Pro Auftrag höchstens **ein** Ollama-Modell:

| Rolle | Modell | Server |
|---|---|---|
| qwen | qwen3.8-flash-next | `.94` vLLM |
| qwen (stark) | Qwen3.8-27B Q4 | `.95` llama.cpp |
| zweite Familie | gpt-oss:20b | `.95` Ollama |
| dritte Familie | llama3.1:8b | eigener Versuchsblock, siehe offene Frage 1 |

Ohne llama3.1 stehen zwei Familien ohne Ollama-Wechsel zur Verfügung. Das genügt für `draft_verify_synthesize`, ist für `parallel` und `parallel_verify` aber knapp.

## Aufgaben

Erste Runde nur mit automatisch prüfbaren Aufgaben:

| Kategorie | Anzahl | Bewertung |
|---|---|---|
| Code (Python) | 40 | Unit-Tests mit Zeitlimit, ohne Netz, in einem Wegwerf-Verzeichnis |
| Mathematik | 40 | exakte Zahl, aus der Antwort extrahiert |

Vorgeschlagene Quellen: Teilmengen aus HumanEval (Code) und GSM8K (Textaufgaben), beide frei verwendbar, englisch. Faktenprüfung, multimodale Analyse und offene Planung folgen später, da sie blinde oder menschliche Bewertung brauchen.

Der Aufgabensatz liegt als JSONL unter `bench/` und wird vor dem Lauf per SHA-256 festgeschrieben (Commit-Reveal, siehe Messplan).

## Ablauf

**Stufe 1 – Einzelmodelle.** Jedes Modell löst alle Aufgaben direkt am Backend, ohne Schwarm. Läufe sind nach Modell gruppiert (erst alle Aufgaben mit gpt-oss, dann mit llama3.1 …), damit Ollama nur einmal wechselt. Vor jedem Block ein Aufwärm-Aufruf.

**Stufe 2 – Schwarm.** Über das Gateway der VM mit den Mustern `single`, `draft_verify_synthesize`, `parallel` und `parallel_verify`. Aufträge strikt nacheinander. Pro Aufgabe erfasst: gelöst ja/nein, Tokens und Dauer je Schritt, beteiligte Modelle, Modellwechsel.

**Budget-Gleichheit.** Das beste Einzelmodell erhält dasselbe Token-Budget für Self-Consistency:

- Mathematik: mehrfach lösen, Mehrheitsentscheid.
- Code: mehrere Versuche; gewertet wird der erste, der die *sichtbaren* Beispieltests besteht (die verdeckten Tests entscheiden über „gelöst“).

**Auswertung** wie im Messplan:

- Erfolgsquote je Muster, gepaarter McNemar-Test gegen das beste Einzelmodell
- Tokens pro gelöster Aufgabe, Bootstrap-Konfidenzintervall
- Latenz (warm und mit Modellwechsel getrennt)
- Fehlerkorrelation: machen unabhängige Modelle dieselben Fehler?

Geschätzte Dauer: Stufe 1 etwa 2 h, Stufe 2 etwa 5–6 h. Ein Probelauf mit einer Aufgabe je Art und Lauf deutet eher auf 3–4 h insgesamt hin.

## Umsetzung

1. **Tokens pro Schritt:** `steps[]` der Gateway-Antwort enthält `prompt_tokens` und `completion_tokens`.
2. **Agenten pro Anfrage begrenzen:** Header `X-SwarmAiNet-Agents` mit kommagetrennten Agent-IDs; der Orchestrator wählt nur aus diesen. Vermeidet Ollama-Wechsel und erlaubt die Ablation pro Familie.
3. **`swarmainet bench run | report | hash`** (`internal/bench`): prüft den SHA-256 des Aufgabensatzes, führt die Läufe strikt nacheinander und fortsetzbar aus, prüft Code in einer `bwrap`-Sandbox (nur `/usr`, kein Netz, 1 GiB, 10 s CPU), schreibt `results.jsonl` und erzeugt den Bericht (Quote, Tokens pro gelöster Aufgabe mit Bootstrap-Intervall, Median-Latenz, gepaarter Vergleich mit McNemar, Fehlerüberlappung der Einzelmodelle).
4. **Aufgaben:** `bench/tasks-r1.jsonl`, Plan: `bench/plan-r1.yaml`.
5. **Zwischentexte** (nach Runde 1): Header `X-SwarmAiNet-Trace: 1` liefert in `steps[].output` die Texte der Schritte. `bench run` bewertet Entwürfe, Teillösungen und Synthesen einzeln (`step_solved`) und speichert den Prüferbefund (`issues`). Der Bericht zeigt je Lauf berechtigte Mängelbefunde, Fehlalarme und übersehene Fehler des Prüfers, ob die Überarbeitung repariert oder verschlechtert hat und wie oft die Synthese eine richtige Teillösung verliert oder aus falschen eine richtige macht.
6. **Mathe-Prüfer** (nach Runde 1): Bei `reasoning.math` löst der Prüfer die Aufgabe zuerst selbst, meldet nur Fehler, die das Ergebnis verfälschen, und gibt sein eigenes Ergebnis (`result`) an. Stimmt es mit dem Endergebnis des Entwurfs überein, verwirft der Orchestrator den Mängelbefund und überarbeitet nicht. Gegenprobe am 17.10.2026 (40 Matheaufgaben aus Runde 1, gleicher Aufbau, nur der Prompt verschieden): In `parallel_verify` sinken die Mängelbefunde von 11 auf 1 von 40, alle Fehlalarme (die Qwen-Prüfer melden besonders oft zu Unrecht), die Median-Latenz von 24,8 auf 18,9 s. In `draft_verify_synthesize` prüft in beiden Läufen gpt-oss und meldet nie Mängel. Der verbliebene Fehlalarm hat ein abweichendes Ergebnis und verschlechtert die Antwort (39/40 statt 40/40); die Verwerfungsregel hat nie gegriffen. Ob echte Fehler nun übersehen werden, zeigt erst Runde 2.

## Ergebnis Runde 1

Vollständiger Bericht: [`bench/de/report-r1.md`](../../bench/de/report-r1.md). Laufzeit knapp 4 h, 800 Ergebnisse.

| Lauf | Code | Mathe | Tokens/gelöst (Code / Mathe) |
|---|---|---|---|
| einzel-flash (Referenz) | 39/40 | 40/40 | 472 / 484 |
| schwarm-draft-verify | 39/40 | 40/40 | 1776 / 1532 |
| schwarm-parallel | 37/40 | 40/40 | 2293 / 2001 |
| schwarm-parallel-verify | 40/40 | 40/40 | 3776 / 2933 |
| sc3-flash | 40/40 | 40/40 | 1051 / 1558 |

- **Deckeneffekt:** Das beste Einzelmodell löst 79 von 80 Aufgaben; kein Unterschied ist signifikant. Der Schwarm schadet kaum, kostet aber das 3- bis 8-Fache an Tokens; Self-Consistency erreicht dieselbe Quote günstiger.
- `parallel` ohne Prüfer verschlechtert (Synthese macht richtigen Code falsch), der Prüfer in `parallel_verify` fängt das ab.
- Der Prüfer meldet vermutlich zu oft Mängel (17-mal in `draft-verify`, überwiegend bei Mathe-Entwürfen eines Modells, das allein alle Matheaufgaben löst).
- Die Fehler der Einzelmodelle überlappen kaum – die Voraussetzung für einen Schwarm-Vorteil bleibt bestehen.

**Für Runde 2:** schwerere Aufgaben (Ziel: bestes Einzelmodell bei 50–70 %), Zwischentexte mitschreiben (umgesetzt, siehe Umsetzung Punkt 5), `reasoning.math` auch bei den Qwen-Modellen, zurückhaltenderer Prüfer bei Mathe (umgesetzt, siehe Umsetzung Punkt 6).

## Gemischter Aufgabensatz (Hypothese 2)

Für [Hypothese 2](measurement-plan.md#hypothese-2-team-kleiner-spezialisten) (Team kleiner Spezialisten) verbindet jede Aufgabe mehrere Teile aus verschiedenen Bereichen, jeder automatisch prüfbar. Der Entwurf [`bench/tasks-mix-draft.jsonl`](../../bench/tasks-mix-draft.jsonl) enthält 10 Aufgaben mit je 3 Teilen (Code, Rechnung, Fakten, Daten aus einer Tabelle, Datumsangaben, eine Übersetzung), eigens für dieses Projekt geschrieben statt aus öffentlichen Benchmarks übernommen, damit kein Modell sie aus dem Training kennt. Jeder Teil ist gegen eine Musterlösung geprüft.

Format, eine Aufgabe je Zeile:

```json
{"id": "mix/04", "kind": "multi", "prompt": "…", "parts": [
  {"n": 1, "kind": "number", "answer": "5.4"},
  {"n": 2, "kind": "code", "entry_point": "total_cost", "tests": "def check(f): …"},
  {"n": 3, "kind": "text", "pattern": "(?i)^\\W*melon\\W*$"}]}
```

Der Prompt verlangt allen Code in einem einzigen Python-Block und endet mit einer Zeile `Answer <n>: <Wert>` je Teil, der kein Code ist.

| Teil | Bewertung |
|---|---|
| `code` | der Code aus der Antwort läuft mit den verdeckten Tests `check(f)` in der Sandbox, wie in Runde 1 |
| `number` | der Wert aus `Answer <n>:` stimmt mit `answer` überein (relative Abweichung höchstens 10⁻⁶, oder `tolerance`) |
| `text` | der Wert aus `Answer <n>:` passt zum regulären Ausdruck `pattern` |

Eine Aufgabe gilt als gelöst, wenn alle Teile stimmen. `swarmainet bench run` bewertet mehrteilige Aufgaben so und speichert das Ergebnis je Teil (`parts`, `part_kinds`); der Bericht ergänzt eine Tabelle mit den Teilen je Art und der **Abdeckung**: Aufgaben, die ein Lauf löst und kein Einzelmodell (direkt, eine Stichprobe) vollständig. Schwarm-Läufe senden für diese Aufgaben keine Fähigkeit, der Planer von `decompose` entscheidet; Self-Consistency nimmt den ersten Versuch, weil eine Mehrheitswahl über mehrere Teile nicht sinnvoll ist. [`bench/plan-mix.yaml`](../../bench/plan-mix.yaml) enthält die Aufbauten A bis C aus dem Messplan. Ein Probelauf mit drei Aufgaben gegen Qwen3.8 Flash löste 9 von 9 Teilen: Format und Bewertung funktionieren, der Entwurf ist für ein großes Modell aber zu leicht.

**Vor der Messung noch zu tun:** Ausbau auf 30 bis 100 Aufgaben mit schwereren Teilen, damit der Generalist nicht alles löst (Deckeneffekt aus Runde 1); Teile mit Bildern (erzeugte Diagramme und Tabellen mit bekannten Werten) und mit Audio (Aufnahmen mit bekanntem Text), damit sich die Abdeckung messen lässt.

## Offene Fragen

1. **llama3.1:8b als dritte Familie:** auf `.95` als GGUF über llama.cpp betreiben, oder reicht der VRAM für zwei geladene Ollama-Modelle (`OLLAMA_MAX_LOADED_MODELS=2`)? Bis dahin ist es ein eigener Block mit Modellwechseln (`schwarm-parallel-verify-llama` im Plan); in Runde 1 hob das die Median-Latenz von 24 auf 39 s ohne Qualitätsgewinn.
2. ~~Aufgabenquelle~~ – entschieden: HumanEval und GSM8K.
3. ~~Laufzeit~~ – entschieden: Lauf über Nacht.
