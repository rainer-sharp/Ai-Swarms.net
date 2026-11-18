# Measurement plan: single model versus swarm

**English** · [Deutsch](de/measurement-plan.md)

Core hypothesis: **at equal budget**, the swarm delivers higher verified quality than the best single node and than a fixed multi-model flow – or reliably recognises that a single model is sufficient. This is the gate of phase 3; if it is missed, the concept is revised before phase 4.

## Task sets

| Category | Size | Assessment |
|---|---|---|
| Code | 30–100 | unit tests in a sandbox |
| Mathematics | 30–100 | exact solution |
| Fact checking | 30–100 | ground truth, assessed blind |
| Multimodal analysis | 30–100 | ground truth (once vision agents exist) |
| Open planning | 30–100 | blind pairwise comparison by models of other families + human |

Tasks are versioned and fixed by hash before the run (commit-reveal), so that no flow can be adjusted afterwards.

## Experiment stages

1. Three heterogeneous models solve all tasks individually (baseline: best single model per category).
2. Swarm with a fixed flow: draft + verifier + synthesis (`swarmainet/draft_verify_synthesize`). Comparison variants: independent solutions with synthesis (`swarmainet/parallel`) and additionally with review of the synthesis (`swarmainet/parallel_verify`).
3. A PI controller adjusts the number of reviews dynamically.
4. Several independent coordinators in the P2P network, with failure tests and simulated manipulated assessments.

## Equal budget

Budget = GPU seconds and tokens across all participating models. A single model may use the same budget for self-consistency (solving several times, majority/self-review); otherwise the comparison would be unfair.

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
