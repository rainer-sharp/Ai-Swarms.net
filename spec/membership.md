# Membership networks (certificates instead of a network key)

**English** · [Deutsch](de/membership.md)

Status: being implemented (§ 11). Certificates, commands, admission, roles, invitations, revocation lists and renewal work. Tested in-process, passed on real hardware across two sites and a helper on the internet (§ 11); continuous operation is pending. Answers open question 1 of the [P2P layer](p2p-layer.md) § 12.

A private network with a PSK suits your own devices, but not groups and companies: whoever has the key is a member. Individuals cannot be removed, and a shared helper knows the key and would be a full member. A **membership network** replaces the shared key with a certificate per node.

**Goals**

1. Admit and remove members individually without touching the others.
2. A helper on the internet can carry the network without being allowed to read or accept tasks. If it is compromised, only it needs to be revoked.
3. No service outside the group is needed: no certificate authority and no central server.
4. QUIC and thus better hole punching, as in the open network.

**Non-goals:** a member that processes a task can read and pass on its content; a certificate does not prevent that either ([privacy classes](privacy-classes.md)). Participation as such does not stay completely invisible to strangers (§ 8).

PSK networks remain. This is a third network type, not a change to `pnet`.

## 1. Comparison of network types

| | PSK network | Membership network | Open network |
|---|---|---|---|
| Purpose | own devices | group, company | any foreign nodes |
| Access | shared key | certificate per node | free |
| Remove a member | new key for everyone | revoke individually | not applicable |
| Trust | every participant | only roles `member` and `issuer` (§ 3) | only `trusted_peers` |
| Helper can read tasks | yes, it is a member | no, role `helper` | no, relay only |
| Visible to strangers | only noise | a libp2p node, on mere dialling not which group (§ 8) | everything public |
| Transports | TCP | QUIC, TCP | QUIC, TCP |
| DHT | its own | none (§ 6) | shared |
| Who administers | nobody | issuer | nobody |

Recommendation: your own devices stay with the PSK, because it needs no administration and shows strangers the least. As soon as several people are involved, the membership network is the right choice.

## 2. Network ID and root key

`swarmainet network create` generates an Ed25519 **root key**. The ID is derived from it:

```
network_id = "m-" + first 16 hex characters of SHA-256("swarmainet/member-network/v1\n" + root_pubkey)
```

The ID is thus self-certifying: no allocating authority is needed, and nobody can unnoticed operate a second network with the same ID and a different root. The check always compares the complete key, not just the ID.

The root key SHOULD be kept offline. It is only needed to appoint or revoke issuers. If it is lost, the issuers keep working until their certificates expire; after that a new network has to be built. If it is stolen, the network counts as compromised and has to be rebuilt.

## 3. Certificates

The chain has at most two levels: root → issuer → node. Small groups MAY issue directly with the root.

```json
{
  "v": 1,
  "network_id": "m-3fa9c1d27be04a68",
  "serial": "<16 random bytes, base64url>",
  "subject": "<peer ID of the node in the network>",
  "role": "member",
  "name": "notebook-2",
  "issued_at": "2026-11-08T10:00:00Z",
  "expires_at": "2026-12-08T10:00:00Z",
  "issuer": "<peer ID of the issuer or root>",
  "sig": "<ed25519, base64url>"
}
```

What is signed is `"swarmainet/member-cert/v1\n" + JSON(certificate without sig)`. The ID in the signed content prevents a certificate from being valid in another network.

**Signed bytes:** JSON in UTF-8 without whitespace, fields in the order of the example, empty optional fields (`name`, `reason`, `serial` in revocation lists) are omitted, times as RFC 3339 in UTC. `<`, `>` and `&` are not escaped. Verification is done over the bytes rebuilt from the fields. Test vectors for other implementations: `internal/membership/testdata/vectors.json`.

