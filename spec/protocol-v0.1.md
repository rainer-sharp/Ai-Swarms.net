# SwarmAiNet protocol v0.1

**English** · [Deutsch](de/protocol-v0.1.md)

Status: draft, phase 0/1. Reference implementation: `internal/protocol`, `internal/p2p`.
The keywords MUST, SHOULD and MAY are normative.

## 1. Identity

- Every node has a locally generated Ed25519 key pair. The peer ID is the libp2p peer ID of this key and stays the same when the IP changes.
- There are no accounts and no central registration.

## 2. Transport

| Protocol ID | Purpose |
|---|---|
| `/swarmainet/capability/0.1` | Fetching a peer's signed capability announcement |
| `/swarmainet/task/0.1` | One subtask per stream: offer, acceptance, result, receipt |
| `/swarmainet/pex/0.1` | Peer exchange (phase 2, see [P2P layer](p2p-layer.md) § 2.4) |

- Framing: exactly one JSON message (envelope) per line, terminated with `\n`, at most 4 MiB.
- mDNS service name: `swarmainet-<network_id>`. Private swarms additionally use a libp2p PSK (then TCP only).
- Inference backend and management port listen on `localhost` only; only the P2P port faces outwards.
- From phase 2 (draft, see [P2P layer](p2p-layer.md)): Kademlia DHT `/swarmainet/kad/1.0.0` (open network) or `/swarmainet/<network_id>/kad/1.0.0` (private network), AutoNAT v2, DCUtR and Circuit Relay v2. The protocols above MUST also be offered over relay connections (limited connections).

## 3. Envelope

```json
{
  "protocol": "/swarmainet/0.1",
  "type": "TASK_PROPOSE",
  "task_id": "<id>",
  "sender": "<peer-id>",
  "expires_at": "2026-10-14T12:05:00Z",
  "nonce": "<128 bit hex>",
  "payload": { },
  "signature": "<base64>"
}
```

**Signature:** Ed25519 over

```
"swarmainet/sig/v0.1\n" + protocol + "\n" + type + "\n" + task_id + "\n" + sender + "\n"
+ expires_at (RFC 3339, UTC, nanoseconds without trailing zeros) + "\n" + nonce + "\n" + hex(sha256(payload-bytes)) + "\n"
```

`payload-bytes` are the bytes of the payload exactly as they appear on the wire (compact JSON).

Receivers MUST discard the message if
1. `protocol` does not match,
2. `expires_at` has been reached,
3. the signature does not match the key in `sender`,
4. `sender` is not the remote end of the connection,
5. the pair (`sender`, `nonce`) has already been seen before expiry (replay).

**Extensibility:** unknown fields in the payload MUST be ignored. New header fields only come with a new protocol version; extensions belong in the payload so that the signature stays stable.

## 4. Messages

| Type | Direction | Payload |
|---|---|---|
| `CAPABILITY_ANNOUNCE` | provider → anyone | `Capability` |
| `TASK_PROPOSE` | coordinator → provider | `ProposePayload` |
| `TASK_BID` | provider → coordinator | `BidPayload` (from phase 3) |
| `TASK_ACCEPT` | provider → coordinator | `subtask_id`, `lease_expiry` |
| `TASK_REJECT` | provider → coordinator | `subtask_id`, `reason` (worded generally) |
| `TASK_PROGRESS` | provider → coordinator | status or token stream |
| `TASK_RESULT` | provider → coordinator | `Result` |
| `VERIFY_REQUEST` | coordinator → verifier | from phase 3, until then `TASK_PROPOSE` with `role: verifier` |
| `TASK_CANCEL` | both | `subtask_id`, `reason` |
| `TASK_RECEIPT` | both | `Receipt` (first from the coordinator, then countersigned back) |
| `TRUST_ATTESTATION` | anyone | from phase 3 |

`TASK_REJECT` has been added compared with the overall concept: a refusal is a terminal state of its own and needs a message of its own.

### 4.1 Capability

```json
{
  "peer_id": "<peer-id>",
  "expires_at": "2026-10-14T12:05:00Z",
  "agents": [{ "agent_id": "ollama/qwen2.5-coder:7b", "model_family": "qwen",
               "skills": ["code.python", "code.review"], "modalities": ["text"], "context_max": 32768,
               "strength": 5, "tools": true }],
  "availability": { "slots_now": 1, "windows": [] },
  "policy": { "accepts": ["trusted"], "retention": "no-prompt-storage" }
}
```

`strength` (optional, 1–10, missing = unknown) is the provider's self-assessment. Coordinators only use it to order equally suitable agents, e.g. when choosing the verifier; it is not verified (see threat model).

`tools` (optional, missing = false) indicates that the model can handle tool calls in the OpenAI format (configuration `models[].tools: true`).

The announcement contains no prompts, internal IPs, hardware or energy data and not the reason for availability. `peer_id` MUST match the envelope sender. TTL: 5 minutes, fetched when a connection is established and every minute. If the last connection to a peer breaks, its announcement remains valid for another 30 seconds (after that it is discarded, even before the TTL expires), and trusted peers are redialled: the side with the smaller peer ID immediately, the other only after 2 to 3 seconds and only if no connection exists by then. If both dialled at the same time, TCP with port reuse would produce a simultaneous open in which the handshake fails on both sides. A subtask that hits libp2p's dial backoff clears it for this peer and dials once more.

