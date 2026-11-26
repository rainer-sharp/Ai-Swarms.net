# Configuration

**English** · [Deutsch](de/configuration.md)

The configuration lives in `~/.swarmainet/config.yaml` (create it with `swarmainet init`, use another file with `-config PATH`). A complete, commented example with all sections is [`config.example.yaml`](../config.example.yaml):

| Section | Contents |
|---|---|
| `node` | Addresses, gateway, network, trust, reachability (table below) |
| `backends` | Inference servers and their models: `family`, `skills`, `context`, `strength`, `tools`, `vision` (understands images), `offer` |
| `resources` | Resource manager: parallel tasks, GPU limits, temperature, RAM reserve, time windows, GPU minutes, energy (PV surplus, electricity price via Home Assistant) |
| `tasks` | Default pattern, default privacy class, token limit `max_tokens` (default 8192, applies when a request names none; the provider lowers it to fit the model's `context`), deadline (`job_deadline_s` for [asynchronous jobs](usage.md#asynchronous-jobs), default 3600), subtasks; `pseudonymize_terms`: own terms (names, projects, customers) that are always replaced for foreign nodes with `reduced` ([usage](usage.md#optional-headers)) |
| `cooperation` | How the pattern is chosen when a request names none (table below) |

Secrets do not belong in the file: the Home Assistant token comes from an environment variable (`token_env`), network keys are kept in separate files with permissions 0600.

## Choosing the pattern (`cooperation.…`)

| Key | Default | Meaning |
|---|---|---|
| `selection` | `rules` | `rules`: `tasks.pattern` applies; `bandit`: the node chooses the pattern per task category from its competence profile ([cooperation](../spec/cooperation.md) § 3). A request with `swarmainet/<pattern>` always gets that pattern |
| `coordinate_for_others` | `false` | Coordinate [asynchronous jobs](usage.md#asynchronous-jobs) of trusted peers and hold their checkpoints as substitute coordinator; this node then sees their jobs in plain text |
| `exploration` | `0.05` | Share of random choice among the feasible patterns (0 to 0.5) |
| `targets` | code 0.9, math 0.95, otherwise 0.85 | Targets per category, e.g. `code: { quality: 0.9, cascade_error: 0.05 }`. With `bandit`, controllers keep the success rate at `quality` (by setting a minimum effort) and the error rate of the cheap stage of the cascade at `cascade_error` (default 0.05, by setting the threshold `θ`) ([cooperation](../spec/cooperation.md) § 4.2); `latency_p90_s` is reserved for the latency loop |

## Network keys (`node.…`)

| Key | Default | Meaning |
|---|---|---|
| `listen` | 4711 TCP/QUIC, IPv4 and IPv6 | Listen addresses; `[]` means outgoing only, reachable via relays |
| `gateway` | `127.0.0.1:7711` | OpenAI API and status page, localhost only |
| `network_id` | `swarmainet` | Network ID; `swarmainet` is the open network |
| `network_key_file` | – | Key file of a private network (PSK) |
| `membership` | – | Makes a network a membership network: `root`, optionally `cert_file` |
| `mdns` | `true` | Find other nodes on the LAN automatically |
| `static_peers` | `[]` | Permanently connected addresses |
| `bootstrap` | `[]` | Entry addresses, dialled while the node has no peers, no DHT partners or, behind NAT, no relay slot (`join` adds them) |
| `trackers` | `[]` | HTTPS address mailboxes |
| `trusted_peers` | `[]` | Peer IDs that may receive confidential tasks and all tools |
| `provide` | `false` | Offer your own models in the network |
| `allowed_tools` | PSK all, otherwise none | Tools that models of this network may see |
| `pex` | `true` | Peer exchange with connected nodes |
| `relay_service` | `auto` | Relay connections of others: `auto` in the open network when publicly reachable, `true` always (helper), `false` never |
| `relay_limits` | – | Relay limits: `reservations`, `circuits_per_peer`, `data_mib`, `duration_s` |
| `helper` | `false` | Only carry the network (relay, DHT server), no models, no tasks |
| `dht_server` | `auto` | `auto`: open network when publicly reachable, private with its own listen address or as a relay; `true`/`false` to force |
| `dns_seeds` | open: `seed.ai-swarms.net` | Domains with `_dnsaddr` records for entry; `[none]` disables. Also applies to the networks under `networks` unless they specify their own; membership networks use none |
| `announce` | `[]` | Additionally announced addresses without `/p2p`, e.g. `/dns4/swarm.example.de/tcp/4711` behind a port forwarding |
| `announce_private` | `false` | Publish internal addresses in the open network too (test networks only) |
| `port_mapping` | `false` | Request a port forwarding from the router via UPnP/NAT-PMP; status under `networks[].port_mapping` |
| `networks` | – | Further networks, each with its own port and `identity: separate` (default) or `shared`; see [Network types](networks.md#several-networks-at-once) |

## Files under `~/.swarmainet/`

| File | Contents |
|---|---|
| `config.yaml` | Configuration |
| `identity.key` | The node's private key, determines the peer ID |
| `*.key` | Network keys of private networks |
| `peers-<network_id>.json` | Most recently connected peers |
| `jobs.json` | Asynchronous jobs and their answers, kept for 24 hours |
| `profile.json` | Competence profile of the agents (see [usage](usage.md#feedback-and-competence-profile)) |
| Files per membership network | Certificate, revocation lists, on an issuer its key and the register of open invitations |

Never pass on key files and do not put them in a repository. Whoever has `identity.key` appears under your peer ID.
