# P2P layer (phase 2)

**English** · [Deutsch](de/p2p-layer.md)

Status: implemented, gate for the private swarm met on real hardware on 29 Oct 2026 (§ 14); the open network on 18 Nov 2026 across two sites (§ 15). Implemented: peer cache (§ 2.1), peer exchange (§ 2.4), reachability (§ 3, port forwarding via UPnP/NAT-PMP without PCP), DHT (§ 4), capability references (§ 5), relay (§ 6), helper and bridge nodes (§ 6.1), abuse protection (§ 8), diagnostics (§ 10), invitation (§ 2.2), tracker (§ 2.3), several networks at once (§ 9). Extends [protocol v0.1](protocol-v0.1.md) with network entry, reachability and finding peers beyond the LAN. The messages, the envelope and the flow of a subtask remain unchanged. The keywords MUST, SHOULD and MAY are normative.

Principle from the overall concept: DHT and relay only provide contacts, addresses and references, **no control and no trust**. Whether a peer is trusted and what it may see is still decided by the PSK, `trusted_peers` and the [privacy classes](privacy-classes.md).

**Guiding principle for the open network: self-governing like BitTorrent.** No node and no operator is indispensable. After the first contact, the network carries itself:
- Every publicly reachable node automatically becomes a DHT server.
- Peers exchange contacts with each other (section 2.4).
- Everyone remembers working peers (section 2.1).

Bootstrap nodes, trackers and DNS seeds are only a jump start, like trackers and `router.bittorrent.com` in BitTorrent. Anyone can run one, and none has special rights. There is no registration and no central list of participants.

## 1. Two network types

The same software supports two kinds of networks. A node can participate in several networks at once (section 9).

| | Private network | Open network |
|---|---|---|
| Purpose | your own devices or a closed circle, also across several sites | any foreign nodes |
| Access | shared PSK (libp2p `pnet`) | free |
| Trust | every participant is trusted | only keys from `trusted_peers` |
| Privacy classes | `trusted`, `reduced`, `public` | `public`; `trusted`/`reduced` only to `trusted_peers` |
| Transports | TCP only (limitation of `pnet`) | QUIC, TCP, optionally WebTransport/WebSocket |
| DHT | its own, members only | shared, open |
| Entry | invitation, own helper nodes, tracker | bootstrap list, tracker, DNS seeds, invitation, then peer exchange and DHT |
| Relay | only relays of the same network | relays of any participants (opt-in) |
| Size | small (up to about 64 nodes), full mesh | large, partial mesh |

**Network ID (`network_id`).** Every network has an ID from `[a-z0-9-]{1,32}`. The open network is called `swarmainet`. A private network MUST use a different ID, recommended: `p-` and 8 random hex characters, which `swarmainet invite` generates. The ID separates mDNS service, DHT and peer cache. It is **not a secret**; the protection of a private network lies solely in the PSK.

### 1.1 Consequences of the PSK

`pnet` encrypts every connection with the network key before the handshake. Nodes without the key cannot establish a connection and only see noise. It follows that:

1. **TCP only.** QUIC and WebTransport cannot be used with `pnet`. Hole punching works via TCP (simultaneous open) and succeeds less often than via QUIC.
2. **No foreign infrastructure.** Public bootstrap nodes, DHT servers and relays cannot be reached. The private network carries itself with its own helper nodes (section 6.1). A helper needs no GPU and is entry point, DHT server and relay at the same time.
3. **What is needed for what:**

| Setup | needed |
|---|---|
| one site (LAN) | nothing additional; mDNS, all nodes directly connected |
| several sites, variant A: **bridge node** | a helper at a place that reaches both sides **outgoing**, e.g. a VM with a VPN connection to the other site. It does not need to be reachable itself, no port is opened. |
| several sites, variant B: **helper on the internet** | a helper on a server with a public address (or behind a port forwarding). All sites connect to it outgoing. The same server MAY additionally be a helper of the open network (own host and port, section 9). |

   Both variants can be combined. Two helpers at different places make the network independent of the failure of a single one.
4. **Without a helper**, only nodes on the same LAN (mDNS) and nodes directly reachable via IPv6 or port forwarding find each other. `swarmainet status` MUST report this case as a diagnosis (section 10).

## 2. Network entry

At start, a node tries to establish contact in parallel or in this order:

