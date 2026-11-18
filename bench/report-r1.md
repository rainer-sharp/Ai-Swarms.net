# Benchmark round 1: result

**English** · [Deutsch](de/report-r1.md)

Run of 17 Oct 2026, 17:39–21:28, on the test setup from [`spec/benchmark.md`](../spec/benchmark.md) (coordinator VM, vLLM on `.94`, llama.cpp and Ollama on `.95`). Task set `tasks-r1.jsonl` (SHA-256 `ad80b86d…2cc841`), plan `plan-r1.yaml`, raw data `results-r1.jsonl` (800 results, 1 error).

Generated with `./swarmainet bench report bench/results-r1.jsonl` (the program writes the report in German; the tables below are translated); the reference is `einzel-flash`. Run names are IDs from the plan (`einzel` = single model, `schwarm` = swarm).

## Assessment

1. **Ceiling effect.** On its own, flash only fails at HumanEval/124. All differences are within ±1–3 tasks, no McNemar test is significant (smallest p = 0.625). Round 1 shows: the swarm hardly does harm but costs 3 to 8 times the tokens.
2. **The lead of `parallel_verify` (40/40 code) is chance:** it rests solely on HumanEval/124, which qwen27b, gpt-oss, `schwarm-single` and `sc3-flash` also solve.
3. **Self-consistency is cheaper at the same quality:** `sc3-flash` reaches 100 % with about a third of the tokens of `parallel_verify`.
4. **`parallel` without a verifier makes things worse:** it fails at HumanEval/116, /132 and /140, which flash solves on its own. The synthesis of two solutions turns correct code into wrong code; the verifier in `parallel_verify` catches that.
5. **HumanEval/132** is the only difficult task: qwen27b, gpt-oss, llama individually as well as `draft-verify`, `parallel` and `parallel-verify-llama` fail, flash, `sc3-flash`, `schwarm-single` and `parallel_verify` solve it.
6. **Verifier probably too critical:** in `draft-verify` 17 times "issues", 14 of them on maths drafts by gpt-oss, which solves all 40 maths tasks on its own. Not provable, because the intermediate texts are not stored.
7. **Ollama model switches** (llama3.1 as a third family) raise the median latency from 24 to 39 s without a quality gain.
8. **The errors of the single models hardly overlap** – a precondition for a swarm advantage, but not exploitable with these tasks.
9. **Distribution:** in `schwarm-single`, all maths tasks end up at gpt-oss, because only this model offers the skill `reasoning.math` (node configuration).

## Conclusions for round 2

- Harder tasks on which the best single model solves about 50–70 % (e.g. HumanEval+ from EvalPlus, LiveCodeBench, MATH level 5), about 80 per type.
- Record intermediate results (draft, verifier findings, synthesis) to measure hit rate and false alarms of the verifier.
- Offer `reasoning.math` for the Qwen models too.
- Have the verifier report "issues" in maths only for a demonstrable calculation or logic error.

## Report

### code

| Run | solved | rate | errors | tokens/solved (95 %) | median s | Δ to reference (95 %) | only ref / only run | p (McNemar) |
|---|---|---|---|---|---|---|---|---|
| einzel-flash | 39/40 | 98 % | 0 | 472 [395; 566] | 4.9 | reference | – | – |
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

| Run | solved | rate | errors | tokens/solved (95 %) | median s | Δ to reference (95 %) | only ref / only run | p (McNemar) |
|---|---|---|---|---|---|---|---|---|
| einzel-flash | 40/40 | 100 % | 0 | 484 [437; 533] | 8.5 | reference | – | – |
| einzel-qwen27b | 39/40 | 98 % | 0 | 599 [458; 860] | 12.1 | -2.5 pp [-7.5; +0.0] | 1 / 0 | 1.000 |
| einzel-llama31 | 36/40 | 90 % | 0 | 329 [293; 375] | 4.4 | -10.0 pp [-20.0; -2.5] | 4 / 0 | 0.125 |
| einzel-gptoss | 40/40 | 100 % | 0 | 498 [438; 561] | 4.9 | +0.0 pp [+0.0; +0.0] | 0 / 0 | 1.000 |
| schwarm-single | 40/40 | 100 % | 0 | 537 [454; 633] | 5.2 | +0.0 pp [+0.0; +0.0] | 0 / 0 | 1.000 |
| schwarm-draft-verify | 40/40 | 100 % | 0 | 1532 [1266; 1848] | 7.9 | +0.0 pp [+0.0; +0.0] | 0 / 0 | 1.000 |
| schwarm-parallel | 40/40 | 100 % | 0 | 2001 [1803; 2201] | 18.4 | +0.0 pp [+0.0; +0.0] | 0 / 0 | 1.000 |
| schwarm-parallel-verify | 40/40 | 100 % | 0 | 2933 [2662; 3207] | 21.9 | +0.0 pp [+0.0; +0.0] | 0 / 0 | 1.000 |
| schwarm-parallel-verify-llama | 40/40 | 100 % | 0 | 3502 [3230; 3782] | 37.0 | +0.0 pp [+0.0; +0.0] | 0 / 0 | 1.000 |
| sc3-flash | 40/40 | 100 % | 0 | 1558 [1399; 1724] | 26.5 | +0.0 pp [+0.0; +0.0] | 0 / 0 | 1.000 |

#### Error overlap of the single models, code (shared failures / failures of at least one)

| | einzel-flash | einzel-qwen27b | einzel-llama31 | einzel-gptoss |
|---|---|---|---|---|
| einzel-flash | – | 0.00 | 0.07 | 0.00 |
| einzel-qwen27b | 0.00 | – | 0.07 | 0.20 |
| einzel-llama31 | 0.07 | 0.07 | – | 0.11 |
| einzel-gptoss | 0.00 | 0.20 | 0.11 | – |

#### Error overlap of the single models, math (shared failures / failures of at least one)

| | einzel-flash | einzel-qwen27b | einzel-llama31 | einzel-gptoss |
|---|---|---|---|---|
| einzel-flash | – | 0.00 | 0.00 | – |
| einzel-qwen27b | 0.00 | – | 0.00 | 0.00 |
| einzel-llama31 | 0.00 | 0.00 | – | 0.00 |
| einzel-gptoss | – | 0.00 | 0.00 | – |