| Role | Accept and give tasks (`trusted`, `reduced`) | Admit others | Relay, peer exchange | Validity (default) |
|---|---|---|---|---|
| `issuer` | yes | yes, only `member` and `helper` | yes | 365 days |
| `member` | yes | no | yes | 30 days |
| `helper` | **no** | no | yes | 30 days |

**Rules**

- `issuer` MAY only be issued by the root. A certificate MUST NOT be valid longer than that of its issuer. Issuer certificates are renewed by the root manually, because it is kept offline.
- `subject` MUST match the peer ID of the remote end. libp2p proves possession of the key when the connection is established; an additional proof is not needed.
- A node's private key never leaves the device. Certificates are only issued for its peer ID.
- As in [§ 9 of the P2P layer](p2p-layer.md), every network has its own identity. `identity: shared` is not allowed here, otherwise membership could be linked with other networks.
- Times are checked with 5 minutes of tolerance. Nodes without a reliable clock (e.g. a Raspberry Pi without RTC shortly after start) MUST wait with the check until the time is synchronised, and show that in `swarmainet status`.

**Renewal:** from half of the validity period on, a node asks reachable issuers via `/swarmainet/member-renew/0.1` for a new certificate, only peers with role `issuer`. The issuer issues it if the current one is valid and not revoked; role and name come from the verified certificate, not from a request. The node checks the result against the root and only adopts it with the same role and a later expiry, then announces it to all admitted peers. Certificates are valid for at most 365 days. Issuer certificates are renewed by the root manually. The short validity limits the damage if a revocation list does not reach a node. The price is that at least one issuer has to be online within 15 days, at the same time as the node.

## 4. Admission to the network

**With an invitation:**

```
swarmainet://join?v=2&net=<network_id>&root=<root_pubkey>&issuer=<peer-id>&peer=<multiaddr>[&peer=...]&token=<base64url>
```

1. `swarmainet member invite [-role member|helper] [-name N] [-ttl 24h]` creates a random token (32 bytes) on an issuer and only remembers its hash.
2. `swarmainet join <link>` locally creates the identity for the network and connects to the given addresses.
3. The new node sends the token **only to the peer ID `issuer`**, which libp2p proves when the connection is established. Further addresses in the link, e.g. those of a helper, are only entry points; a compromised helper must not learn the token, otherwise it could redeem the invitation itself. The issuer checks whether the token is valid and unused and issues the certificate for the new node's peer ID.

A token is valid **once** and for at most 24 hours. Unlike the PSK link, it reveals no network key and only opens one slot. If a link is intercepted, the issuer sees an additional admission, and the invited node fails visibly with "invitation already used". The intruder can then be revoked individually. The link SHOULD still be passed on confidentially.

Redemption runs via `/swarmainet/member-join/0.1`, before admission. The protocol exists on every node of a membership network, so that the protocol list does not reveal who issues; a node without issuer rights rejects every invitation. Invitations exist only for `member` and `helper`; issuers are appointed by the root via a file. If the same identity asks again, it gets a new certificate in case the first answer was lost. Only the token's hash is kept in the issuer's book.

**Issuer behind NAT:** a newcomer only reaches it via a helper, but is not yet admitted there. The relay therefore passes a non-admitted node through to peers with role `issuer`, but not to others. A node that is to accept invitations via a helper therefore needs an issuer certificate, even if it has the root key.

Whoever runs `join` MUST check the key `root` from the link against `net` and every certificate delivered by the issuer against `root`. A forged link can thus only lead into another network, but cannot fake a certificate for the real one. The link itself must therefore come from a trustworthy source.

**Without a running issuer:** `swarmainet member request -net <id>` prints the peer ID, `swarmainet member issue -net <id> <peer-id>` creates the certificate file on the device with the issuer key, `swarmainet member install -root <root_pubkey> <file>` checks and adopts it. The root key is given explicitly, because the one in the file certifies nothing.

## 5. Admission on a connection