1. **Peer cache:** most recently successfully connected peers (section 2.1). The main source from the second start on.
2. **mDNS** on the local LAN (as in phase 1, service name `swarmainet-<network_id>`).
3. **Invitation:** imported at the first join (section 2.2).
4. **Bootstrap list** (`bootstrap`): multiaddresses with a fixed peer ID. For the open network, the software ships a list of several independent operators; for private networks it is the network's own entry list.
5. **Tracker** (`trackers`): HTTPS address mailboxes (section 2.3).
6. **DNS seeds** (`dns_seeds`): `dnsaddr` TXT records under `_dnsaddr.<domain>` of the form `dnsaddr=/ip4/…/tcp/4711/p2p/<peer-id>`. They only provide addresses; the peer ID in each record MUST be verified when connecting. DNS is a hint and never a proof of trust. In the open network, the built-in seed `seed.ai-swarms.net` applies if nothing is specified; `[none]` disables it, a list of your own replaces it. The setting also applies to further networks (§ 9) unless they have their own; membership networks never ask a seed. Up to 8 randomly chosen records are dialled as long as the node needs an entry (see below). Via the seed, entry nodes can be exchanged without shipping new software: a switched-off start node is simply removed from DNS, new helpers are added.
7. **Static peers** (`static_peers`, as in phase 1).

**When entry nodes are dialled.** The bootstrap list and DNS seeds are dialled every minute as long as (a) no peer is connected, (b) the DHT routing table is empty or (c) a node without public reachability and without a relay of its own has no relay slot. Entry nodes that are already connected are skipped. Failed attempts are only logged as long as no peer at all is connected. Reason for (b) and (c): two nodes behind NAT that only know each other after the start node has failed would otherwise form an island that new nodes cannot find via the returned start node (§ 15).

During operation, **peer exchange** (section 2.4) and the DHT add contacts without any of the jump starts being needed any more.

The connection to an address with `/p2p/<peer-id>` MUST be aborted if the remote end identifies itself with a different key (the libp2p handshake takes care of this).

After that, the node fills its DHT routing table (section 4), publishes its references only with `provide: true` (section 5) and keeps connections according to the rules of section 7.

If all entry sources have failed, known nodes keep working via the peer cache; only new nodes then need an invitation.

### 2.1 Peer cache

- Storage: `<data_dir>/peers-<network_id>.json`, local only. Content per peer: peer ID, most recently working addresses, time of the last success, connection type (direct, hole punching, relay).
- At most 128 entries. Entries without success for 30 days drop out.
- Against eclipse, the cache SHOULD preserve diversity: at most 8 entries per IPv4 /16 or IPv6 /32 and per relay. `trusted_peers` and bootstrap nodes are preferably kept.
- At start, at most 16 entries are dialled at once, trusted peers first. In a private network, the remaining entries are available for rebuilding (section 7).
- Saved every minute and on exit; the file is replaced, not overwritten (first a temporary file, then rename).

### 2.2 Invitation

`swarmainet invite [--with-key] [--qr]` creates a link and optionally a QR code in the terminal (error correction L, a border of two modules, black on white regardless of the colour scheme):

```
swarmainet://join?v=1&net=<network_id>&peer=<multiaddr with /p2p/...>[&peer=...][&tracker=<https-url>][&psk=<base64url>]
```

- `peer` (once or several times): publicly reachable addresses of the inviter or its entry nodes. Local addresses only if explicitly requested (`--lan`).
- `psk` only for private networks and only with `--with-key`. **Whoever has the link is a member.** The link MUST therefore be passed on via a confidential channel. The software displays a warning about this and does not store the link.
- `swarmainet join <link>` creates the network in the configuration, writes the key to `<data_dir>/<network_id>.key` (mode 0600) and adopts the addresses as the bootstrap list (`node.bootstrap`: dialled as long as the node needs an entry, § 2). Without a key in the link, the key file can be given with `-key FILE`. An existing key with different content is not overwritten.
- `swarmainet invite` takes the addresses of the running node without loopback, in the open network only public ones (with `-lan` also local ones); `-peer` adds further ones, e.g. those of a bridge node.
- An invitation without a PSK only contains public contact data and may be shared openly.

A PSK cannot be revoked. If a member leaves, the only option is a new key for everyone (`swarmainet psk` + new invitations). Admitting and removing members individually is an open question (section 12).

### 2.3 Tracker (HTTPS mailbox)

A tracker is an HTTP endpoint where nodes deposit their current addresses and fetch the addresses of others. The interface is deliberately simple, so that trackers can be operated independently of each other. The reference is part of the software (`swarmainet tracker`, a small HTTP service without libp2p) and runs, for example, on a helper node behind a TLS reverse proxy. The tracker does not traverse NAT. It replaces DynDNS and fixed bootstrap addresses and helps with the first contact.

**Interface** (HTTPS only):

