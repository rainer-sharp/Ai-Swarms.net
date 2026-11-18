# Threat model v0.1

**English** · [Deutsch](de/threat-model.md)

**Worth protecting:** users' prompts and results, operators' hardware and energy, the integrity of results, the availability of the network.

**Not promised:** secrecy on foreign computers, correctness of majority answers, complete Sybil resistance, unlimited real-time scaling.

## Attackers

| Attacker | Capabilities |
|---|---|
| Curious provider | reads all prompts it processes |
| Malicious provider | delivers wrong or manipulated results, including prompt injection |
| Free rider | uses capacity without contributing |
| Sybil | creates any number of identities |
| Network attacker | replay, eclipse, DHT poisoning, gossip spam |
| Website in the operator's browser | addresses the local gateway (CSRF, DNS rebinding) |

## Attacks and countermeasures

| Attack | Countermeasure | Status |
|---|---|---|
| Data harvesting | privacy classes, fragmentation, sensitive parts local | classes implemented; fragmentation open |
| Forged results | verifiers of other families, canary tasks, spot checks | verifier of another family implemented; canaries from phase 3 |
| Inflated self-assessment (`strength`) | only acts as ordering at equal suitability; verifier stays from another family; measured rating (Elo) replaces the declaration | phase 1: trusted circle only; Elo from phase 3 |
| Prompt injection between models | results are data; delimiters; structured outputs; discard instruction patterns | implemented (heuristic) |
| Smuggled tool calls (a provider gets the local agent to execute commands), leakage of tool results | tool list per network, none for strangers and in membership networks without a list; calls not listed are discarded, history with other tools does not go to restricted agents; only pattern `single`, only models with `tools: true`. The list only checks names; confirming risky calls remains the agent's job | implemented |
| Replay | nonce + expiry + signature | implemented |
| Identity forgery | sender = remote end of the connection, signature with the peer ID key | implemented |
| Provider overload | REM: slots, time windows, GPU minutes, mean utilisation, temperature, RAM reserve; max. 4 streams per peer; 4 MiB per message | implemented |
| Own use displaced | own use aborts foreign jobs | implemented |
| Access to the gateway from outside | bound to localhost only, check of sender and Host header | implemented |
| Free riders | credit limits, tit-for-tat, limited optimistic unchoking | receipts from phase 1, credit from phase 4 |
| Sybil | proof of useful work, earned trust, guarantees | phase 1: trusted circle only |
| Rating cartels | subjective trust, objective checks before opinions | phase 3 |
| Benchmark manipulation | commit-reveal, rotation, reputation of submitters | phase 4 |
| Eclipse | many entry sources, peers from different networks, remember trusted peers, diversity filter in peer cache and routing table | phase 2, implemented ([P2P layer](p2p-layer.md) § 2.1, § 8) |
| DHT poisoning, gossip spam | no freely writable DHT values, provider records only for oneself, capabilities signed directly at the peer, no gossip in phase 2, rate limits | phase 2, implemented ([P2P layer](p2p-layer.md) § 4, § 5, § 8) |
| Abuse of foreign relays | in the open network automatic only for publicly reachable nodes and with a conservative time, data and connection budget, can be disabled; content end-to-end encrypted, metadata visible | phase 2, implemented ([P2P layer](p2p-layer.md) § 6) |
| Linking private and public participation | own key and own host per network; private networks never published in the open network | phase 2, implemented ([P2P layer](p2p-layer.md) § 9) |
| Compromised helper, departed member, intercepted invitation in groups | membership networks: certificate per node, role `helper` without tasks, individual revocation, single-use invitation ([membership networks](membership.md) § 9) | implemented, tested on hardware across two sites; in a PSK network revocation only via a new key for everyone |
| Execution of foreign code | Wasm sandbox without network/files, on Linux additionally a container | phase 4 (sandbox audit) |

## Principles

- No security-critical decision based solely on peer votes.
- Foreign tasks have no shell, file, browser or network rights.
- Missing or outdated measurements lead to "no new jobs", not to "everything allowed".