Strangers can establish a connection, because there is no PSK in front. The separation therefore lies in the protocol:

1. After the connection is established, both exchange their certificate chain and the sequence numbers of their revocation lists via `/swarmainet/member/0.1`, the dialling side first (§ 8).
2. Checked are: chain up to the root, ID, `subject` equals peer ID, validity, revocation list, role rules.
3. Only then does the peer count as **admitted**, with its role.

| Protocol | without admission | `helper` | `member`, `issuer` |
|---|---|---|---|
| Identify, ping | yes | yes | yes |
| Relay (hop) at the helper | only to `issuer`, without reservation (§ 4) | – | yes |
| Redeem invitation | yes | yes | yes |
| Peer exchange | no | yes | yes |
| Capability announcement | no | **no** | yes |
| Tasks (`task`) | no | **no** | yes |

Every handler of these protocols MUST check admission itself before reading or answering; a forgotten handler would be a leak. The check therefore belongs in a single place through which all handlers are registered. Non-admitted peers are disconnected after 10 seconds and subject to rate limits (§ 8 of the P2P layer).

Both sides check. A coordinator only gives tasks to admitted `member` and `issuer` peers, a provider only accepts them from these. The privacy classes apply unchanged: `trusted` and `reduced` only to these roles, `public` is treated in membership networks as in the PSK network, and `local` never leaves the node.

**Tools:** in a membership network, models only see the tools from `allowed_tools`, none without a list ([privacy classes](privacy-classes.md), rule 6). Members can be other people, and the model's response triggers commands at the requester.

**Order:** the accepting side records the admission before it answers, because the peer reserves at the relay right afterwards. It only sends its own follow-up requests once the peer has closed the stream and is thus done on its side.

## 6. Finding and keeping connections

Membership networks run **without a DHT**. They are small (up to 64 nodes, full mesh as in the private network, § 7 of the P2P layer). Peer exchange and peer cache are sufficient for that, and a DHT protocol of their own would show the network ID to every stranger via identify (§ 8).

- **Entry:** addresses from the invitation, peer cache, mDNS with a service name from the hash of the ID (on the same LAN it is therefore recognisable who belongs to the same group). Helpers only exchange peers with admitted nodes.
- **Relay:** nodes only reserve a slot at admitted helpers. Foreign relays are technically possible, but not intended: they see who exchanges how much with whom.
- **Direct paths:** as in the PSK network, a node also names its own addresses in peer exchange, including private ones, but only to admitted peers. Relay connections are replaced by direct ones (§ 6 of the P2P layer).
- **Tracker:** not provided in the first version. Sealing is currently derived from the PSK, which does not exist here (§ 12).

## 7. Revocation

One **revocation list** per issuer, signed and with an increasing sequence number:

```json
{
  "v": 1, "network_id": "m-3fa9c1d27be04a68", "issuer": "<peer-id or root>",
  "seq": 17, "issued_at": "2026-11-08T10:00:00Z",
  "revoked": [{ "serial": "...", "subject": "<peer-id>", "revoked_at": "...", "reason": "device lost" }],
  "sig": "<ed25519, base64url>",
  "issuer_cert": { "...": "certificate of the issuer, missing for the root" }
}
```

What is signed is `"swarmainet/member-crl/v1\n" + JSON(list without sig and issuer_cert)`. `issuer_cert` proves the issuer and is itself checked against the root. Without this field, nobody could check the list of an issuer they do not already know.