| Call | Effect |
|---|---|
| `POST <url>` with `{"net": …, "id": …, "record": …}` | creates or replaces the record `id` in the network `net` |
| `GET <url>?net=<net>` | returns `{"records": [...], "your_ip": "…"}`, at most 64 records, randomly chosen |

The tracker stores records for at most 1 h (nodes renew every 20 min), at most 2 KiB per record and 256 records per network. It SHOULD limit requests per IP (reference: 60 per minute, at most 1024 networks and 16384 records in total, in memory only; behind a reverse proxy, the last entry in `X-Forwarded-For` counts, because all before it can come from the sender). `your_ip` is the sender address of the request and only a hint.

Nodes deposit their record at start and every 20 min, provided they have dialable addresses. Records are fetched at start and every 20 min as long as fewer than 8 peers are connected, every minute without any peer. Nodes only accept trackers via HTTPS (`http` only for localhost, e.g. behind a local reverse proxy). `swarmainet tracker` listens on `127.0.0.1:7720` by default for a TLS reverse proxy, with `-tls-cert`/`-tls-key` via HTTPS itself; `-trust-proxy` takes the sender IP from `X-Forwarded-For`.

**Open network:** `net = "swarmainet"`, `id` = peer ID, `record` in plain text:

```json
{ "v": 1, "peer_id": "<peer-id>", "addrs": ["/ip4/…/tcp/4711", "…"],
  "expires_at": "…", "nonce": "<128 bit hex>", "signature": "<base64>" }
```

Signed with the key of the peer ID over `"swarmainet/tracker/v1\n" + net + "\n" + peer_id + "\n" + expires_at + "\n" + nonce + "\n" + addrs` (addresses separated by `\n`). Nodes MUST check signature and expiry. The tracker MAY check them, but does not have to. The address rules from section 3 apply, so private addresses do not belong in it.

**Private network:** the tracker must learn neither the network ID nor members or addresses.
- `k = HKDF-SHA256(psk, info = "swarmainet/tracker/v1")`
- `net` = hex of the first 16 bytes of `HMAC-SHA256(k, "net")`
- `id` = hex of the first 16 bytes of `HMAC-SHA256(k, peer_id)`
- `record` = base64 of `nonce ‖ XChaCha20-Poly1305(k, nonce, record as above)`

For the tracker this is opaque data. Whoever does not know the PSK cannot create valid records. They can only fill the tracker with garbage, which nodes discard when decrypting.

A node MAY use several trackers. If one fails, nothing is blocked. A tracker sees the IP addresses of the querying nodes. In the open network these are public anyway, and in the private network that is the price of simple operation.

### 2.4 Peer exchange

Protocol `/swarmainet/pex/0.1`, modelled on PEX in BitTorrent: nodes tell each other about peers they are currently connected to. This way knowledge of the network grows without entry nodes.

- Fetched after the connection is established and every 10 min thereafter. One line of JSON per direction: `{"peers": [{"peer_id": "…", "addrs": ["…"]}]}`, at most 32 peers, only those with which a direct or relay connection existed in the last 30 min.
- Not passed on: in the open network `trusted_peers` (otherwise the circle of trust would become visible) and private addresses, as well as peers of another network. In the private network all members are trusted anyway and all are named.
- In the private network, a node also names itself with its own addresses, including private ones. Identify discards private addresses if the remote end looks public, i.e. also via a relay on the internet; two members on the same LAN would otherwise not find the direct path.
- A relay additionally names the circuit address via itself for each peer (relay part: the address of the connection to the requester). Peers behind NAT often have no other address, and libp2p does not adopt circuit addresses via the receiver from identify.
- A newcomer asks its first peers immediately after identify and connects to the ones named. Because connections count in both directions, the others thereby learn about the newcomer without having to ask again.
- Received peers are hints. At most 8 new connections are attempted per answer. The diversity rules from section 2.1 apply. A peer only goes into the peer cache after a successful connection.

## 3. Reachability

Connection paths, automatically in this order:

1. **Direct** via QUIC/TCP, preferably via a public IPv6 address.
2. **Directly on the LAN**, found via mDNS.
3. **Hole punching (DCUtR)** over a connection initially established via a relay.
4. **Relay** as a fallback, with a budget (section 6).
5. If nothing works: clearly report "not connected" and offer the diagnosis.

Building blocks:

