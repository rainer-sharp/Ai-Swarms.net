# Self-organised cooperation (phase 3)

**English** · [Deutsch](de/cooperation.md)

Status: **draft**; stages 1 and 2 (competence profile, bandit, cascade; § 2, § 3, § 5, § 11) implemented on 21 Nov 2026. The bandit is opt-in (`cooperation.selection: bandit`); by default the previous rule applies.

So far the orchestrator decides by fixed rules: it takes the pattern from the configuration or the request and the verifier by family and `strength` (a self-declaration). Phase 3 replaces this with **measured experience**:

- A **competence profile** records how good each agent actually is at each skill (§ 2).
- A **bandit** chooses the recipe, i.e. pattern and casting of the roles, per task category (§ 3).
- **PI controllers** determine how much effort is used, e.g. how many reviews (§ 4).
- New **patterns** (cascade, decompose and distribute via a task graph) extend the choice (§ 5, § 6).
- A **lease** makes the coordination of a task failure-safe (§ 7).

**Gate phase 3** is the core hypothesis from the [measurement plan](measurement-plan.md): at equal budget, the swarm delivers higher verified quality than the best single model, or it reliably recognises that a single model is sufficient. If that is missed, the concept is revised before phase 4 (§ 10).

Principles that apply to everything below:

- **Local judgement:** every node keeps its own profile from its own experience. There is no global competence value.
- **Evidence before opinion:** objectively checkable signals (tests, exact solutions) weigh more than judgements of other models, and these more than self-declarations.
- **Hard limits stay hard:** privacy class, other nodes' resource policy, budgets and delegation depth cannot be overruled by any bandit or controller.
- **Creator and verifier from different model families**, as before.

## 1. Terms

| Term | Meaning |
|---|---|
| Agent | an offered model on a node, key `(peer_id, agent_id)` |
| Skill | entry of the taxonomy, e.g. `code.python`, `reasoning.math`, `general` |
| Task category | coarse class for bandit and controller: `code`, `math`, `facts`, `vision`, `planning`, `general`; derived from the skill |
| Recipe | pattern plus casting of the roles, described by families or competence levels, not by individual agents (§ 3.1) |
| Signal | measurement of the quality of a result: test, exact solution, verifier verdict, user feedback |
| Effort | number of participating models and review rounds of a task |

## 2. Competence profile

### 2.1 Model

For every agent it works with, each node keeps a rating per skill according to **Glicko-2**: value `r`, uncertainty `RD` and volatility `σ`. New agents start with high uncertainty; their starting value can be derived from `strength` (`r₀ = 1500 + 50·(strength−5)`), so that self-declarations only shape the beginning and are quickly overwritten by measurements.

Glicko-2 instead of simple Elo, because it carries the uncertainty along. The bandit (§ 3) needs exactly this to distinguish between "bad" and "unknown".

Storage: `<data_dir>/profile.json`, local only, written atomically. Agents are keyed by peer ID and agent ID (own agents as `local/<agent>`); since every network has its own peer ID, agents of different networks are kept apart. Values of agents not observed for 90 days lose certainty (RD grows by σ per day, as provided for in Glicko-2, at most back to the starting value 350).

### 2.2 Signals and weights

| Rank | Signal | Effect | Origin |
|---|---|---|---|
| 1 | **objective**: unit tests passed, exact solution hit | match against a fixed reference (win/loss) with full weight | benchmark (`swarmainet bench`), canary tasks (§ 2.3), later code execution in the sandbox (phase 4) |
| 2 | **canary**: task with a known solution, inconspicuously interspersed | like objective | § 2.3 |
| 3 | **verifier verdict** of an agent of another family (`ok` / `issues`) | match with half weight; only if the verifier's own verifier rating is sufficient: at least 5 objectively checked verdicts, at least 60 % of them correct | every task with a verifier |
| 4 | **user feedback** on the final result (good / bad) | match with weight ¼ for all participating creators | gateway, § 8 |
| – | self-declaration (`strength`, `confidence`) | starting value only, never a signal | capability announcement |