- `swarmainet member revoke <peer-id|name> [-reason …]` increments `seq` and stores the list as a file. The running node reads it within seconds, adopts it and sends it via `/swarmainet/member-crl/0.1` to all admitted peers, through which both sides exchange their lists. A list of the root, whose key is kept offline, is imported with `member import-crl`.
- So that a lost transmission is not missing permanently, every node reconciles with a random admitted peer every minute.
- The lists are stored in `<data_dir>/crlstore-<network_id>.json`. Otherwise a revocation could be rolled back with an older list after a restart. A file whose check fails prevents this network from starting instead of silently revoking less.
- Whoever receives a newer list checks it, stores it, passes it on and **immediately** withdraws admission from affected peers. The revoked node still gets the list delivered (at most 2 seconds) so that it learns the reason, and is then disconnected; running tasks are aborted. It shows this in `swarmainet status`, admits nobody any more and no longer starts in this network. On admission, the sequence numbers are compared (§ 5), so temporarily disconnected nodes catch up as well.
- What is revoked is the peer ID, not only the serial number, so that a second certificate of the same node does not remain valid. Entries drop out of the list after 365 days, because no certificate is valid longer; they remain in the issuer's book so that it never issues again. Another issuer could issue again for such an old entry; therefore: a revoked node uses a new identity, and the issuer checks whom it admits.
- **Only the root** revokes issuers. An issuer's entry applies to `member` and `helper`, not to `issuer`, otherwise a compromised issuer could paralyse the others and thus the administration.
- The root can revoke issuers. This invalidates **all** certificates issued by that issuer. There is no exception for older certificates, because a compromised issuer could backdate `issued_at` at will. Those affected need a new invitation from another issuer, which is why companies SHOULD appoint two issuers. The revocation lists of a revoked issuer no longer apply, so that revocations from the time of the compromise also end. The price: justified revocations by this issuer for nodes of another issuer have to be issued again.
- A revocation list with a lower sequence number than the stored one is discarded, against rollback.

**Limit:** a node that no revocation list reaches trusts the revoked node until the certificate expires, i.e. at most 30 days. If everyone is online, the list reaches everyone in the full-mesh network within seconds.

## 8. What strangers see

Without a PSK, the node answers anyone attempting a connection. That is the most important disadvantage compared with the PSK network.

- **Visible to anyone who connects:** peer ID, the fact that it is a libp2p node, protocol list from identify. The protocol IDs therefore contain **no network ID**; `/swarmainet/member/0.1` is the same for all membership networks. The list reveals that the node participates in some membership network, but not in which.
- **Order of the exchange:** the dialling side sends first, and a node only answers a valid chain of its own network. Whoever merely dials a node thus retrieves neither ID nor name nor role.
- **Limit:** if a member itself dials a foreign node, that node sees its certificate. A node therefore only dials addresses from the invitation, peer cache, mDNS and the peer exchange of admitted peers. An entry from there can still point to a stranger or be outdated, e.g. after an address change. This does not completely prevent such disclosure.
- **Helpers** see who is online, from where and how much is transferred, and the names in the certificates. `name` is therefore optional and SHOULD NOT contain personal names. They do not see content; end-to-end encryption runs through the relay.
- **Attack surface:** bugs in the handlers before admission can be reached from outside. Before admission, only the exchange from § 5 is therefore open, with limited size (64 KiB) and rate limits.

## 9. Threats

| Attack | Countermeasure | Remaining risk |
|---|---|---|
| Helper compromised | role `helper` accepts no tasks and gets none; individually revocable, others unaffected | sees metadata until revocation |
| A member's device stolen | revocation, short validity; key only on the device | access until revocation; recommended: disk encryption |
| Departing member keeps a copy | revocation blocks the peer ID | content already seen remains known |
| Invitation link intercepted | token single use, 24 h, admission visible in the log | intruder until revocation; `member list` shows it |
| Issuer compromised | root revokes it, only a two-level chain, cannot appoint issuers | can admit and revoke nodes until then, including members |
| Root key stolen | offline storage | network has to be rebuilt |
| Revocation list suppressed (separation from the network) | reconciliation at every admission, short validity | up to 30 days in an isolated part |
| Certificate used in another network | ID is signed, chain ends at the network's own root | – |
| Connections from strangers as load | rate limits before admission, disconnection after 10 s | port remains recognisable from outside |
| Member collects tasks or delivers wrong results | no technical defence against reading along; verifier of another family as before | as with `trusted` |