- **AutoNAT v2** determines whether the own addresses can be reached from outside (result: `public`, `private`, `unknown`), per address. The status summarises this per address family (`reachability_by_family`): behind DS-Lite or CGNAT, IPv4 is never reachable, IPv6 is with a forwarding in the router's firewall; a hint explains this. In the private network only members check this.
- **Identify/observed addresses** provides the address seen from outside.
- **Port forwarding via UPnP/NAT-PMP/PCP** only after opt-in (`port_mapping: true`, off by default, also per network), because it changes router settings. Implemented with libp2p's NAT manager (UPnP IGD and NAT-PMP; PCP is missing): it forwards the listen ports, requests forwardings with a lifetime of 1 minute and renews them every 20 s, so that they expire at the router after the node ends. The forwarded public address is announced and confirmed by AutoNAT. The status shows whether a router was found and which addresses are forwarded; after a minute without a router or without a forwarding, a hint appears (on a FritzBox, "Allow automatic port forwarding" must be set for the device).
- **Publishing addresses:** a node only publishes addresses whose reachability is confirmed, and relay addresses (`/p2p-circuit`). In the open network, private addresses (RFC 1918, ULA `fc00::/7`, link-local, loopback) MUST be removed from identify responses and DHT records, also as the relay part of circuit addresses. Behind a port forwarding, a node does not see its public address; with `announce` it announces additional addresses, e.g. `/dns4/swarm.example.de/tcp/4711` (guide: `deploy/raspberry-pi/`). A node does not pass on loopback addresses in any network (identify, peer exchange, tracker). Only for test networks does `announce_private: true` lift both. Internal IPs would otherwise be publicly visible, which would undermine the capability rule of the protocol (§ 4.1). On the LAN, mDNS is sufficient. In the private network, private addresses may be published.

## 4. DHT

Kademlia according to the libp2p specification, with a protocol prefix of its own per network:

| Network | Protocol ID |
|---|---|
| open | `/swarmainet/kad/1.0.0` |
| private | `/swarmainet/<network_id>/kad/1.0.0` |

The prefix separates the swarm from other libp2p networks (e.g. IPFS) and private networks from each other. For private networks it separates in addition to the PSK.

- **Server or client mode:** as in BitTorrent, every node for which AutoNAT reports `public` **automatically** becomes a DHT server (`dht_server: auto`, default). With `dht_server: true` it always is, with `false` never. In the private network, `true` SHOULD be set if the node is reachable for the members or reaches them on its own, even without public reachability (bridge node, section 6.1). Unreachable nodes are clients: they ask, but store nothing for others.
- **Purpose 1, peer routing:** finding addresses for a known peer ID. With this a node reaches its `trusted_peers` at other sites without knowing their IP.
- **Purpose 2, capability references:** section 5.
- The node SHOULD NOT accept arbitrary values (`PUT_VALUE`). Only provider records and peer addresses are stored. There is thus no freely writable storage that strangers can fill.
- The validator namespace `/swarmainet/` is reserved for later signed records (e.g. benchmark pool, phase 4).

## 5. Finding capabilities

The capability announcement (protocol § 4.1) remains the only source of capabilities. It is still fetched directly from the peer and signed by it (`/swarmainet/capability/0.1`). The DHT only contains **references** "peer X offers capability Y".

### 5.1 Keys

For every offered skill (`skills`, e.g. `code.python`) and every modality (`modality.image` etc.), a provider publishes a provider record under

```
key(tag, shard) = CIDv1(raw, sha2-256("swarmainet/skill/v1\n" + network_id + "\n" + tag + "\n" + shard))
```

with `shard` as a decimal number in `0 … S-1`, `S = 4`. Every provider chooses **one** shard per tag at random and keeps it as long as it runs. This way a popular tag is spread over four DHT regions, and no single node becomes a bottleneck (overall concept: "own index scheme on top of the DHT"). Searchers query all `S` shards.

Additionally, there is a collective key `tag = "*"` for "offers anything at all". Private networks use it as a member list.

### 5.2 Flow

- A provider only publishes with `provide: true` and only tags of models with `offer: true`. Records are republished every 12 h and immediately on change; if that fails (e.g. with a still empty routing table), again after 5 s, 10 s … up to 1 min. The validity on the DHT servers is the libp2p default of 48 h.
- A coordinator searches for the required tag when needed: in the open network for tasks of class `public` if fewer than 2 connected agents offer the skill, for at most 5 s. It connects to at most 8 new providers per search, fetches their announcement and adds it to its catalogue (TTL 5 min as before).
- The selection of agents stays with the orchestrator. The DHT answer is an unverified list of candidates. A provider whose announcement does not contain the tag is ignored for this tag for 1 h.

**Security:** in Kademlia a peer only publishes provider records for itself; the remote end MUST check that provider and sender match. Forged references can thus only point to the forger itself, not to third parties. What remains (many Sybil identities under one tag) is limited by section 8 and by rating from phase 3.

### 5.3 Gossip