Every match is played against a fixed reference (1500 ± 50); weights enter as fractions of a match. A rating thus says how often an agent succeeds in this skill, not how it compares to a particular other agent. A verifier verdict refers to the result it reviewed, i.e. the last successful creator step before it. Objective feedback on a step also counts for the verifiers that reviewed this step: `issues` on a correct result is a false alarm, `ok` on a wrong one a miss.

Pairwise comparisons ("A is better than B") from parallel patterns are counted as a match between the two agents if a signal of ranks 1–3 decides them.

**Against bias:**
- A verifier whose findings often turn out to be false alarms (an objective signal contradicts them) loses weight as a verifier. For this, the profile additionally keeps a **verifier rating** per agent (hit rate, false alarm rate), separate from its rating as a creator. Benchmark round 1 showed how necessary this is.
- Verifiers give their verdict before they see other verdicts (as before).
- An agent never rates itself; verifiers of the same family do not count.

### 2.3 Canary tasks

A node MAY occasionally test foreign agents with tasks of known solution, e.g. from the benchmark set. Canaries are public tasks (privacy class `public`) and look like ordinary subtasks.

- At most 5 % of the subtasks to an agent, more for new agents in the **newcomer phase** (until the RD threshold).
- Canaries consume foreign computing time: they count in the balance (receipts) like normal tasks, and a node SHOULD only send them to peers for which it computes itself or which it trusts.
- The shared benchmark pool with commit-reveal is phase 4; in phase 3 a local set is sufficient.

### 2.4 Sharing ratings