### 4.2 ProposePayload

```json
{
  "subtask_id": "<id>", "role": "specialist|verifier|synthesizer|planner|router|tool",
  "skill": "code.python", "agent_id": "<agent>",
  "messages": [{ "role": "user", "content": "..." }],
  "limits": { "deadline": "...", "max_depth": 1, "depth": 1, "max_subtasks": 8,
              "max_tokens": 2048, "max_bytes": 0, "privacy_class": "trusted" },
  "json_schema": "<optional>",
  "tools": [ ... ], "tool_choice": "auto"
}
```

`messages[]` follows the OpenAI format including `tool_calls` (assistant), `tool_call_id` and `name` (role `tool`). `tools` and `tool_choice` (optional) are the tool descriptions of the calling application and are passed to the model unchanged. The provider does not execute tools; it only returns the calls, which are executed at the requester. Which tools an agent may see is therefore decided by the requester ([privacy classes](privacy-classes.md), rule 6). A provider MUST refuse a task with `tools` if the chosen agent does not announce `tools` (reason `agent beherrscht keine werkzeuge`, "agent does not support tools").

`audio` (optional) carries a recording for a transcription: `{ "data": <base64>, "mime", "filename", "language", "prompt" }`, at most 2.75 MiB of audio (`MaxAudioBytes`), so that the message stays below 4 MiB. Such a subtask has the skill `audio.transcribe`, the result is the transcript in `result.content`. Voices are personal data: a coordinator MUST NOT send audio with privacy class `public`, and a provider MUST refuse it (`audio nur im vertrauten kreis`, "audio only within the trusted circle"), as well as audio to an agent without `audio.transcribe` (`agent verarbeitet kein audio`) and empty or too large recordings (`aufnahme leer oder zu groß`). The provider passes the recording to its backend's `/audio/transcriptions`, or, if the backend has no such endpoint, to the chat as `audio_url`. A coordinator splits longer recordings into overlapping pieces, each its own subtask, and joins the transcripts itself.

`depth` counts the delegations so far (1 at the first receiver). A provider MUST refuse if `depth > max_depth`. Further delegations inherit equal or stricter limits and consume the same budget.

### 4.3 Result

As in the overall concept: `task_id`, `subtask_id`, `producer_peer`, `role`, `result {status, summary, content}`, `evidence[]`, `confidence {value, kind}`, `limitations[]`, `usage`. For tasks with tools, `result` additionally contains `tool_calls[]` (`id`, `type: "function"`, `function {name, arguments}`); `content` may then be empty. `confidence.kind = "self-reported"` is not a calibrated probability; `evidence.digest` proves that something is unchanged, not that it is correct.

### 4.4 Receipt

Fields: `task_id`, `subtask_id`, `coordinator`, `provider`, `role`, `agent_id`, `model_family`, `status`, `finished_at`, `duration_ms`, `prompt_tokens`, `completion_tokens`, `result_digest`, `coordinator_sig`, `provider_sig`.
What is signed is `"swarmainet/receipt/v0.1\n" + JSON(receipt without signatures)`. The provider only countersigns if the digest and token counts match its work. Receipts are idempotent over (`task_id`, `subtask_id`, `provider`).

## 5. Flow of a subtask

```
Coordinator                         Provider
  TASK_PROPOSE  ───────────────────▶  checks privacy class, trust, deadline, depth, REM
               ◀───────────────────  TASK_ACCEPT (lease) | TASK_REJECT
               ◀───────────────────  TASK_PROGRESS*
               ◀───────────────────  TASK_RESULT | TASK_CANCEL
  TASK_RECEIPT (signed) ───────────▶ checks, countersigns
               ◀───────────────────  TASK_RECEIPT (signed by both)
```

If the coordinator aborts, it sends `TASK_CANCEL` and resets the stream; the provider aborts the inference. If the stream ends before the result, the subtask counts as aborted.

## 6. States

`CREATED → DISCOVERING → OFFERED → ACCEPTED → RUNNING → REVIEWING → COMPLETED`;
`OFFERED → REJECTED`; from every non-terminal state `CANCELLED`, `EXPIRED`, `FAILED`.
A transition into the same state is allowed (idempotent repetition).

## 7. Provider acceptance rules (phase 1)

| Privacy class | trusted peer | other peer |
|---|---|---|
| `local` | refuse | refuse |
| `trusted` | accept* | refuse |
| `reduced` | accept* | refuse |
| `public` | accept* | only with `accept_public_tasks: true`* |

\* provided the resource manager agrees. If it refuses for lack of capacity (reason `keine kapazität`, "no capacity") or the rate limit applies (`busy`), the coordinator tries other candidates and, if all are busy, retries with increasing intervals (1 s to 8 s, at most 2 minutes or until the deadline). A peer is trusted if its key is listed under `trusted_peers`, if it is a participant of a private swarm (PSK) or if it is an admitted peer with role `member` or `issuer` in a [membership network](membership.md).
