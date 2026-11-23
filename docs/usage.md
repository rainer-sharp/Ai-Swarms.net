# Usage

**English** · [Deutsch](de/usage.md)

Every node provides an OpenAI-compatible API and a status page at `http://127.0.0.1:7711`. The gateway listens on localhost only. Any application that knows an OpenAI endpoint can use the swarm: Open WebUI, an IDE or a coding agent. Enter `http://127.0.0.1:7711/v1` as the endpoint; the gateway does not need an API key.

## Model names: how the swarm works

The model name selects the pattern by which the models work together:

| Model name | Pattern |
|---|---|
| `swarmainet` | Default from `tasks.pattern` (draft + verifier + synthesis) |
| `swarmainet/single` | Best specialist, the next one if it fails |
| `swarmainet/parallel` | Up to three models of different families independently, synthesis locally |
| `swarmainet/draft_verify_synthesize` | Draft, review by a different family, synthesis only if flaws are found |
| `swarmainet/parallel_verify` | Like `parallel`, then the synthesis is reviewed by a different family, revision only if flaws are found |
| `swarmainet/cascade` | A cheap model first, review by a different family; only if flaws are found does the best other model solve the task anew |

With `cooperation.selection: bandit`, the plain model name `swarmainet` lets the node choose the pattern per task category from its [competence profile](#feedback-and-competence-profile) instead of using `tasks.pattern`.

```sh
curl -s http://127.0.0.1:7711/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -H 'X-SwarmAiNet-Privacy: trusted' \
  -d '{"model":"swarmainet","messages":[{"role":"user","content":"Write a Python function for prime numbers"}]}'
```

The response additionally contains the field `swarmainet` with the procedure, the review verdict, the flaws found and limitations. `steps[]` lists agent, peer, duration and tokens for each step. `selected_by` says who chose the pattern (`request`, `rules` or `bandit`), `escalated: true` that the cascade escalated.

## Streaming

With `stream: true`, the answer arrives piece by piece as Server-Sent Events in the OpenAI format. Anything not yet reviewed goes into `delta.reasoning_content`, which Open WebUI shows collapsed as the thinking phase: a heading per step (draft, review, revision, synthesis, escalation) with the model, the text of the draft, the verdict and the thinking of the model that writes the answer. `delta.content` only carries text after which no review follows: with `single` straight away, with `draft_verify_synthesize`, `parallel_verify` and `cascade` the revision or escalation. If the verifier finds no flaws, the reviewed draft follows as one block.

If a step fails after it has already streamed text, a separator follows ("Diese Fassung wurde verworfen; es folgt eine neue.") and then the new version. The last chunk carries `finish_reason` and the field `swarmainet`. Errors before the first piece are ordinary HTTP errors (e.g. 400); after it, they appear in the text. With tools, the text streams live as well; the tool calls arrive complete in the last piece, only after the node has checked that the model calls nothing but allowed tools. Models on nodes with version 0.3.0 or older answer as one block.

## Optional headers

| Header | Meaning |
|---|---|
| `X-SwarmAiNet-Privacy` | Privacy class `local`, `trusted`, `reduced` or `public`: who may see the request ([privacy classes](../spec/privacy-classes.md)) |
| `X-SwarmAiNet-Skill` | Skill, e.g. `code.python`; if omitted, the router decides |
| `X-SwarmAiNet-Agents` | Comma-separated agent IDs to which the selection is restricted (for measurements) |
| `X-SwarmAiNet-Trace: 1` | Texts of the intermediate steps in `steps[].output` |

Without the header, `tasks.privacy_class` from the configuration applies (default `trusted`): the request only goes to your own or trusted devices. Only what is marked `public` goes to foreign nodes in the open network. If the privacy class excludes all known agents, the gateway responds with 400 and states the reason, e.g. for `trusted` without your own model and without trusted nodes.

## Open WebUI

1. *Settings → Connections → OpenAI API*: URL `http://127.0.0.1:7711/v1`, any key.
2. The models `swarmainet`, `swarmainet/single` etc. appear in the model selection.
3. For tools (e.g. Open Terminal): in the model's *Advanced Params*, set **Function Calling to "Native"**.

If Open WebUI runs in Docker, it only reaches the computer's localhost with host networking (`network_mode: host`) or via `host.docker.internal` with a forwarding. The gateway itself stays on localhost.

## Coding agents and tools

Requests with `tools` (e.g. from pi or Open WebUI with function calling "Native") go to exactly one model that has `tools: true` in the configuration; the pattern is always `single`, regardless of the model name. The response contains `tool_calls` and `finish_reason: "tool_calls"`, also when streaming.

The tools are executed by the **application itself**, i.e. Open WebUI or the agent on your computer, not by the model. But every result goes back to the model. That is why `allowed_tools` defines per network which tools its models may see:

| Network | without `allowed_tools` | with a list |
|---|---|---|
| own node | all | all |
| private network (PSK) | all | only the list |
| membership network | none | only the list |
| open network, foreign nodes | none | only the list |
| `trusted_peers` | all | all |

If no agent with the required permission is found, the gateway responds with 400. If a model calls a tool that was not shown to it, the response is discarded. Details: [privacy classes, rule 6](../spec/privacy-classes.md).

Content given as a list of text parts is joined. Images in chat messages are not supported yet (400).

## Transcription

`POST /v1/audio/transcriptions` accepts a recording in the format of the OpenAI transcription API (`multipart/form-data` with `file`, optionally `language`, `prompt`, `response_format` `json` or `text`) and has it transcribed by a model with the skill `audio.transcribe`, e.g. on another node of your own network. The node providing the model calls `/audio/transcriptions` of its backend (e.g. Whisper); if that does not exist, it sends the recording via the chat as `audio_url` with thinking disabled (e.g. vLLM with Nemotron-Transcribe). The field `model` is ignored; the swarm chooses the model. Audio only goes to your own or trusted devices, never into the open network (`X-SwarmAiNet-Privacy: public` gives 400).

```sh
curl -s http://127.0.0.1:7711/v1/audio/transcriptions -F file=@aufnahme.wav -F language=de
```

One subtask carries at most 2.75 MiB. The gateway splits longer recordings (up to 512 MiB) itself: into pieces of 10 minutes with 1 minute of overlap, as mono MP3 at 16 kHz. It sends up to four pieces through the swarm at the same time and joins the transcripts; within each overlap it looks for the longest common sequence of words and cuts in its middle, so no words are lost or doubled. If a provider stays busy, the piece waits and is sent again; if a piece fails, the whole request fails with the cause. Splitting needs `ffmpeg` and `ffprobe` on the node with the gateway (Debian/Ubuntu: `apt install ffmpeg`); without them, recordings over 2.75 MiB give 413. With `response_format=verbose_json`, the field `swarmainet` contains `duration_s`, `chunks` and the procedure of every piece in `parts[]`.

In Open WebUI: *Admin Settings → Audio → Speech-to-Text*, engine *OpenAI*, URL `http://127.0.0.1:7711/v1`, any key and model.

A transcription model is entered like any other, only with its own skill, so that it never receives text questions:

```yaml
- name: nvidia/NVIDIA-Nemotron-3.5-Transcribe-30B-A4B-NVFP4
  family: nemotron
  skills: [audio.transcribe]
  offer: true
```

## Feedback and competence profile

Every node keeps a competence profile of the agents it works with: a Glicko-2 rating per agent and skill, plus a verifier statistic (how often a verifier's `issues` or `ok` turned out to be right). It only collects data for now; the selection of agents does not use it yet ([cooperation](../spec/cooperation.md) § 2).

Feedback on an answer uses its `task_id` from the field `swarmainet`:

```sh
curl -s http://127.0.0.1:7711/v1/feedback -H 'Content-Type: application/json' \
  -d '{"task_id":"<task_id>","good":true}'
```

| Field | Meaning |
|---|---|
| `good` | the result was good (`true`) or not (`false`) |
| `objective` | `true` for a checked result (tests, exact solution): full weight; otherwise user feedback with weight ¼ |
| `step` | index in `steps[]`, e.g. to rate a draft separately; without it, the final result counts |

The response is 204, or 404 for an unknown task, 409 if feedback was already given for this step, 400 for an invalid step. `swarmainet bench run` sends its assessments as objective feedback automatically. `GET http://127.0.0.1:7711/api/profile` shows the profile; it is stored in `~/.swarmainet/profile.json`. In the answer, `steps[].verdict` shows each verifier's verdict.

## Commands

| Command | Purpose |
|---|---|
| `init [-open]` | Create configuration and keys, by default with a private network of your own; `-open` for the open network |
| `run` | Start the node |
| `status` | Status of the running node (JSON) |
| `pause`, `resume`, `stop` | Pause or resume foreign tasks, emergency stop (aborts running foreign tasks); pause and emergency stop also apply after a restart until `resume` |
| `ping [-net ID] <peer>` | Check the path to a peer |
| `psk FILE` | Create a key for a private network |
| `invite [-with-key] [-lan] [-peer ADDR] [-net ID] [-qr]` | Create an invitation link (the node must be running), with `-qr` also as a QR code |
| `join [-key FILE] <link>` | Join a network from an invitation |
| `network create` | Create a membership network with a root key |
| `member invite\|request\|issue\|install\|revoke\|import-crl\|list` | Admit members and manage certificates |
| `tracker [-listen ADDR] [-tls-cert F -tls-key F] [-trust-proxy]` | Run an address mailbox |
| `bench run\|report\|hash` | Benchmark, see [`bench/`](../bench/README.md) |
| `version` | Show the version |

All commands accept `-config PATH` (default `~/.swarmainet/config.yaml`) and `-v` for verbose logs; options come before the other arguments. The program's own messages and help texts are in German.

## Diagnostics

- **`swarmainet status`:** per network (`networks[]`) peer ID, addresses, `reachability` (open network), relay service and reservations, DHT mode and size, published capabilities and `hints` with plain-text advice; per peer network, role and `via: direct|relay`.
- **`swarmainet ping <peer-id>`** (or `/ip4/…/tcp/4711/p2p/<peer-id>`, `-net` for a specific network): known addresses, DHT lookup, connection setup directly or via relay (with a short wait for hole punching), round-trip time, offered agents; on failure with hints on firewall, network key or missing relay.
- **Status page** at `http://127.0.0.1:7711`: resources, own agents, connected nodes, own addresses and receipts; a peer can also be dialled manually there.
- **"kein passender agent verfügbar"** (no suitable agent available): no connected model has the requested skill and privacy class, or all are currently declining (time window, load). `status` shows which agents are known. If the privacy class is the reason, the message says so.