In phase 3 the profile stays local. `TRUST_ATTESTATION` (selectively shared experience, weighted with one's own trust in the sender) is provided for, but only to be implemented once manipulation tests (§ 10, stage 4) show that local learning alone is too slow.

## 3. Selection: bandit over recipes

### 3.1 Recipes

A recipe describes pattern and roles abstractly:

```
single(spec=best)
draft_verify(spec=best, verifier=other_family)
parallel(n=3, families=different) + synth(local)
parallel_verify(n=3) + synth + verifier
cascade(stage1=cheap, stage2=best, threshold=θ)
decompose(planner=local) + subtasks each with their own recipe
```

Only at execution are the roles cast with concrete agents: the creator by profile (§ 2), the verifier by verifier rating and family diversity. This keeps the recipe space small (in the order of 10 per category), even if there are many agents.

### 3.2 Selection

1. **Hard filters:** modality, privacy class, available agents and families, budget and deadline of the request. Recipes that cannot be cast with these drop out.
2. **Thompson sampling per task category:** for each recipe, the node keeps a beta distribution over the success rate (signals of ranks 1–4, weighted as in § 2.2) and the mean effort in tokens. It chooses the **cheapest recipe whose sampled success rate reaches the target value `q*` of the category**; if none reaches the target, the one with the highest sampled rate.
3. **Exploration:** a small share (default 5 %) goes to recipes or castings with new agents, so that they get ratings at all.
4. **The user's choice wins:** if the request explicitly asks for a pattern (`swarmainet/<pattern>` or header), it is used; the bandit still learns from the result.

The previous rule-based selection becomes the **prior knowledge** of the bandit and the fallback as long as there is too little data.

**Implementation (stage 2):** recipes are the patterns `single`, `cascade`, `draft_verify_synthesize`, `parallel`, `parallel_verify`; their casting stays rule-based (cascade: § 5). A pattern counts as feasible if the admitted agents can fill it: everything except `single` needs two model families. Per task category (derived from the skill: `code.*` → code, `reasoning.math` → math, otherwise general …) and pattern, the node keeps Beta(1+α, 1+β) over the success rate, α and β being the weighted successes and failures of **final results** (objective feedback weight 1, user feedback ¼; intermediate steps only feed the competence profile). The pattern from `tasks.pattern` gets one success as prior knowledge. Costs are the mean tokens per task once at least 3 tasks have been observed, before that rough values from benchmark round 1. Default target values: code 0.9, math 0.95, otherwise 0.85; exploration 5 %. The answer states who chose the pattern (`selected_by`: `request`, `rules`, `bandit`).

### 3.3 Delimitation

The bandit chooses discretely *what* is used. How much effort within a recipe (review rounds, number of parallel models) is determined by the controller (§ 4). A PID controller for selection is explicitly not intended.

## 4. Controlling the effort

### 4.1 Control loops

| Control loop | Actual value | Target | Manipulated variable |
|---|---|---|---|
| Quality (outer) | filtered success rate per category | `q*`, e.g. 0.9 | effort budget: number of models, review rounds |
| Cascade | error rate of the cheap stage | e.g. ≤ 5 % | confidence threshold `θ` for escalation |
| Latency | p90 response time per category | e.g. 20 s | parallelism, fallback timeout |
| Own load | swarm load on your own hardware | strictest limit of the resource policy | accepted jobs, slots (as in the REM so far) |

### 4.2 Control law and rules

PI controller per category:

```
u_k = u_0 + K_p·e_k + K_i·Σ e_j·Δt,   e_k = q* − q̄_k
```

- **PI instead of PID:** the quality measurement is noisy and comes with dead time.
- **Smoothing:** `q̄` as EWMA (default α = 0.1) over the signals of the category.
- **Dead band:** within ±0.02 around the target, no adjustment is made.
- **Anti-windup:** at the limit (minimum or maximum effort, no peers available), integration stops.
- **Integer manipulated variable:** `u` is mapped to whole numbers of models or review rounds, with hysteresis and a cool-down (default 20 tasks) between two changes.
- **Against oscillations in the network:** small gains, random jitter, multiplicative decrease and additive increase (AIMD).
- The controller can **never overrule the resource policy of other nodes**; "no capacity" is a limit, not a control error.

Targets and controller parameters per category are configurable (`cooperation.targets`). Their calibration is an open question (§ 12).

**Implementation (stage 3):** two of the four loops, each per category, only with `cooperation.selection: bandit`, state in the competence profile (`controls` in `GET /api/profile`):

- **Quality:** the actual value is the success rate of the final results from feedback (objective with full weight, user feedback with ¼), smoothed with α = 0.1. The manipulated variable is a **minimum effort** for the bandit, in levels 0 to 3 (`single` 0; `cascade` and `parallel` 1; `draft_verify_synthesize` 2; `parallel_verify` 3); at level 0 there is no constraint. Gains `K_p` = 2, `K_i` = 0.05, `u_0` = 0; the effort rises from 0.02 below the target and only falls from 0.05 above it, so that no limit cycle arises when no level hits the target exactly. Level changes need a hysteresis of 0.15 and a cool-down of 20 feedbacks. In addition, the controller remembers the success rate per level: it does not step down to a level that recently missed the target, and only probes it again after 1000 feedbacks.
- **Cascade:** the actual value is the error rate of cascades that were not escalated (feedback on the final result), the target `cascade_error` (default 0.05), the manipulated variable `θ` between 0 and 0.95, starting at 0 (criterion 3 off). Gains `K_p` = 0.5, `K_i` = 0.05, dead band ±0.02.
- **Not yet:** latency loop, AIMD and jitter against oscillations in the network. Simulations with artificial agents of known quality are in `internal/profile/control_test.go`: the quality loop settles at the cheapest level that reaches the target, the cascade loop holds the error rate at 5 % (without `θ`, 11 % in the simulation).

## 5. Pattern: cascade

New pattern `cascade`: first a cheap model solves the task (lowest expected cost with a sufficient rating). If the result is uncertain, the coordinator escalates to the best available model. "Uncertain" means, in this order:

1. a verifier of another family reports flaws, or
2. for `reasoning.math`, the result computed by the verifier itself deviates, or
3. the self-declared `confidence` is below `θ` (weakest criterion, only in combination).

`θ` is set by the cascade controller (§ 4.1). According to the concept, the cascade is the future default for most requests, because it only uses effort where it is needed.

**Implementation (stage 2):** available as the pattern `cascade` (model name `swarmainet/cascade`, or chosen by the bandit). Stage 1 is the candidate with the best matching skill, without a measured rating below 1650 (about 75 % success), with the lowest `strength` as an approximation of lower cost. If the verifier of another family reports flaws (in maths only with a deviating result), the best other model by measured rating, otherwise by `strength`, solves the task anew; the answer then carries `escalated: true`. **Stage 3** adds criterion 3: if the verifier reports `ok` with a `confidence` below `θ`, the coordinator escalates as well and says so in the thinking phase. The criterion uses the verifier's confidence, so it only applies in combination with the review; a missing confidence (0) does not count as uncertain.

## 6. Pattern: decompose and distribute (task graph)

### 6.1 Graph

For multi-part tasks, a **planner** (preferably local, because it sees the whole picture) creates a directed acyclic graph of subtasks:

```json
{
  "nodes": [
    { "id": "a", "skill": "code.python", "prompt": "…", "deps": [] },
    { "id": "b", "skill": "code.review", "prompt": "…", "deps": ["a"] },
    { "id": "c", "skill": "general",     "prompt": "…", "deps": ["a", "b"] }
  ],
  "synthesis": "…"
}
```

- The coordinator checks the graph: acyclic, at most `max_subtasks` nodes, only known skills, prompts within the size limit. Invalid graphs fall back to a simpler recipe.
- Subtasks without open dependencies run in parallel, each with its own recipe chosen by the bandit.
- Results of predecessors go into the prompts of successors as **data**, in delimiters as before (privacy classes rule 5).
- All subtasks share the budget of the task (tokens, deadline, subtasks) and inherit privacy class and delegation depth.
- A **synthesiser** (preferably local) merges the results at the end.

### 6.2 Errors

If a subtask fails after the usual fallback attempts, its successors are not executed. The synthesiser receives what is available and names the gap under `limitations`.

**Implementation (stage 4):** pattern `decompose`, only on explicit request (model name `swarmainet/decompose` or `tasks.pattern: decompose`). The bandit does not choose it yet, because its benefit is unmeasured.

- **Planner:** chosen like the synthesiser (own models first, skill `general`), not by the best specialist skill, because it sees the whole conversation. It receives the skills of the admitted agents. If decomposing does not pay off, it returns exactly one subtask, and the task runs without decomposition with the pattern from `tasks.pattern` (`draft_verify_synthesize` if that is `decompose` itself).
- **Checks:** unique ids, known predecessors, no cycles, prompts up to 8 KiB; unknown skills become `general`, because small models invent skills. At most 6 subtasks and at most (`max_subtasks` − 2) / 2: planner and synthesis cost one subtask each, a subtask with review about two. An invalid plan or a failed planner leads to the answer without decomposition, with a note under `limitations`.
- **Execution:** in waves, all subtasks whose predecessors are done run in parallel. Each runs as a sub-task with its own pattern (bandit for the skill of the subtask, otherwise the rule pattern); all share budget, deadline and privacy class. A subtask receives its prompt and the results of its predecessors in `<ergebnis>` delimiters as a single user message. With `reduced`, it is pseudonymised for foreign agents on its own; no shortening is needed, because the prompt is already self-contained.
- **Not decomposed:** requests with images or tools.
- **Answer:** `graph` lists the subtasks (`id`, `skill`, `deps`, `pattern`, `status` `ok`, `failed` or `skipped`; without texts). Steps carry `node` and `skill`, so that the competence profile rates them in the skill of the subtask and a verifier only judges steps of its own subtask.

Tested on hardware with one model (Qwen3.8 via vLLM): the planner split a three-part request into code, review of the code and an explanation, and left a simple question undivided. Not yet: selection by the bandit, nested decomposition, mixed modalities.

## 7. Lease coordinator

### 7.1 Purpose

Today, the node whose gateway receives the request coordinates, and the result goes back to the caller synchronously. If it fails, the task is lost. That is acceptable for short requests, but not for long tasks (task graph, many reviews) or for nodes without their own models that want to hand off coordination, like the notebook in the hardware test.

Phase 3 therefore introduces **asynchronous tasks** and a **lease**.

### 7.2 Flow

1. The requester sends the task to a **coordinator** (itself or a trusted peer that offers coordination) and receives a **job ID**.
2. The coordinator holds a **lease** with an expiry time (default 60 s) and renews it as long as it is working.
3. It stores **checkpoints**: graph, completed partial results, consumed budget. They go to exactly one **substitute coordinator**, named by the requester or chosen by the coordinator among the trusted peers. The checkpoint contains prompts and partial results; the substitute MUST therefore satisfy the same privacy class as the task (`trusted` only to trusted peers, `local` never).
4. If the lease expires without being renewed, **the substitute takes over**: it increments the lease epoch, continues at the last checkpoint and renews the lease itself.
5. The requester queries the result via the job ID, at the coordinator or at the substitute.

### 7.3 Duplicate execution

- Every lease has an **epoch**. Subtasks carry it in the payload; providers refuse subtasks of an older epoch as soon as they have seen a newer one.
- Subtasks are idempotent over `(task_id, subtask_id)`; partial results already acknowledged are not requested again (receipts are already idempotent, protocol § 4.4).
- A coordinator that returns after an interruption and sees a newer epoch aborts (`TASK_CANCEL` to its open subtasks).

### 7.4 Implementation (stage 5)

Implemented: asynchronous tasks, coordinated by your own node or by a trusted peer, with an optional substitute coordinator that takes over after a failure (steps 1 to 5 of § 7.2, epoch from § 7.3).

- **Gateway:** `POST /v1/jobs` accepts a request in the format of the chat API and responds immediately with 202 and the job ID. `GET /v1/jobs/{id}` returns status (`running`, `done`, `failed`, `canceled`) and, when done, the answer in the format of the chat API under `result`; `GET /v1/jobs` lists the own jobs. `DELETE /v1/jobs/{id}` cancels a running job, also at the coordinator, and deletes a finished one. The header `X-SwarmAiNet-Coordinator: <peer-id>` names the coordinating peer; without it, your own node coordinates.
- **Protocol `/swarmainet/job/0.1`:** one message per stream and one answer. `JOB_SUBMIT` (`job_id`, `request`: messages, pattern, privacy class, skill, agents, tools, `max_tokens`, `max_subtasks`, `deadline`, `pseudonymize_terms`, `trace`), `JOB_STATUS` (query with `job_id`) and `JOB_CANCEL`; the coordinator answers with `JOB_STATUS` while the job runs and with `JOB_RESULT` (`status`, `error`, `content`, `tool_calls`, `answer`: the metadata of the answer) once it is finished. A refusal comes as `JOB_STATUS` with `status: rejected` and the reason.
- **Who coordinates for whom:** only for peers it trusts, and only with `cooperation.coordinate_for_others: true`; the requester only sends to peers it trusts. The coordinator sees the task in plain text and applies the privacy class from **its own** view: `trusted` then means its trusted circle. In a private network both are the same; with `trusted_peers` they can differ, so only choose coordinators whose trusted circle you accept. `local` never leaves the node. Each requester only sees its own jobs.
- **Limits:** at most 4 jobs for others at the same time, 2 per peer. Deadline `tasks.job_deadline_s` (default one hour); the coordinator also caps the requester's deadline to its own.
- **Storage:** `jobs.json` in the data directory (permissions 0600, contains answers). Finished jobs stay retrievable for 24 hours, at most 200. A job without substitute that the node itself was coordinating when it stopped counts as failed after the restart; a coordinator that no longer knows a job reports it as unknown, and the requester marks it as failed.
- **Substitute coordinator, opt-in per job:** the header `X-SwarmAiNet-Substitute` names a peer or `auto` (the coordinator chooses among the peers it trusts, not the requester). Without it, there is no substitute, because a checkpoint contains the task in plain text. The substitute needs `coordinate_for_others` and MUST trust the requester, otherwise it refuses (the requester could not fetch the result there). It holds at most 8 checkpoints; `local` never gets one.
- **Protocol `/swarmainet/lease/0.1`:** `LEASE_CHECKPOINT` (job ID, requester, epoch, expiry, the task as in `JOB_SUBMIT`, for `decompose` plan and results of finished subtasks), `LEASE_RENEW` (new expiry) and `LEASE_RESULT` (the final status; the substitute then drops task and partial results, keeps the result for 24 hours and no longer takes over); the substitute answers with `LEASE_ACK` (`ok`, `taken_over`, `epoch`). The lease lasts 60 s, the coordinator renews it every 20 s and sends a new checkpoint instead when there is one; the substitute checks every 5 s. If the substitute is not reachable, the coordinator tries another one (with `auto`) or the same one again.
- **Takeover:** if the lease expires, the substitute increments the epoch and runs the task itself, for `decompose` from the plan of the checkpoint without the finished subtasks; other patterns start anew. Every subtask of a job with a substitute carries `job: {id, epoch}`; a provider refuses subtasks of an older epoch once it has seen a newer one (`veraltete lease-epoche`, "stale lease epoch"). A coordinator that is still running learns of the takeover at its next renewal, aborts and from then on refers requests to the substitute. Until then, both may work in parallel; the epoch only stops subtasks at providers that have already seen the new one, the coordinator's own models are not affected. After a takeover there is no further substitute.
- **Requester:** it learns the substitute from the coordinator's answers (`substitute`) and asks there when the coordinator does not answer or no longer knows the job. A node that is shut down does not report a result to the substitute, so that it takes over; after the restart, its own jobs refer to the substitute.
- **Not implemented:** idempotence over `(task_id, subtask_id)` (§ 7.3); unfinished subtasks run again at the substitute. Finer checkpoints than per subtask of `decompose`.

## 8. Protocol and interfaces

New messages and fields (versioning as before: new optional payload fields are compatible):

| Message / field | Purpose |
|---|---|
| `TASK_BID` (so far "from phase 3") | optional bid before acceptance: free capacity, expected runtime; the coordinator can ask several providers and take the cheapest |
| `VERIFY_REQUEST` (so far "from phase 3") | separate message for reviews instead of `TASK_PROPOSE` with `role: verifier` |
| Protocol `/swarmainet/job/0.1` | asynchronous tasks: `JOB_SUBMIT`, `JOB_STATUS`, `JOB_RESULT`, `JOB_CANCEL` (implemented, § 7.4) |
| Protocol `/swarmainet/lease/0.1` | `LEASE_CHECKPOINT`, `LEASE_RENEW`, `LEASE_RESULT` (coordinator → substitute), `LEASE_ACK` (implemented, § 7.4) |
| `ProposePayload.job` | `{id, epoch}`: job and lease epoch of a subtask (§ 7.3, implemented) |
| `TRUST_ATTESTATION` | still deferred (§ 2.4) |

Gateway:

- `POST /v1/feedback` with `{ "task_id": …, "good": true|false, "objective": false, "step": <index> }`: feedback on a task of your own. Without `objective` it is user feedback (§ 2.2, rank 4, weight ¼), with `objective: true` a checked result (rank 1, full weight), as sent by `swarmainet bench run`. `step` refers to an entry in `steps[]` of the answer; without it, the final result counts. The task must be among the last 1024 of this node; each step accepts one feedback. Responses: 204, 404 (unknown task), 409 (already given), 400 (invalid step, e.g. a verifier). Implemented in stage 1.
- With `cooperation.selection: bandit`, the model name `swarmainet` selects via the bandit (implemented in stage 2); `swarmainet/<pattern>` still forces a pattern.
- `GET /api/profile`: competence profile and controller state per category (diagnostics, "control stability" in the measurement plan). Stage 1 shows the profile: rating per agent and skill (`r`, `rd`, `sigma`, `games`) and verifier statistics (`hit`, `false_alarm`, `correct_ok`, `miss`); stage 2 adds the bandit statistics per category and pattern (`recipes`: `alpha`, `beta`, `tasks`, `tokens`); stage 3 the controllers (`controls`: per category `quality` with `level` and `levels`, `cascade` with `θ` in `output`).

Configuration (draft; implemented so far: `selection`, `exploration`, `targets` with `quality` and `cascade_error` per category, `coordinate_for_others`):

```yaml
cooperation:
  selection: bandit              # bandit | rules (previous behaviour)
  exploration: 0.05
  targets:                       # targets per category
    code:  { quality: 0.9, latency_p90_s: 60 }
    math:  { quality: 0.95 }
    general: { quality: 0.85, latency_p90_s: 20, cascade_error: 0.05 }
  canary_share: 0.05
  coordinate_for_others: false   # offer coordination for trusted peers (§ 7)
```

## 9. Privacy

- Profiles, recipe statistics and controller state are local and contain no prompts, only categories, agents and result signals.
- Checkpoints of a task (§ 7) contain prompts and partial results and are subject to the privacy class of the task.
- Asynchronous tasks (§ 7.4): the coordinator sees the task in plain text and keeps the answer for 24 hours in `jobs.json` (0600); the requester keeps its own jobs there as well. A substitute holds checkpoints only in memory and drops task and partial results as soon as the result arrives.
- Canaries are public tasks without any relation to user data.

## 10. Gate and measurement

The gate is the core hypothesis (measurement plan). It is tested with the experiment stages of the measurement plan:

| Stage | Contents | Precondition |
|---|---|---|
| 1–2 | single models and fixed patterns | benchmark round 1 (done, ceiling effect) |
| 3 | bandit and PI controller against fixed patterns and against the best single model with self-consistency at equal budget | implementation of § 2–5, harder tasks, **at least three different model families without model switching** |
| 4 | several coordinators, failure of a coordinator, simulated manipulated ratings (an agent deliberately delivers bad results or false alarms) | implementation of § 6–7 |

Metrics in addition to the measurement plan: convergence of the bandit (how many tasks until a stable choice), control stability (oscillation of the effort), share of exploration, time to detect a manipulating agent.

**Note on the status:** with the models currently available (two families without model switching), stage 3 is not meaningful. The mechanisms can still be built and tested with simulations (artificial agents with known quality); measuring the gate follows as soon as more diverse models are available.

## 11. Implementation in stages

1. ✅ **Competence profile and signals:** Glicko-2 per agent and skill, verifier rating, signals from benchmark, verifier verdict and `POST /v1/feedback`; display via `GET /api/profile`. Testable with artificial agents of known quality. Implemented in `internal/profile`; prior knowledge from `strength`, verifier verdicts per step (`steps[].verdict`). Not yet: pairwise comparisons from parallel patterns.
2. ✅ **Bandit over recipes** with today's selection as prior knowledge, and the pattern **cascade**. Implemented with fixed rules for "uncertain" (§ 5); opt-in via `cooperation.selection: bandit`.
3. ✅ **PI controller** for the review effort per category, and the controller for `θ` with criterion 3 of the cascade (§ 4.2, § 5). Not yet: latency loop.
4. ✅ **Task graph:** planner, decompose and distribute, synthesis. Pattern `decompose`, only on request (§ 6). Not yet: selection by the bandit, nested decomposition.
5. ✅ **Asynchronous tasks and lease coordinator**, failure tests (§ 7.4): failure and return of the coordinator over three nodes in tests, failure on hardware with real lease times. Not yet: idempotence over subtask IDs.
6. **Canary tasks** and manipulation tests.

Every stage can be switched off with `cooperation.selection: rules`, so that the previous behaviour is kept for comparison.

## 12. Open questions

1. **Calibration:** how are targets and controller parameters per category determined? Proposal: starting values from the benchmark, then adjust during stage 3.
2. **Verifiers without ground truth:** in everyday use an objective signal is usually missing. How much may the verifier verdict alone shift the profile without verifier misjudgements becoming entrenched?
3. **Recipe space:** which recipes belong in the starting set per category, and when is a new recipe added?
4. **Coordination for others:** may a node also coordinate for untrusted peers (open network)? It then sees the whole picture of foreign tasks.
5. From the overall concept: from when on is an additional peer worthwhile for quality reasons and not only for capacity?
