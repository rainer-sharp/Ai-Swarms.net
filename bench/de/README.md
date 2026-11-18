# Benchmark

[English](../README.md) · **Deutsch**

Aufgaben und Lauf-Pläne für den Benchmark aus [`spec/de/benchmark.md`](../../spec/de/benchmark.md).

| Datei | Inhalt |
|---|---|
| `tasks-r1.jsonl` | Runde 1: 40 Code-Aufgaben (HumanEval/0, /4, … /156) und 40 Mathe-Aufgaben (GSM8K-Test 0, 33, … 1287) |
| `plan-r1.yaml` | Läufe für den Testaufbau (vLLM auf `.94`, llama.cpp und Ollama auf `.95`) |
| `results-r1.jsonl` | Ergebnisse, eine Zeile je Lauf und Aufgabe (entsteht beim Lauf) |
| `report-r1.md` | Bericht und Bewertung der Runde 1 (17.10.2026) |

## Ablauf

```sh
./swarmainet bench hash bench/tasks-r1.jsonl          # SHA-256 muss mit tasks_sha256 im Plan übereinstimmen
./swarmainet bench run -limit 1 bench/plan-r1.yaml    # Probelauf: je Lauf eine Aufgabe pro Art
./swarmainet bench run bench/plan-r1.yaml             # voller Lauf, über Nacht
./swarmainet bench report bench/results-r1.jsonl      # Auswertung als Markdown
```

- Der Lauf ist **fortsetzbar**: bereits vorhandene Ergebnisse werden übersprungen. `-retry-errors` führt Aufgaben mit Fehler erneut aus, `-only lauf1,lauf2` beschränkt auf einzelne Läufe.
- Stimmt der SHA-256 des Aufgabensatzes nicht mit `tasks_sha256` im Plan überein, verweigert `bench run` den Lauf (Commit-Reveal).
- Schwarm-Läufe brauchen einen laufenden Knoten auf der VM und die Peers mit den in `agents` genannten Modellen.

## Bewertung

- **Code:** Der Code aus der Antwort läuft mit den verdeckten HumanEval-Tests in einer Sandbox (`bwrap`): nur `/usr` lesbar, kein Netz, keine Umgebungsvariablen, 1 GiB Speicher, 10 s CPU, 20 s Gesamtzeit.
- **Mathematik:** Die Zahl aus der letzten Zeile `Answer: …` (sonst die letzte Zahl im Text) muss exakt der GSM8K-Lösung entsprechen.
- **Zwischenschritte** (Schwarm-Läufe): Der Lauf fordert mit `X-SwarmAiNet-Trace: 1` die Texte aller Schritte an und bewertet Entwürfe, Teillösungen und Synthesen wie eine Endantwort (`step_solved`). Daraus berechnet der Bericht Trefferquote und Fehlalarme des Prüfers sowie die Wirkung von Überarbeitung und Synthese. Die Ergebnisdatei wird dadurch mehrere MB groß.
- **Self-Consistency** (`samples > 1`): Mathematik per Mehrheitsentscheid; Code nimmt den ersten Versuch, der die Beispiele aus dem Docstring besteht, sonst den ersten Versuch. 21 der 40 Code-Aufgaben haben keine brauchbaren Beispiele (17 ohne, 4 mit fehlerhaften Beispielen in HumanEval).

## Lizenzen

Die Aufgaben stammen aus [HumanEval](https://github.com/openai/human-eval) und [GSM8K](https://github.com/openai/grade-school-math), beide © OpenAI unter MIT-Lizenz (`LICENSE-human-eval`, `LICENSE-gsm8k`). Die Prompts sind um eine Antwortanweisung ergänzt.
