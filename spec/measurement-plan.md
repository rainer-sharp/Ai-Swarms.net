# Measurement plan: single model versus swarm

**English** · [Deutsch](de/measurement-plan.md)

Two hypotheses, each a reason for a swarm:

1. **Quality (hypothesis 1):** **at equal budget**, the swarm delivers higher verified quality than the best single node and than a fixed multi-model flow – or reliably recognises that a single model is sufficient.
2. **Specialists (hypothesis 2):** a team of **small specialists** reaches the quality of a large generalist on mixed, multi-part tasks with clearly less hardware per device, or solves tasks that none of its members solves alone ([section below](#hypothesis-2-team-of-small-specialists)).

The gate of phase 3 is met if hypothesis 1 or hypothesis 2 holds; if both are missed, the concept is revised before phase 4.

## Task sets

| Category | Size | Assessment |
|---|---|---|
| Code | 30–100 | unit tests in a sandbox |
| Mathematics | 30–100 | exact solution |
| Fact checking | 30–100 | ground truth, assessed blind |
| Multimodal analysis | 30–100 | ground truth (once vision agents exist) |
| Open planning | 30–100 | blind pairwise comparison by models of other families + human |
| Mixed, multi-part (hypothesis 2) | 30–100 | per part: unit tests, exact value or pattern; solved if all parts are correct ([benchmark](benchmark.md#mixed-task-set-hypothesis-2)) |

Tasks are versioned and fixed by hash before the run (commit-reveal), so that no flow can be adjusted afterwards.

## Experiment stages

1. Three heterogeneous models solve all tasks individually (baseline: best single model per category).
2. Swarm with a fixed flow: draft + verifier + synthesis (`swarmainet/draft_verify_synthesize`). Comparison variants: independent solutions with synthesis (`swarmainet/parallel`) and additionally with review of the synthesis (`swarmainet/parallel_verify`).
3. A PI controller adjusts the number of reviews dynamically.
4. Several independent coordinators in the P2P network, with failure tests and simulated manipulated assessments.

## Equal budget

Budget = GPU seconds and tokens across all participating models. A single model may use the same budget for self-consistency (solving several times, majority/self-review); otherwise the comparison would be unfair.

## Hypothesis 2: team of small specialists

**Claim:** on mixed, multi-part tasks (code, calculation, facts, later images and audio in one request), a team of small specialists – each at most about 8 billion parameters and runnable on a device with at most 12 GB VRAM – is **not worse** than a large generalist (24 billion parameters or more on one device), or it solves tasks that none of its members solves alone.

**Setups,** each on the same task set:

| Setup | Flow |
|---|---|
| A · team | the specialists as a swarm with `swarmainet/decompose`: the planner splits, each part goes to the matching specialist |
| B · generalist | the large model alone (`swarmainet/single`, restricted to it with `X-SwarmAiNet-Agents`) |
| C · each specialist alone | every member alone on all tasks, to measure coverage |

**Metrics:** success rate (all parts correct) and success rate per part; **coverage** (tasks solved by A that no setup from C solves completely); peak VRAM of the largest participating device and the sum over all devices; GPU seconds, energy where measured, latency and tokens.

**Statistics:** paired comparison A against B on the same tasks. Hypothesis 2 holds if the lower bound of the 95 % bootstrap interval of the difference in success rate (A − B) lies above −5 percentage points while A needs at most half the VRAM per device, or if the coverage is significant (McNemar, A against the best setup from C).

**Prerequisites:** the task set (draft in [`bench/tasks-mix-draft.jsonl`](../bench/tasks-mix-draft.jsonl)), support for multi-part tasks in `swarmainet bench`, specialists of at least three model families without model switching. Results get better once the router and the planner use the competence profile ([cooperation](cooperation.md) § 10).

## Metrics

| Metric | Comparison | Source in the node |
|---|---|---|
| Success rate | swarm vs. best single model | test results |
| Verified correctness | share of verifiably supported statements | evidence in the result object |
| Error correlation | do independent models repeat the same errors? | individual solutions from stage 1 |
| Quality per effort | added value per GPU second, token, joule, euro | receipts (`duration_ms`, tokens) |
| Latency | time to a usable answer | `steps[].duration_ms` |
| Resilience | result despite a peer dropping out | failure test, `steps[].status` |
| Diversity | does a different kind of peer add measurable value? | ablation per family |
| Control stability | does the effort oscillate? | controller history (phase 3) |
| Privacy exposure | which data went to which identities? | receipts + privacy class |
| Barrier to participation | time and steps until the first contribution | user tests |

## Statistics

Per category a paired comparison (same tasks), McNemar test for success rates, bootstrap confidence intervals for quality per effort. An added value only counts once the 95 % interval does not contain zero.
