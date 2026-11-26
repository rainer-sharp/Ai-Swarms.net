# Benchmark: first round

**English** · [Deutsch](de/benchmark.md)

Makes stages 1 and 2 of the [measurement plan](measurement-plan.md) concrete for the test setup from phase 1. Status: **carried out** on 17 Oct 2026 (`swarmainet bench`, tasks, plan and result in [`bench/`](../bench/README.md)); question 1 at the end is open.

## Starting point

Three nodes (VM, host, notebook), but only two compute servers:

| Server | Backend | Models | Family |
|---|---|---|---|
| `192.168.101.94` | vLLM | qwen3.8-flash-next (permanently loaded) | qwen |
| `192.168.101.95` | llama.cpp (router, loads on demand) | Qwen3.8-27B Q4 | qwen |
| `192.168.101.95` | Ollama, **one model at a time** | gpt-oss:20b, llama3.1:8b, qwen2.5-coder:7b, qwen3-vl:8b | gpt-oss, llama, qwen |

Consequences for the measurement:

1. **On `.95`, simultaneous does not mean parallel.** If two Ollama models meet in the same task, one waits, and Ollama reloads at every switch. The runtime then mainly measures loading.
2. **The nodes only distribute; computation happens on two GPUs.** One coordinator (the VM) is sufficient for the benchmark.
3. **GPU seconds cannot be measured cleanly** (loading times, shared GPU on `.95`). The primary budget is therefore **tokens** (prompt + completion across all steps). Runtime is recorded in addition, separated into "with model switch" and "warm".

## Model selection

At most **one** Ollama model per task:

| Role | Model | Server |
|---|---|---|
| qwen | qwen3.8-flash-next | `.94` vLLM |
| qwen (strong) | Qwen3.8-27B Q4 | `.95` llama.cpp |
| second family | gpt-oss:20b | `.95` Ollama |
| third family | llama3.1:8b | separate experiment block, see open question 1 |

Without llama3.1, two families are available without an Ollama switch. That is sufficient for `draft_verify_synthesize`, but tight for `parallel` and `parallel_verify`.

## Tasks

First round only with automatically checkable tasks:

| Category | Number | Assessment |
|---|---|---|
| Code (Python) | 40 | unit tests with a time limit, without network, in a throwaway directory |
| Mathematics | 40 | exact number, extracted from the answer |

Proposed sources: subsets of HumanEval (code) and GSM8K (word problems), both freely usable, in English. Fact checking, multimodal analysis and open planning follow later, as they need blind or human assessment.

The task set is stored as JSONL under `bench/` and fixed by SHA-256 before the run (commit-reveal, see measurement plan).

## Procedure

**Stage 1 – single models.** Each model solves all tasks directly at the backend, without the swarm. Runs are grouped by model (first all tasks with gpt-oss, then with llama3.1 …) so that Ollama switches only once. A warm-up call precedes each block.

**Stage 2 – swarm.** Via the VM's gateway with the patterns `single`, `draft_verify_synthesize`, `parallel` and `parallel_verify`. Tasks strictly one after another. Recorded per task: solved yes/no, tokens and duration per step, participating models, model switches.

**Equal budget.** The best single model gets the same token budget for self-consistency:

- Mathematics: solve several times, majority vote.
- Code: several attempts; the first one that passes the *visible* example tests counts (the hidden tests decide on "solved").

**Evaluation** as in the measurement plan:

- Success rate per pattern, paired McNemar test against the best single model
- Tokens per solved task, bootstrap confidence interval
- Latency (warm and with model switch separately)
- Error correlation: do independent models make the same errors?

Estimated duration: stage 1 about 2 h, stage 2 about 5–6 h. A trial run with one task per type and run rather points to 3–4 h in total.

## Implementation

1. **Tokens per step:** `steps[]` of the gateway response contains `prompt_tokens` and `completion_tokens`.
2. **Restrict agents per request:** header `X-SwarmAiNet-Agents` with comma-separated agent IDs; the orchestrator only chooses from these. Avoids Ollama switches and allows the ablation per family.
3. **`swarmainet bench run | report | hash`** (`internal/bench`): checks the SHA-256 of the task set, executes the runs strictly one after another and resumably, checks code in a `bwrap` sandbox (only `/usr`, no network, 1 GiB, 10 s CPU), writes `results.jsonl` and generates the report (rate, tokens per solved task with bootstrap interval, median latency, paired comparison with McNemar, error overlap of the single models).
4. **Tasks:** `bench/tasks-r1.jsonl`, plan: `bench/plan-r1.yaml`.
5. **Intermediate texts** (after round 1): header `X-SwarmAiNet-Trace: 1` returns the texts of the steps in `steps[].output`. `bench run` assesses drafts, partial solutions and syntheses individually (`step_solved`) and stores the verifier's findings (`issues`). The report shows per run justified findings, false alarms and errors missed by the verifier, whether the revision repaired or worsened the result, and how often the synthesis loses a correct partial solution or turns wrong ones into a correct one.
6. **Math verifier** (after round 1): for `reasoning.math`, the verifier first solves the task itself, only reports errors that falsify the result and states its own result (`result`). If it matches the final result of the draft, the orchestrator discards the findings and does not revise. Cross-check on 17 Oct 2026 (40 maths tasks from round 1, same setup, only the prompt differs): in `parallel_verify`, findings drop from 11 to 1 of 40, all of them false alarms (the Qwen verifiers report wrongly particularly often), and the median latency from 24.8 to 18.9 s. In `draft_verify_synthesize`, gpt-oss verifies in both runs and never reports flaws. The remaining false alarm has a deviating result and worsens the answer (39/40 instead of 40/40); the discard rule never applied. Whether real errors are now overlooked will only be shown by round 2.

