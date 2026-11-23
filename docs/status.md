# Development status

**English** · [Deutsch](de/status.md)

SwarmAiNet is a **research prototype**. The network layer is more advanced than the collaboration of the models: selection so far follows fixed rules, and a quality advantage of the swarm over the best single model has not yet been shown (see [Benchmark](#benchmark)).

## Phases

| Phase | Contents | Status |
|---|---|---|
| 0 · Specification | Protocol, privacy classes, threat model, measurement plan | Draft v0.1 in [`spec/`](../spec/README.md) |
| 1 · Local swarm | mDNS, static peers, OpenAI backends, resource manager, patterns, load balancing, model strength, receipts | **completed**, tested on real hardware: 3 devices, 3 model families, Ollama, llama.cpp and vLLM |
| 2 · P2P layer | DHT, entry sources, AutoNAT, hole punching, relay; private, membership and open networks, also simultaneously | **private network, membership network and open network confirmed on hardware across two sites** |
| 3 · Self-organised cooperation | Competence profile, bandit, PI controller, task graph, lease coordinator | Specification draft: [`spec/cooperation.md`](../spec/cooperation.md); **stages 1 and 2 implemented** (competence profile, bandit over patterns as opt-in, cascade) |
| 4 · Open test network | One-click client, credit, sandbox, benchmark pool | open |

## Testing on hardware

- **Local swarm:** *"Two models per task, a peer failure does not block"* – covered by `internal/node/node_test.go` and confirmed in operation with three devices, including fallback on refusals and backend errors.
- **Private network across sites:** a notebook on a mobile network (CGNAT) and a VM behind a FritzBox without port forwarding found each other via a helper on another connection. A task ran via the relay, and after the helper failed all nodes reconnected on their own. Details: [`spec/p2p-layer.md`](../spec/p2p-layer.md) § 14.
- **Membership network across sites:** issuer in the home network, helper on a VPS, new member on a mobile router. Joining by invitation and a task via the relay succeeded; a revoked member lost its admission after 0.4 s. Details: [`spec/membership.md`](../spec/membership.md) § 11.
- **Open network across sites:** a VM behind a FritzBox and a notebook on a mobile network found each other solely via the DNS seed and a start node on a VPS, without an invitation. A public task ran, a confidential one stayed on the notebook. Hole punching over IPv6/QUIC succeeded, the connection ran directly. Foreign models only saw the tools allowed with `allowed_tools`. Details: [`spec/p2p-layer.md`](../spec/p2p-layer.md) § 15.
- **Tools:** Open WebUI with Open Terminal via the swarm, tool calls and streaming.

## Benchmark

Round 1 (80 tasks from HumanEval and GSM8K, [report](../bench/report-r1.md)): the swarm hardly does harm but costs 3 to 8 times the tokens. The tasks were too easy for the best single model, no difference is significant. Round 2 with harder tasks and more diverse models is pending.

## Known gaps

**Collaboration of the models**

- The router is a keyword heuristic, not a model; bandit and competence profile follow in phase 3.
- Privacy class `reduced` without pseudonymisation; until then it is treated like `trusted`.
- Streaming shows the answer live only where no further review follows; with review, the draft streams into the thinking phase.
- Images in requests are not supported. Audio only as a transcription (`/v1/audio/transcriptions`, only up to `trusted`); the gateway splits long recordings into overlapping pieces with `ffmpeg`. Tested on hardware: a 57-minute recording in a single request, seven pieces from the VM via a node with Nemotron-Transcribe in 143 s, without doubled or lost words at the seams. The pieces run in parallel, but the gain depends on how many providers are free; with only one model, they run one after another.

**Resources**

- `vram_budget_gib` and `extra_power_max_w` are not enforced yet; GPU measurement only via `nvidia-smi` (ROCm and macOS to follow).
- Energy source only Home Assistant (REST, read-only); MQTT and Modbus/SunSpec to follow.
- Pause and emergency stop apply until the next restart.

**Network**

- Not yet tested on hardware: the bridge node via VPN, Windows and macOS as platforms, membership networks in continuous operation over weeks.
- In the hardware test, hole punching succeeded over IPv6/QUIC but not over IPv4 behind mobile CGNAT or over TCP; there, connections between sites run permanently via a helper.
- Port forwarding only via UPnP and NAT-PMP for IPv4, not via PCP; IPv6 forwardings must be set manually in the router. `port_mapping` has only been tested with a fake NAT.
- WebTransport/WebSocket over port 443 are missing; in networks that only allow web traffic, a node cannot get out.
- `invite` needs a running node. A phone can read the link from the screen with `-qr`; to another device without a camera it still has to be transferred another way (e.g. by mailing it to yourself).
- Members of a private network (PSK) cannot be removed individually; membership networks exist for that.
- The built-in DNS seed `seed.ai-swarms.net` currently lists only one start node; the concept envisages several independent operators.

**Distribution**

- No prebuilt binaries; build from source.
- Protocol at version 0.1, incompatible changes are possible.
