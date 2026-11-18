# Benchmark

**English** · [Deutsch](de/README.md)

Tasks and run plans for the benchmark from [`spec/benchmark.md`](../spec/benchmark.md).

| File | Contents |
|---|---|
| `tasks-r1.jsonl` | Round 1: 40 code tasks (HumanEval/0, /4, … /156) and 40 maths tasks (GSM8K test 0, 33, … 1287) |
| `plan-r1.yaml` | Runs for the test setup (vLLM on `.94`, llama.cpp and Ollama on `.95`) |
| `results-r1.jsonl` | Results, one line per run and task (created during the run) |
| `report-r1.md` | Report and assessment of round 1 (17 Oct 2026) |

## Procedure

```sh
./swarmainet bench hash bench/tasks-r1.jsonl          # SHA-256 must match tasks_sha256 in the plan
./swarmainet bench run -limit 1 bench/plan-r1.yaml    # trial run: one task per type for each run
./swarmainet bench run bench/plan-r1.yaml             # full run, overnight
./swarmainet bench report bench/results-r1.jsonl      # evaluation as Markdown
```

- The run is **resumable**: existing results are skipped. `-retry-errors` re-runs tasks that failed, `-only run1,run2` restricts to individual runs.
- If the SHA-256 of the task set does not match `tasks_sha256` in the plan, `bench run` refuses to run (commit-reveal).
- Swarm runs need a running node on the VM and the peers with the models named in `agents`.

## Assessment

- **Code:** the code from the answer runs with the hidden HumanEval tests in a sandbox (`bwrap`): only `/usr` readable, no network, no environment variables, 1 GiB memory, 10 s CPU, 20 s total time.
- **Mathematics:** the number from the last line `Answer: …` (otherwise the last number in the text) must exactly match the GSM8K solution.
- **Intermediate steps** (swarm runs): the run requests the texts of all steps with `X-SwarmAiNet-Trace: 1` and assesses drafts, partial solutions and syntheses like a final answer (`step_solved`). From this the report computes hit rate and false alarms of the verifier as well as the effect of revision and synthesis. The results file thereby grows to several MB.
- **Self-consistency** (`samples > 1`): mathematics by majority vote; code takes the first attempt that passes the examples from the docstring, otherwise the first attempt. 21 of the 40 code tasks have no usable examples (17 without, 4 with faulty examples in HumanEval).

## Licenses

The tasks come from [HumanEval](https://github.com/openai/human-eval) and [GSM8K](https://github.com/openai/grade-school-math), both © OpenAI under the MIT license (`LICENSE-human-eval`, `LICENSE-gsm8k`). The prompts have been extended with an answer instruction.
