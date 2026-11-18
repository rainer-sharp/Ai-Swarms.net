# Architecture

**English** · [Deutsch](de/architecture.md)

A node is a single Go program. It accepts requests through a local OpenAI-compatible API, splits them into subtasks according to a pattern, selects its own and remote models for them and has results checked by a different model family. It reaches remote models via libp2p; every subtask is a signed message, and both sides acknowledge it with a receipt.

```
Application (Open WebUI, IDE, agent)
        │  OpenAI API, localhost only
        ▼
   Gateway ──► Orchestrator ──► local backends (Ollama, llama.cpp, vLLM …)
                    │
                    ▼
               P2P layer ──► other nodes (direct, hole punching or relay)
                    ▲
     Resource manager decides whether foreign tasks are accepted
```

| Module | Package |
|---|---|
| Gateway | `internal/gateway` – OpenAI API, status API, web UI, localhost only |
| Selection | `internal/orchestrator` (`candidates`, `Route`) – rule-based: skill, on a tie load balancing (free slots, running subtasks, round robin), then context size; verifier by model strength (`strength`), then context size, then own before remote |
| Orchestration | `internal/orchestrator` – patterns `single`, `parallel`, `draft_verify_synthesize`, `parallel_verify`; tool permissions per network |
| Trust | `internal/receipt` – receipts signed by both sides; `internal/membership` – certificates, chains, revocation lists, invitations |
| Resources | `internal/rem` – slots, time windows, GPU minutes, mean GPU utilisation, temperature, RAM reserve, PV hysteresis, own use, emergency stop |
| P2P transport | `internal/p2p` – one libp2p host per network (QUIC/TCP, PSK), mDNS, static and bootstrap addresses, relay with reservations, hole punching, AutoNAT, peer exchange, peer cache, Kademlia DHT, admission in membership networks, tracker client, rate limits, diagnostics |
| Network entry | `internal/invite` – invitation links and key files; `internal/tracker` – tracker records, server and client |
| Protocol | `internal/protocol` – signed envelopes, replay protection, state machine |
| Local backends | `internal/backend`, `internal/node/local.go` |
| Benchmark | `internal/bench`, data in `bench/` |
| Commands | `cmd/swarmainet` |

The authoritative description of the protocol, network layer and security assumptions is the [specification](../spec/README.md). The original [overall concept](gesamtkonzept.pdf) (in German) describes the goal including the later phases.