In phase 2, capability announcements are **not** spread via gossip. Fetching on request plus DHT references is sufficient and offers less surface for spam. GossipSub remains an option for phase 3 if search latency or freshness are not sufficient.

## 6. Relay

Circuit Relay v2 (`/libp2p/circuit/relay/0.2.0/hop` or `/stop`).

- **Using a relay** (client) is the default. A node with AutoNAT `private` reserves a slot at up to 2 relays of its network and publishes the relay addresses.
- **Offering a relay** (`relay_service`):
  - `auto` (default): in the **open network** automatically as soon as AutoNAT reports the node as publicly reachable, with more conservative limits (see table); off in the private network. This deviates from the original opt-in of the overall concept: without relays at reachable nodes, the open network falls apart into islands as soon as the start node goes away, because nodes behind NAT can only reach each other via a relay (as in BitTorrent, the reachable participants carry the network). Content stays end-to-end encrypted, and the limits restrict bandwidth and connections.
  - `true`: always, even without public reachability, e.g. helper and bridge nodes. In the private network it only matters that the members are connected to the relay, no matter who established the connection.
  - `false`: never.

  Default limits, individually overridable with `relay_limits` (`reservations`, `circuits_per_peer`, `data_mib`, `duration_s`):

| Limit | Default |
|---|---|
| Reservations in total | 32, with `auto` 16 |
| Reservations per IP | 4, with `auto` 2 |
| Duration of a reservation | 1 h |
| Connections per peer | 2, in the private network 16 |
| Duration of a relay connection | 10 min |
| Data per relay connection | 8 MiB |

  In the private network, the per-IP limits are dropped, because via a bridge node every member of one side reaches every member of the other. The libp2p default limit of 128 KiB per connection is not sufficient for tasks; 8 MiB covers a message of at most 4 MiB in each direction.
- Relay connections end when the limits are reached. In the private network, every node re-establishes connections to known members every minute, with backoff up to 15 min on failures (section 7).
- If there is only a relay connection to a trusted peer, the node tries a direct connection via the known addresses after identify and every minute thereafter, independently of DCUtR. The limited connection then expires. Foreign peers in the open network stay with DCUtR.
- Connections via a relay are *limited*. The protocols `capability` and `task` MUST allow them. A coordinator SHOULD, however, first wait for hole punching (up to 5 s) and preferably give tasks with a large context to directly connected providers.
- End-to-end encryption (Noise/TLS) runs through the relay. A foreign relay only sees who exchanges how much with whom, not the content. This metadata is deliberately accepted in the open network. In the private network, all relays are members.
- In the open network, offering a relay will be rewarded via receipts from phase 4. Until then it is voluntary.

### 6.1 Helper nodes

A helper node is a node without backends that only carries the network: entry point, DHT server and relay. It needs no GPU, little CPU and about 100 MB RAM. `node.helper: true` sets `dht_server: true` and `relay_service: true` and requires no `backends`. A node with models MAY take on the same tasks in addition.

- **Bridge node** (private network only): a helper that does not need to be reachable but reaches both sides on its own, e.g. a VM behind the hypervisor's NAT with a VPN connection to another site.
  - It dials the nodes of both sides via `static_peers`/`bootstrap`. mDNS does not get through NAT or VPN.
  - Via these connections, the members reserve relay slots and reach the other side.
  - The bridge node MUST protect the connections it established itself (connection manager) and re-establish them after an interruption, because the other side cannot dial it.
  - Hole punching does not help here, because the sides have no common direct path. Traffic between the sides runs permanently via the relay.
  - If the tunnel belongs to a foreign network, e.g. a corporate network, the relay forwards traffic into this network. This must be agreed with its operator.
- **Helper on the internet:** on a server with a public address. It can carry the private network and, as a separate host, the open network at the same time (section 9). This way it can serve the open network as a jump start at the beginning until other helpers exist.

