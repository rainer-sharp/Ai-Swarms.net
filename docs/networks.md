# Network types

**English** · [Deutsch](de/networks.md)

SwarmAiNet knows three kinds of networks. A node can belong to several at once.

| | Private network | Membership network | Open network |
|---|---|---|---|
| For | your own devices | groups, clubs, companies | everyone |
| Access | shared key (PSK) | certificate per node | free |
| Remove individuals | no, only a new key for everyone | yes, via revocation list within seconds | – |
| Trust | all members | admitted members, not helpers | nobody except `trusted_peers` |
| Tools without a list | all | none | none |
| Across sites | with your own helper | with your own helper that cannot read along | via foreign relays and DHT |
| Tested on hardware | yes | yes, two sites | yes, two sites |
| Network ID | free, e.g. `p-3fa91c07` | `m-…` derived from the root key | `swarmainet` |

## Private network: own devices

All devices with the same key form a closed network and are considered trusted. Whoever does not have the key does not even get through the connection handshake.

```sh
swarmainet psk ~/.swarmainet/swarm.key
```

Copy the file to all devices and enter it under `node.network_key_file`; a network ID of its own (`node.network_id`, e.g. `p-3fa91c07`) separates the network from others. It is easier via an [invitation](#invitations). Private networks run over TCP only, because libp2p does not support the key for QUIC.

Alternatively, individual devices can be trusted without a shared network: enter their peer IDs under `node.trusted_peers`.

### Invitations

```sh
swarmainet invite -with-key [-peer <address>] [-net <network>] [-qr]   # on a running node
swarmainet join '<link>'                                         # on the new device
```

`invite` takes the addresses of the running node (without loopback; in the open network only public ones, with `-lan` also local ones), with `-peer` further ones, e.g. those of a helper, plus the configured trackers. `-with-key` embeds the network key: **whoever has the link is a member.** So only pass the link on confidentially. Without the key in the link, it is given to `join -key FILE`. With `-qr`, the link additionally appears as a QR code in the terminal, e.g. to read it with a phone; with a key, the same applies to the code as to the link: do not photograph it, delete it from the phone after the transfer.

`join` creates the configuration if needed, stores the key as `~/.swarmainet/<network_id>.key` (permissions 0600), enters addresses under `node.bootstrap` and trackers under `node.trackers` and keeps the comments in the file. If the node already belongs to a network, the new one is added under `node.networks` with the next free port.

### Several sites

Because of the key, a private network cannot use foreign relays. If the devices are at different sites behind NAT, it needs **a helper node of its own** that all sides can reach:

- **Helper on the internet:** a helper node on a server with a public address, e.g. via [Docker](#helper-nodes-with-docker). All sites connect to it outgoing. Guide for a VPS with firewall and DNS: [`deploy/vps/`](../deploy/vps/README.md).
- **Helper behind a port forwarding:** e.g. a Raspberry Pi behind a FritzBox and a second router, isolated from the home network, reachable via a DNS name. Step-by-step guide: [`deploy/raspberry-pi/`](../deploy/raspberry-pi/README.md).
- **Bridge node:** a node at a place that both sides can reach **on their own**, e.g. a VM with a VPN to the other site. It does not need to be reachable itself, no port is opened. There set `node.helper: true` (only relay and DHT server, without models) or `node.relay_service: true` (in addition to its own models) and enter at least one node of each side under `node.static_peers`.

The other nodes automatically reserve a relay slot at the helper, find the other side via peer exchange and reach it through the helper. As soon as a direct path is possible, the node replaces the relay connection with a direct one. Every node re-establishes expired relay connections (limit 10 min or 8 MiB) on its own. A single helper is a single point of failure between the sites; a second one fixes that.

## Membership network: groups and companies

A membership network replaces the shared key with a **certificate per node**. Individuals can be admitted and removed without touching the others, and a helper on the internet carries the network without seeing or accepting tasks. It needs neither a certificate authority nor a central server. Specification: [`spec/membership.md`](../spec/membership.md).

```sh
swarmainet network create                      # root key, then store it offline
swarmainet member request -net m-…             # on the future issuer
swarmainet member issue -net m-… -role issuer <peer-id>
swarmainet member install -root <key> FILE

swarmainet run                                 # the issuer must be running for invitations
swarmainet member invite -net m-… [-qr]        # link: single use, at most 24 hours
swarmainet join '<link>'                       # on the new node
```

On Ubuntu, [`deploy/linux/gruppe-beitreten.sh`](../deploy/linux/README.md#setting-up-a-node-for-a-group-network) takes care of all steps after `git clone` for a new member, including installing Go.

**Roles:** `issuer` admits, `member` gives and receives tasks, `helper` only carries the network. A helper is invited with `member invite -role helper`. An invitation link is valid once and names the issuer; the one-time token only goes to the issuer, not to the helper.

**Removing:** `swarmainet member revoke -net m-… <peer-id|name>` on the issuer. The running node distributes the revocation list within seconds, everyone disconnects the revoked node, and running tasks from it are aborted. Whoever is not reachable at the moment learns about it at the next connection. The root revokes from a removable medium with `member revoke -root-key FILE …`, followed by `member import-crl` on a running node.

**Validity:** members and helpers are valid for 30 days and renew themselves from half of that time on if an issuer is reachable. An issuer therefore has to be online at least every 15 days, at the same time as the member. Issuer certificates (365 days) are renewed by the root manually; `swarmainet status` warns 7 days in advance.

**Limits:** membership networks run without DHT, trackers and DNS seeds and are meant for up to about 64 nodes. They need a correct clock, because certificates expire.

## Open network

Without a network key and without membership, a node is in the open network (`network_id: swarmainet`). No foreign peer is trusted there:

- Tasks with `X-SwarmAiNet-Privacy: public` may go to foreign nodes, `trusted` and `reduced` only to `trusted_peers`.
- A node only accepts foreign tasks with `resources.accept_public_tasks: true`.
- Foreign models only see tools from `allowed_tools`, none without a list.
- If suitable providers are missing for a `public` task, the coordinator searches for them via the DHT.

**Protection in the open network:**

- **AutoNAT** checks the node's own reachability (`reachability` in the status, per address family under `reachability_by_family`). Unreachable nodes reserve relay slots, reachable ones automatically become DHT servers.
- **No internal addresses** (LAN, loopback) in identify, DHT, peer exchange and tracker. For a test network on the LAN, `node.announce_private: true` lifts this.
- **Rate limits** for foreign peers: 10 tasks per minute and peer, 60 in total; 6 capability and peer exchange requests each per minute and peer.
- **Diversity filter** of the DHT routing table against eclipse attacks.
- **Relay automatically:** publicly reachable nodes relay connections of others (`node.relay_service: auto`, with conservative limits, can be disabled with `false`). This way the network carries itself like BitTorrent, even if the start node goes away.
- **Entry via DNS seeds:** a new node without peers asks the built-in seed `seed.ai-swarms.net` (TXT records under `_dnsaddr.seed.ai-swarms.net`) for entry nodes; after that, peer cache, peer exchange and DHT carry it. It asks again if its DHT routing table becomes empty or it has no relay slot behind NAT, e.g. after the start node was down. Own seeds with `node.dns_seeds`, disable with `[none]`.
- **Port forwarding via UPnP/NAT-PMP** (`node.port_mapping: true`, opt-in) makes a node behind a home router reachable without configuring the router manually. The forwardings expire within a minute after the node ends. On a FritzBox, "Allow automatic port forwarding" must be set for the device; that, however, allows every program on this device to open ports. It does not help with DS-Lite and CGNAT.

## Several networks at once

Further networks are listed under `node.networks`, each with its own port, its own DHT and by default its own peer ID (`identity: separate`), so that participation in a private network cannot be recognised in the open one. Backends, resource manager and receipts are shared; `provide`, `accept_public_tasks`, `trackers`, `allowed_tools`, `dns_seeds` and others apply per network and take the values of the first one if not specified. Tasks of the classes `trusted` and `reduced` only go to peers of private networks, membership networks or to `trusted_peers`; `public` may go into any network. Example in [`config.example.yaml`](../config.example.yaml); `ping` and `invite` choose the network with `-net`.

## How nodes find each other

A node finds others via peer cache, mDNS, invitation, `bootstrap` addresses and trackers; after that, peer exchange (`node.pex`) and a Kademlia DHT per network (not in membership networks) carry it. Connections are made directly, via hole punching or via a relay. No node needs an open port for this as long as there is a reachable helper somewhere. Specification: [`spec/p2p-layer.md`](../spec/p2p-layer.md).

### IPv6 and DS-Lite

By default, nodes also listen on IPv6 (`/ip6/::/tcp/4711`, in the open network additionally QUIC). Public IPv6 addresses are announced, ULA and link-local addresses only in the private network.

**Behind DS-Lite or CGNAT**, the connection shares an IPv4 address with others; via IPv4 a node is then never reachable from outside, not even with port forwarding or UPnP. It still works: outgoing it reaches helpers, relays and DHT via IPv4 and IPv6, and via a relay slot it is reachable for everyone. Between two IPv6-capable sites, hole punching usually succeeds because IPv6 does not rewrite ports; this was confirmed on hardware with QUIC. A node behind DS-Lite only becomes reachable without a relay via IPv6, with an IPv6 forwarding for port 4711 in the router's firewall. `swarmainet status` shows this under `reachability_by_family` (e.g. `ip4: private`, `ip6: public`), and `hints` explains it.

**Privacy:** if the operating system derives the IPv6 address from the MAC address (EUI-64, recognisable by `ff:fe` in the middle), it is a permanent device identifier in the open network. With NetworkManager, `ipv6.addr-gen-mode stable-privacy` helps.

### Tracker

`swarmainet tracker` runs an address mailbox for nodes without fixed addresses. The default is `127.0.0.1:7720` behind a TLS reverse proxy (`-trust-proxy` then takes the sender IP from it); with `-tls-cert`/`-tls-key` it listens via HTTPS itself. Records are kept in memory only and expire after an hour. Nodes with `node.trackers: [https://…]` deposit their addresses there every 20 minutes and fetch those of others as long as they have few peers. In a private network everything is encrypted: the tracker sees neither the network ID nor members or addresses. Nodes only accept trackers via HTTPS (`http` only for localhost).

## Helper nodes with Docker

A helper node carries the network (entry, DHT server, relay), needs no GPU and is suitable for a server with a public address. Template: [`deploy/docker/`](../deploy/docker/compose.yaml).

```sh
cd deploy/docker
mkdir data && cp helper.yaml data/config.yaml     # enter the network ID
cp ~/.swarmainet/swarm.key data/swarm.key         # key of the private network
sudo chown -R 65532:65532 data                    # the container runs as nonroot
docker compose up -d --build
docker compose exec helper swarmainet status -config /data/config.yaml
docker compose exec helper swarmainet invite -config /data/config.yaml -with-key
```

The container uses host networking (`network_mode: host`) so that reachability and IPv6 are correct; incoming, only 4711/tcp needs to be open (open network additionally 4712/tcp+udp, see template). Optionally a tracker runs alongside: `docker compose --profile tracker up -d`. A named volume works instead of a directory too. The image alone: `docker build -t swarmainet .`

For the helper of a membership network, see [`deploy/vps/`](../deploy/vps/README.md), section on the group network.
