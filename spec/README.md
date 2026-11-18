# Specification (phase 0)

**English** · [Deutsch](de/README.md)

Versioned, language-independent specification of SwarmAiNet. It is based on the [overall concept](../docs/concept.md).

| Document | Contents |
|---|---|
| [protocol-v0.1.md](protocol-v0.1.md) | Identity, transport, envelope, messages, flow, states |
| [p2p-layer.md](p2p-layer.md) | Phase 2: private and open networks, network entry, reachability, DHT, relay, results on hardware |
| [membership.md](membership.md) | Draft: membership networks for groups and companies, certificates instead of a network key, roles, revocation |
| [cooperation.md](cooperation.md) | Phase 3 (draft): competence profile, bandit over recipes, PI controller, cascade, task graph, lease coordinator |
| [privacy-classes.md](privacy-classes.md) | Who may see which data |
| [threat-model.md](threat-model.md) | Attackers, attacks, countermeasures, implementation status |
| [measurement-plan.md](measurement-plan.md) | Single model versus swarm at equal budget |
| [benchmark.md](benchmark.md) | First benchmark round: setup, tasks, procedure, result, open questions |

The specification exists in English and German ([`de/`](de/README.md)); both versions have the same section numbers. The normative keywords are MUST, SHOULD and MAY (German: MUSS, SOLL, KANN).

**Versioning:** `MAJOR.MINOR`. Incompatible changes increase the protocol ID (`/swarmainet/<version>`). New optional payload fields are compatible. A second, independent implementation (e.g. in Rust) is to check the specification for interoperability.

**License:** the specification is licensed under [CC BY 4.0](../LICENSE-DOCS). Independent implementations of the protocol are expressly welcome.

**Gate phase 0:** specification versioned, test cases and threat model available. The test cases are Go tests next to the code (`go test ./...`).