- Anyone can run helper nodes. It gets into the open network on its own via peer exchange and DHT, not via a list. The shipped bootstrap list is only a selection of reliable helpers.
- **Docker:** there is an image with the data directory as a volume (`Dockerfile`, template and Compose file under `deploy/docker/`, optionally with a tracker). `network_mode: host` is recommended, because AutoNAT, observed addresses and IPv6 are only correct this way. With a bridge network, only the port forwardings `4711/tcp` and `4711/udp` are possible, practically IPv4 only.
- **Behind a home router** (e.g. FritzBox): port forwarding `4711/tcp` (private network) or `4711/tcp` and `4711/udp` (open network) to the Docker host, for IPv4 and, if available, for the host's IPv6 address. The bootstrap entries SHOULD contain a DNS name (`/dns/…`, e.g. MyFRITZ! or DynDNS) instead of an IP.
- **Moving:** if the helper is moved to another server, its peer ID stays the same if the key moves with it. Then it is sufficient to change the DNS name. Existing nodes find it again via peer cache, tracker and DHT anyway.
- A helper for a private and the open network is two hosts with two ports (section 9).
- **Helper as a role:** a helper is any member that is reachable for the others (or reaches them on its own) and has `relay_service: true`, not a specific device. The dependency on the helper depends on the network type:
  - **Private network across several sites:** if the only such member fails, the sites keep working on their own (LAN, mDNS). Direct connections between the sites that were created by hole punching remain until they break; relay connections end at the latest at their time limit. **No new connections between the sites are created any more**: stored addresses point to devices behind NAT, and hole punching needs a relay to coordinate. A helper becomes dispensable as soon as another reachable member has `relay_service: true`; two at different places remove the single point of failure.
  - **Open network:** helpers are a jump start like the bootstrap nodes in BitTorrent. If enough nodes are publicly reachable and offer a relay, peer cache, peer exchange and DHT carry the network without them; new nodes enter via an invitation from any member or a tracker. Unlike in BitTorrent, relay and port forwarding are opt-in here, and a coordinator has to reach a *specific* provider instead of any peer. The network therefore needs volunteers with `relay_service`; port forwarding via UPnP (`port_mapping`, section 3) increases the number of reachable nodes.

## 7. Keeping connections

| | Private network | Open network |
|---|---|---|
| Goal | full mesh: a connection to every member from peer cache, mDNS and collective key | lower limit 32, upper limit 96 connections (connection manager) |
| Protected | all members up to 64 | `trusted_peers`, bootstrap nodes, peers with a running task |
| Retry | every 60 s, with backoff up to 15 min | only when needed (search, trusted peers every 5 min) |

Above 64 members, a private network behaves like an open one but stays closed.

## 8. Abuse protection

Supplements the [threat model](threat-model.md):

- **libp2p resource manager** with fixed limits for connections, streams and memory, per peer and in total. In addition, at most 4 task streams per peer and 4 MiB per message still apply.
- **Rate limit for foreign tasks** (open network only, untrusted peers): 10 `TASK_PROPOSE` per minute and peer, 60 per minute in total. After that, `TASK_REJECT` with `reason: "busy"` follows, without a check by the resource manager (REM).
- **Rate limit for capability fetches and peer exchange** (likewise): 6 each per minute and peer; above that, the stream is reset.
- **Eclipse:** several independent bootstrap operators; diversity in the peer cache (section 2.1); trusted peers always remembered and protected; in the open network, a routing table with libp2p's diversity filter (at most 3 peers per IP group and bucket, 20 per group in the table; group: IPv4 /16, IPv6 by ASN). Peers behind the same relay count towards the relay's group.
- **DHT poisoning:** no freely writable values (section 4); provider records only for oneself; capabilities always signed directly at the peer.
- **Tracker and peer exchange:** records are signed or, in the private network, encrypted. Peer exchange only provides hints with a connection limit. A tracker or peer can withhold contacts (eclipse attempt), therefore several sources SHOULD be used at the same time.
- **Separation of networks:** a private network is not visible in the open network. Neither its ID nor its members may be published there (see also section 9).

## 9. Several networks at once

A node can be a member of one or more private networks and of the open network at the same time, e.g. as a home node in the private network that additionally offers surplus capacity publicly.

- Per network, a **separate libp2p host** runs with its own port, its own DHT, its own peer cache and its own mDNS service. This is necessary because `pnet` applies to all connections of a host.
- **Identity:** every network SHOULD get its own key (`<data_dir>/identity-<network_id>.key`). Otherwise it would be recognisable in the open network by the peer ID which node is also a member of a certain private network. Whoever accepts the linkage MAY use the same key with `identity: shared`.
- **Shared** are orchestrator, backends, resource manager and receipt store. Foreign tasks from all networks share the same slots; own use still displaces everything.
- **Assignment when coordinating:** the orchestrator sees agents from all networks with the network marked. A task of class `trusted` or `reduced` only goes to peers of a private network or to `trusted_peers`. A task of class `public` may go into any network. A task is never passed on from one network to another; a provider's subtasks stay in the network the task came from.
- The gateway (`127.0.0.1:7711`) remains single and bound to localhost only.
- `accept_public_tasks` is checked by the P2P layer per network; the resource manager admits public tasks as soon as one network accepts them. Receipts are kept in a shared store, the status summarises them across all own peer IDs.
- `swarmainet join` only replaces the first network if it is the unchanged open network of the default configuration or the same network; otherwise it adds a further network with the next free port.