## Result of round 1

Full report: [`bench/report-r1.md`](../bench/report-r1.md). Runtime just under 4 h, 800 results.

| Run | Code | Maths | Tokens/solved (code / maths) |
|---|---|---|---|
| einzel-flash (reference) | 39/40 | 40/40 | 472 / 484 |
| schwarm-draft-verify | 39/40 | 40/40 | 1776 / 1532 |
| schwarm-parallel | 37/40 | 40/40 | 2293 / 2001 |
| schwarm-parallel-verify | 40/40 | 40/40 | 3776 / 2933 |
| sc3-flash | 40/40 | 40/40 | 1051 / 1558 |

- **Ceiling effect:** the best single model solves 79 of 80 tasks; no difference is significant. The swarm hardly does harm but costs 3 to 8 times the tokens; self-consistency reaches the same rate more cheaply.
- `parallel` without a verifier makes things worse (synthesis turns correct code into wrong code), the verifier in `parallel_verify` catches that.
- The verifier probably reports flaws too often (17 times in `draft-verify`, mostly on maths drafts of a model that solves all maths tasks on its own).
- The errors of the single models hardly overlap – the precondition for a swarm advantage remains.

**For round 2:** harder tasks (goal: best single model at 50–70 %), record intermediate texts (implemented, see implementation item 5), `reasoning.math` also for the Qwen models, a more restrained verifier for maths (implemented, see implementation item 6).

## Mixed task set (hypothesis 2)

For [hypothesis 2](measurement-plan.md#hypothesis-2-team-of-small-specialists) (team of small specialists), every task combines several parts from different domains, each automatically checkable. The draft [`bench/tasks-mix-draft.jsonl`](../bench/tasks-mix-draft.jsonl) contains 10 tasks with 3 parts each (code, calculation, facts, data from a table, dates, a translation), written for this project rather than taken from public benchmarks, so that no model knows them from training. Every part was checked against a reference solution.

Format, one task per line:

```json
{"id": "mix/04", "kind": "multi", "prompt": "…", "parts": [
  {"n": 1, "kind": "number", "answer": "5.4"},
  {"n": 2, "kind": "code", "entry_point": "total_cost", "tests": "def check(f): …"},
  {"n": 3, "kind": "text", "pattern": "(?i)^\\W*melon\\W*$"}]}
```

The prompt asks for all code in a single Python block and ends with one line `Answer <n>: <value>` per part that is not code.

| Part | Assessment |
|---|---|
| `code` | the code from the answer runs with the hidden tests `check(f)` in the sandbox, as in round 1 |
| `number` | the value from `Answer <n>:` equals `answer` (relative deviation at most 10⁻⁶, or `tolerance`) |
| `text` | the value from `Answer <n>:` matches the regular expression `pattern` |

A task counts as solved if all parts are correct. `swarmainet bench run` assesses multi-part tasks this way and stores the result per part (`parts`, `part_kinds`); the report adds a table with the parts per kind and the **coverage**: tasks a run solves that no single model (direct, one sample) solves completely. Swarm runs send no skill for these tasks, the planner of `decompose` decides; self-consistency takes the first attempt, since a majority vote over several parts is not meaningful. [`bench/plan-mix.yaml`](../bench/plan-mix.yaml) contains the setups A to C of the measurement plan. A trial run of three tasks against Qwen3.8 Flash solved 9 of 9 parts: format and assessment work, but the draft is too easy for a large model.

**Still to do before the measurement:** extending the set to 30 to 100 tasks, with harder parts, so that the generalist does not solve everything (ceiling effect of round 1); parts with images (generated charts and tables with known values) and with audio (recordings with a known transcript), so that coverage can be measured; installing the specialists that are still missing (see the plan file).

## Open questions

1. **llama3.1:8b as a third family:** run it on `.95` as GGUF via llama.cpp, or is the VRAM sufficient for two loaded Ollama models (`OLLAMA_MAX_LOADED_MODELS=2`)? Until then it is a separate block with model switches (`schwarm-parallel-verify-llama` in the plan); in round 1 this raised the median latency from 24 to 39 s without a quality gain.
2. ~~Task source~~ – decided: HumanEval and GSM8K.
3. ~~Runtime~~ – decided: run overnight.