## 10. Configuration (draft)

```yaml
node:
  networks:
    - network_id: m-3fa9c1d27be04a68
      membership:
        root: "<root_pubkey, base64url>"
        cert_file: ~/.swarmainet/member-m-3fa9c1d27be04a68.cert
        # issuer_key_file: only on issuers
      listen: [/ip4/0.0.0.0/udp/4714/quic-v1, /ip4/0.0.0.0/tcp/4714]
      allowed_tools: [get_current_timestamp]   # without the list: none
```

`membership` and `network_key_file` are mutually exclusive. Without a valid certificate the network does not start and states the reason in `swarmainet status`; other networks of the same node keep running.

## 11. Implementation and gate

1. ✅ `internal/membership`: certificates, chains, revocation lists, language-independent [test vectors](../internal/membership/testdata/README.md) (CC BY 4.0).
2. ✅ Commands: `network create`, `member invite|issue|install|request|revoke|list`.
3. ✅ Node: admission in one place (§ 5), roles instead of `n.private`, invitation, tool lists. Tests in-process, including deliberately disabled checks.
4. ✅ Distribute and store revocation lists, renewal. Tests in-process, including with a malicious issuer and a revoked node.
5. ✅ VPS template: container `demo` is the helper of a membership network without a network key.

**Gate:** *"A member can be removed without touching the others; a compromised helper can neither read nor accept tasks; a stranger who dials a node does not learn which network it belongs to."*

**In-process test:** a stranger without a certificate and with a certificate of another network gets no announcement, no peer exchange and no task; `helper` gets and accepts no tasks; a revoked node is disconnected, tasks are aborted; an expired certificate is rejected; a token works only once; an older revocation list is discarded; a temporarily disconnected node catches up on the list when reconnecting.

**Dress rehearsal on 8 Nov 2026** (VPS as `helper`, two instances on the VM without their own ports, i.e. only via the helper): invitation via the relay in 0.1 s, task via the relay successful, used link rejected and logged at the issuer, helper as coordinator without an agent, tools without a list rejected (400), revocation effective at the issuer and the revoked node after 1.8 s (of which up to 3 s polling of the file), helper adopts the list, the revoked node no longer starts in the network. The private network kept running unaffected. This is not yet a second site.

**Two sites on 8 Nov 2026:** issuer on the VM in the home network behind NAT, VPS as `helper`, a notebook on a mobile router at another site (public address of another connection). Joining by invitation and a test task from the notebook to the model on the VM via the relay successful, receipt signed by both at the issuer. Revocation: 0.41 s until loss of admission at the issuer (command until file read), 0.46 s until disconnection, helper adopts the list after 0.41 s. Found in the process: the revoked node kept redialling its former peers; fixed, but not re-tested on hardware. Not tested: direct path between the sites (no hole punching expected behind a mobile network), renewal during operation, expiry over days.

**Test on hardware:** VPS as `helper`, nodes at two sites, task via relay and directly via QUIC; revocation of a node, measured until disconnection; helper container deliberately misused as coordinator, must be rejected.

## 12. Open questions

1. **Validity:** 30 days for members is an estimate. Shorter limits the damage but requires a reachable issuer more often.
2. **More than 64 members:** a full mesh no longer works. Then a DHT would be needed without showing the ID via identify.
3. **Tracker:** a sealing key known to all members would again be a shared secret, albeit a weak one: it only protects addresses. The alternative would be sealing per recipient.
4. **Finer roles:** e.g. members that only see `reduced`, or groups within a company. Deliberately not in the first version.
5. **Migrating from PSK:** a tool that converts an existing PSK network into a membership network, or simply rebuilding with invitations. With up to 64 nodes, rebuilding is probably sufficient.