## 10. Diagnostics

`swarmainet status` shows per network:

- network type, ID, own peer ID, number of peers (direct / via hole punching / via relay)
- AutoNAT result, published addresses, reserved relays
- DHT mode and size of the routing table
- **Plain-text hints**, e.g. "private network without a publicly reachable node: only LAN peers reachable", "all bootstrap nodes unreachable, peer cache only", "IPv6 available, port 4711 blocked incoming?" (the program's messages are in German)

`swarmainet ping <peer-id>` looks up the addresses via the DHT, tries the connection and reports the path (direct, hole punching, relay) or the point where it fails.

## 11. Configuration (draft)

The previous keys under `node` still describe the **first network**, so that existing configurations remain valid. Further networks are added under `node.networks`:

```yaml
node:
  network_id: p-3fa91c07          # first network: private, because network_key_file is set
  network_key_file: ~/.swarmainet/p-3fa91c07.key
  listen: [/ip4/0.0.0.0/tcp/4711, /ip6/::/tcp/4711]
  bootstrap:                      # new: own entry nodes (e.g. VPS)
    - /dns4/swarm.example.org/tcp/4711/p2p/12D3Koo…
  announce: [/dns4/swarm.example.de/tcp/4711]   # new: additionally announced addresses
  port_mapping: false             # new: UPnP/NAT-PMP/PCP, opt-in
  relay_service: auto             # new: auto | true | false (section 6)
  relay_limits: {}                # new: override the relay limits
  dht_server: auto                # new: auto | true | false
  trackers:                       # new: HTTPS mailboxes (section 2.3)
    - https://example.org/swarmainet/tracker
  pex: true                       # new: peer exchange (section 2.4)
  helper: false                   # new: only carry the network, without backends (section 6.1)
  networks:                       # new: further networks
    - network_id: swarmainet      # open network
      listen: [/ip4/0.0.0.0/udp/4712/quic-v1, /ip4/0.0.0.0/tcp/4712]
      dns_seeds: [seed.ai-swarms.net]  # if omitted, the built-in seed; [none] disables
      trackers: default
      identity: separate          # separate | shared
      provide: true
      accept_public_tasks: true
```

`provide`, `accept_public_tasks` and `trusted_peers` apply per network. If not specified, the values from `node` or `resources` apply.

## 12. Open questions

1. **Membership without a shared key:** a PSK cannot be revoked individually and forces TCP. Draft of a third network type with certificates per node: [membership networks](membership.md).
2. **Shipped jump starts** for the open network: at the beginning there are only own helpers and trackers. By which rules are helpers of other operators added to the shipped list (uptime, reachability, independence)? The network itself does not need the list after the first contact (guiding principle).
3. **Shard count `S`:** 4 is an estimate. To be reviewed as soon as there are measurements of the distribution.
4. **WebTransport/WebSocket over 443** for corporate networks: only provided for in phase 2, implementation when needed.

## 13. Gate phase 2

*"Nodes at different sites behind NAT find each other without fixed addresses; failure of an entry node or tracker does not block existing nodes; private and open network stay separate."*

Testing:

1. **In-process** (Go tests, simulated network): entry via bootstrap and DHT, capability search via shards, relay fallback with limits, peer cache after restart, no contact between PSK and open host, rate limits.
2. **On real hardware:**
   - private network across two sites, **variant A:** bridge node in a Windows VM (the hypervisor's NAT, VPN to the second site), without an opened port. Tests Windows as a platform at the same time.
   - private network across two sites, **variant B:** helper on a server on the internet, as soon as available; the same server as the first helper of the open test network. Optionally a tracker (`swarmainet tracker`) on the same server.
   - Switching off helper and tracker: already connected nodes keep finding each other via peer cache, peer exchange and DHT. In variant A with only one bridge node, both sites keep working separately.
   - an open test network from the same devices with separate identities
   - Measurement: share of peers connected directly, via hole punching and via relay, time to the first connection, task via relay successful

## 14. Result on hardware (29 Oct 2026)

**Setup** (corresponds to variant B from § 13 with a helper behind a port forwarding instead of on a server; guide `deploy/raspberry-pi/`):

| Node | Connection | reachable from outside | Role |
|---|---|---|---|
| Raspberry Pi 4 | another FritzBox, public IPv4, port forwarding 4711/tcp, name via CNAME to MyFRITZ! | yes | helper (relay, DHT server), no models |
| VM | FritzBox at home, no forwarding | no | provider (2 models on an AI server in the home network) |
| Notebook | mobile router with an O2 SIM (CGNAT) | no | coordinator without its own model |

All three in the private swarm (PSK), the Pi admitted via `invite`/`join`, its public name via `announce`.

**Results:**

| Test | Result |
|---|---|
| VM → Pi via the internet | direct, 21 ms; relay slot reserved |
| Notebook → Pi | direct (outgoing from the CGNAT) |
| VM ↔ notebook | found via peer exchange, connection via the Pi as relay, 128 ms |
| Hole punching VM ↔ notebook | did not succeed (TCP only, mobile CGNAT); connection stayed via the relay |
| Task notebook → VM | executed via the relay (pattern `single`), receipt signed by both |
| Pi failure | connections via it break off, the nodes keep running |
| Pi back on | VM directly connected and relay slot renewed within about a minute, notebook back via the relay 30 s later; without restart or intervention |

The time to the first connection at start was not measured. **Still open:** bridge node via VPN (variant A), Windows as a platform, a tracker in operation.

**Experience from commissioning** (incorporated into the guide): the Raspberry Pi Imager from the Ubuntu packages (1.8) is too old for the current Raspberry Pi OS, the customisations get lost; the Pi has no clock, early timestamps after start are wrong; after a configuration change, `systemctl restart` is needed, because the service is already running after boot; the DNS provider requires a trailing dot for the CNAME target.

## 15. Result open network on hardware (18 Nov 2026)

**Setup:** open network (`network_id: swarmainet`, without PSK), each node as a separate instance with its own identity next to the private nodes of the same devices. No mDNS, no invitation, no static peers: entry solely via the built-in DNS seed.

| Node | Connection | reachable from outside | Role |
|---|---|---|---|
| Start node | VPS, public IPv4 and IPv6, TCP and QUIC | yes | relay (`auto`), DHT server, no models; listed in the DNS seed |
| VM | FritzBox at home, no forwarding; IPv6 from outside rejected | no | provider, `accept_public_tasks`, 2 models on an AI server in the home network, no tools allowed |
| Notebook | mobile router (CGNAT for IPv4, IPv6 with packet filter) | no | coordinator without its own model |

**Results:**

| Test | Result |
|---|---|
| Entry via the DNS seed | VM connected after 1 s, relay slot immediately; notebook likewise |
| Finding a capability | notebook finds the provider via the DHT |
| Task `public` notebook → VM | executed (pattern `single`), 2.7–10.6 s depending on the model, receipt recorded |
| Task `trusted` from the notebook | does not leave the notebook ("kein passender agent verfügbar", no suitable agent available) |
| Private addresses | published neither in identify nor in the DHT |
| Hole punching VM ↔ notebook | **succeeded** via IPv6/QUIC, direct connection, 46–48 ms. Cross-check: a fresh node does not reach the notebook at the same address, and the FritzBox rejects incoming IPv6 to the VM (ICMPv6 "administratively prohibited"). On 29 Oct it failed via TCP and IPv4. |
| Start node off | existing direct connection holds, tasks keep running |
| Notebook restart with the start node switched off | connection established anyway, probably by simultaneous dialling from both peer caches (IPv6 without NAT, fixed ports); not reliable |
| Start node back on | **gap:** VM and notebook did not dial it again, because entry nodes were only dialled without any peer. Both remained an island without a relay slot and with an empty DHT table. Fixed with the rule in § 2 ("When entry nodes are dialled"). |
| Fix on hardware | island of three nodes without public reachability (VM, a second node on the VM, notebook), start node off for 40 min: after its return, both VM nodes had a relay slot and DHT partners again after 12 s, without restart or intervention. |

**Tool rights** (privacy classes, rule 6): the requester was a second node with its own identity, for which the provider is a stranger; it found it via the start node and the DHT. A proxy in front of the inference server recorded what the model at the provider actually saw. Every request contained `get_current_timestamp` and `run_command`.

| Case | Result |
|---|---|
| without `allowed_tools` | refused (HTTP 400 "kein agent mit freigabe für diese werkzeuge", no agent with permission for these tools), nothing arrives at the provider |
| `allowed_tools: [get_current_timestamp]` | the model only sees `get_current_timestamp` and calls it |
| `run_command` forced via `tool_choice` | set to `auto` before sending; the model does not see `run_command` and calls nothing |
| second round with the result of `get_current_timestamp` | answer with the time |
| history with call and result of `run_command` | refused, the history does not go to the stranger |

Observations: global IPv6 addresses are published before AutoNAT confirms them; that is necessary for hole punching. `trusted` requests without trusted peers now get a message stating the reason and HTTP 400 (implemented after the test). **Still open:** several independent start nodes, continuous operation. Discarding calls of tools that were not shown could not be provoked on hardware, the model kept to the list shown; it is tested in-process.
