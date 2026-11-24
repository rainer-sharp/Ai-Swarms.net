# Changelog

**English** · [Deutsch](de/CHANGELOG.md)

All notable changes to SwarmAiNet. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/); versions follow [Semantic Versioning](https://semver.org/) with the usual caveat for 0.x: incompatible changes are still possible between minor versions.

## [Unreleased]

### Added

- **Images in requests:** chat messages may contain images as `image_url` parts with an embedded data URL (PNG, JPEG, WebP, GIF, together at most 2.75 MiB), as Open WebUI sends them. They only go to models with the new setting `vision: true`, also as verifier or synthesiser; the backend receives them in the OpenAI format. With `reduced`, requests with images stay in the trusted circle, because an image cannot be pseudonymised. New optional protocol field `images` in the messages; providers with older versions never announce the modality `image` and therefore never receive images. ([usage](docs/usage.md#coding-agents-and-tools), [privacy classes](spec/privacy-classes.md) rule 10, [protocol](spec/protocol-v0.1.md) § 4.2)
- **Minimisation for `reduced`:** if a request contains a history or system instructions, an own or trusted model first rewrites the last request as a self-contained task with only what is needed; foreign nodes, also as verifier or synthesiser, receive this task, pseudonymised, instead of the conversation. Without it, nothing goes to strangers. With tools, the history stays complete. ([privacy classes](spec/privacy-classes.md) rule 9)
- **Container image with `ffmpeg`:** `docker build --target audio` builds a variant (Alpine, about 230 MB instead of 50 MB) for a gateway that splits long recordings. ([installation](docs/installation.md#continuous-operation))

### Changed

- If the request restricts the choice to agents (`X-SwarmAiNet-Agents`) that the privacy class excludes, the gateway now responds with 400 and the reason instead of 502.

## [0.5.0] – 2026-11-23

Foreign nodes can now help with personal requests too: with the privacy class `reduced`, they only see placeholders instead of names, addresses and account numbers. Pause and emergency stop now survive a restart. The protocol stays at version 0.1: nodes with 0.5.0 and 0.4.0 work together. Existing configurations keep working without changes.

### Added

- **Privacy class `reduced` with pseudonymisation:** foreign nodes now also receive `reduced` requests, but only with placeholders such as `[PERSON_1]` or `[EMAIL_1]` instead of names, organisations, places, email addresses, phone numbers, IBANs, card numbers, IP addresses and credentials in URLs; answers come back with the originals, also when streaming. Names are recognised by an own or trusted model, plus the new list `tasks.pseudonymize_terms`; without recognition, nothing goes to strangers. Own and trusted nodes still see the original; audio stays in the trusted circle. Providers accept `reduced` from strangers with `accept_public_tasks`, as for `public`; providers with older versions keep refusing. ([privacy classes](spec/privacy-classes.md) rule 9, [usage](docs/usage.md#optional-headers))

### Changed

- **Pause and emergency stop survive a restart:** `swarmainet pause` and `stop` are stored in `~/.swarmainet/control.json` and keep applying after a restart, e.g. when systemd restarts the service, until `swarmainet resume`. The start-up log and the first hint in `swarmainet status` say so, with the time since when. An unreadable control file leads to an emergency stop rather than being ignored. If the state cannot be saved, it still applies at once, but the command reports an error because it will not survive a restart. ([usage](docs/usage.md#commands))

### Fixed

- Firewall hints in `swarmainet status` and `swarmainet ping` name the ports the node actually uses (e.g. `4711/tcp and 4711/udp`) instead of always `4711/tcp`.

### Upgrade notes

- Strangers only receive `reduced` requests once they run 0.5.0 and have `accept_public_tasks` enabled; until then, such requests stay with own and trusted nodes, as before. The coordinating node needs 0.5.0 as well, plus an own or trusted model that recognises names; without one, nothing goes to strangers.
- Pseudonymisation costs one additional model call per request, but only when a foreign agent actually takes part. What must not leave the trusted circle under any circumstances belongs to `trusted`: models can miss names, and indirect hints such as roles or unusual details stay in the text.
- After updating, a node that was paused or stopped before the update starts running normally; from then on, `pause` and `stop` persist until `swarmainet resume`.

## [0.4.0] – 2026-11-23

Answers now stream, also from models on other nodes and with tools, and every release comes with ready-to-run programs for Linux, macOS and Windows. The protocol stays at version 0.1 and only gains optional fields: nodes with 0.4.0 and 0.3.0 work together. Existing configurations keep working without changes.

### Added

- **Streaming:** with `stream: true`, the answer arrives piece by piece, also from models on other nodes. Unreviewed text (draft, verdict, the model's thinking) goes into `delta.reasoning_content`, which Open WebUI shows as a collapsed thinking phase; `delta.content` only carries text after which no review follows. If the verifier finds no flaws, the reviewed draft follows as one block. With tools, the text streams live and the tool calls follow complete at the end, after the check against the allowed tools. New optional protocol fields: `stream` in the subtask and `delta`/`reasoning` in `TASK_PROGRESS`; providers with older versions answer as one block. ([usage](docs/usage.md#streaming), [protocol](spec/protocol-v0.1.md) § 4.2)
- **Prebuilt binaries:** every release from 0.4.0 contains archives for Linux, macOS and Windows (amd64 and arm64) with `SHA256SUMS`, built reproducibly by a GitHub workflow on every tag `v*` (`scripts/build-release.sh`). ([installation](docs/installation.md#prebuilt-binaries))
- **`swarmainet version`**; a running node also reports its version in `swarmainet status` and at start-up. Container images take it via the build argument `VERSION`.

### Upgrade notes

- A model on another node streams only once that node runs 0.4.0; until then its answer arrives as one block, as before. The node whose gateway receives the request needs 0.4.0 as well.
- Clients that ignore `delta.reasoning_content` do not see drafts and verdicts; they receive the answer once it starts. Open WebUI shows them as a collapsed thinking phase.
- Container images built yourself report `dev` as their version unless the version is passed in: `SWARMAINET_VERSION=$(git describe --tags --always) docker compose build`.

## [0.3.0] – 2026-11-22

Phase 3 begins: every node keeps a competence profile and can choose the pattern by bandit; recordings can be transcribed through the swarm. The protocol stays at version 0.1 and only gains the optional field `audio`: nodes with 0.3.0 and 0.2.0 work together, but only a node with 0.3.0 can provide transcription. Existing configurations keep working without changes; the new section `cooperation` is optional.

### Added

- **Transcription:** `POST /v1/audio/transcriptions` in the format of the OpenAI API has a recording transcribed by a model with the skill `audio.transcribe`, also on another node (new optional field `audio` in the subtask, at most 2.75 MiB). Audio only goes up to privacy class `trusted`; coordinator and provider both enforce this. If a backend has no `/audio/transcriptions` (e.g. vLLM with Nemotron-Transcribe), the node sends the recording via the chat as `audio_url` with thinking disabled, and removes model control tags from the transcript. Recordings over 2.75 MiB (up to 512 MiB) are split by the gateway with `ffmpeg` into overlapping pieces of 10 minutes, transcribed in parallel and joined at the longest common word sequence in each overlap. ([usage](docs/usage.md#transcription), [protocol](spec/protocol-v0.1.md) § 4.2)
- **Competence profile** (phase 3, stage 1): every node rates the agents it works with per skill (Glicko-2) and keeps a verifier statistic of hits, false alarms and misses. Verdicts of proven verifiers count as signals; feedback via `POST /v1/feedback`, display via `GET /api/profile`, storage in `~/.swarmainet/profile.json`. `swarmainet bench run` sends its assessments as objective feedback. The selection of agents does not use the profile yet. ([usage](docs/usage.md#feedback-and-competence-profile), [cooperation](spec/cooperation.md) § 2)
- `steps[].verdict` in the answer shows each verifier's verdict.
- **Pattern `cascade`** (`swarmainet/cascade`, also allowed for `tasks.pattern`): a cheap model first, review by a different family; only if flaws are found does the best other model by measured rating solve the task anew (`escalated: true`).
- **Bandit over patterns** (phase 3, stage 2, opt-in via `cooperation.selection: bandit`): for requests without a pattern, the node chooses per task category by Thompson sampling the cheapest pattern expected to reach the quality target, learning from feedback on final results. Default remains `rules`. The answer states who chose the pattern (`selected_by`). ([configuration](docs/configuration.md#choosing-the-pattern-cooperation), [cooperation](spec/cooperation.md) § 3, § 5)

### Changed

- **Agents without a matching skill** only serve as a fallback if no agent fits at all; agents with only audio skills never receive text tasks, and text generalists (`general`) never receive audio.

### Fixed

- **Reconnecting after a short disconnect:** two trusted nodes used to redial each other at the same moment. Over TCP with port reuse, this produced a simultaneous open in which the handshake failed on both sides, and both then blocked each other for several seconds (dial backoff); a task in that window failed although both were reachable. Now only the side with the smaller peer ID redials immediately, the other after 2 to 3 seconds if still needed, and a task that hits a dial backoff clears it and dials once more. ([protocol](spec/protocol-v0.1.md) § 4.1)

### Upgrade notes

- For transcription, the node with the model needs 0.3.0, as does the node whose gateway receives the recording. To split recordings over 2.75 MiB, that gateway node also needs `ffmpeg` and `ffprobe` (Debian/Ubuntu: `apt install ffmpeg`).
- On first start, the node creates `~/.swarmainet/profile.json` for the competence profile. The pattern choice only changes with `cooperation.selection: bandit`.
- The fix for reconnecting only takes full effect once both sides of a connection run 0.3.0.

## [0.2.0] – 2026-11-21

The protocol is unchanged (version 0.1): nodes with 0.2.0 and 0.1.0 work together. Existing configurations keep working without changes.

### Changed

- **`swarmainet init` now sets up a private network of your own** with a random ID `p-…` and a fresh key, instead of joining the open network. Nobody becomes visible to strangers without wanting to. The previous behaviour is available with `swarmainet init -open`. ([network types](docs/networks.md#private-network-own-devices))
- **`swarmainet join` replaces the private network created by `init`** as long as the node has never run and has no entry addresses, instead of adding a second network next to it. Networks that are already in use stay untouched.
- **Entry nodes and DNS seeds are dialled again** when the DHT routing table is empty or a node behind NAT has no relay slot, not only when no peer at all is connected. Two nodes behind NAT that only knew each other after the start node failed used to remain an island that new nodes could not find; now they reconnect to the start node within seconds of its return. Failed entry attempts are only logged at INFO level while no peer is connected. ([P2P layer](spec/p2p-layer.md) § 2)
- **`dns_seeds` also applies to the networks under `node.networks`** unless they specify their own; membership networks never ask a seed.
- **Clearer message when the privacy class excludes all agents:** for `trusted`, `reduced` or `local` without suitable agents, the error states the reason and the way out (`X-SwarmAiNet-Privacy: public`), and the gateway responds with **400** instead of 502.
- **Documentation in English and German:** the English version is now the main one; German versions are in `de/` subfolders with the same file names. Documentation files were renamed to English names, e.g. `docs/netze.md` → `docs/networks.md` (German: `docs/de/networks.md`), `spec/p2p-schicht.md` → `spec/p2p-layer.md`. Links to the old paths no longer work.
- **Overall concept** brought up to date as Markdown in both languages ([concept](docs/concept.md)), with new graphics; the original of 14 Oct 2026 is archived as a PDF.

### Added

- **QR code for invitations:** `swarmainet invite -qr` and `swarmainet member invite -qr` additionally show the link as a QR code in the terminal (new dependency `rsc.io/qr`, BSD licence).
- **Hardware test of the open network** across two sites ([P2P layer](spec/p2p-layer.md) § 15): entry solely via the DNS seed, a public task between a home network and a mobile network, hole punching over IPv6/QUIC, failure of the start node, tool rights for strangers.

### Upgrade notes

- If your own private network still uses the ID `swarmainet` (the name of the open network), rename it to an ID of its own, e.g. `p-…`, on all nodes at the same time ([P2P layer](spec/p2p-layer.md) § 1).
- Nodes behind NAT benefit from the reconnection fix only once they run 0.2.0; start nodes and helpers are not affected by it.

## [0.1.0] – 2026-11-17

First public version: local swarm (phase 1) and P2P layer with private networks and membership networks, tested on hardware across two sites.

[0.5.0]: https://github.com/rainer-sharp/Ai-Swarms.net/compare/v0.4.0...v0.5.0
[0.4.0]: https://github.com/rainer-sharp/Ai-Swarms.net/compare/v0.3.0...v0.4.0
[0.3.0]: https://github.com/rainer-sharp/Ai-Swarms.net/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/rainer-sharp/Ai-Swarms.net/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/rainer-sharp/Ai-Swarms.net/releases/tag/v0.1.0
