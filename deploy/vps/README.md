# Start node and demo network on a VPS

**English** · [Deutsch](de/README.md)

A small VPS with a fixed IPv4 and IPv6 (e.g. netcup VPS nano: 2 vCPU, 2 GB RAM) carries several separate helpers, each in its own container:

| Container | Network | Port | Gateway | Name | Task |
|---|---|---|---|---|---|
| `seed` | open network (`swarmainet`, without a key) | 4712/tcp + 4712/udp (QUIC) | 7712 | `seed.ai-swarms.net` | Start node: entry via the DNS seed, DHT server, relay. It becomes dispensable as the network grows ([spec/p2p-layer.md](../../spec/p2p-layer.md) § 6.1). |
| `private` | your own private swarm (ID and key via invitation) | 4711/tcp | 7711 | `helper.ai-swarms.net` | Helper through which the sites of your own swarm connect |
| `demo` (optional) | group network (membership network `m-…`, no network key) | 4713/tcp + 4713/udp (QUIC) | 7713 | `demo.ai-swarms.net` | Helper of a group: carries the network, sees no tasks |

Separate containers instead of one node with several networks, so that each helper can be set up, rebuilt or removed independently and each network key is only stored in its own data directory. All of them use the host network and therefore need different ports and gateway ports.

> The VPS knows the key of your own private swarm and is a trusted member there. For your own swarm on your own server that is acceptable as long as it is well secured. In a **group network**, by contrast, it only holds a certificate with role `helper`: it gets no tasks and only sees who is online and how much is transferred ([membership networks](../../spec/membership.md)).

## 1. Ordering and setting up the VPS

1. When ordering, choose **Debian 13** as the operating system and add an SSH key.
2. Log in and update:
   ```sh
   ssh root@<IPv4 of the VPS>
   apt update && apt full-upgrade -y
   apt install -y unattended-upgrades git ufw
   ```
3. Create a user of your own, SSH only with a key and without root:
   ```sh
   adduser admin && usermod -aG sudo admin
   mkdir -p /home/admin/.ssh && cp ~/.ssh/authorized_keys /home/admin/.ssh/
   chown -R admin:admin /home/admin/.ssh && chmod 700 /home/admin/.ssh
   printf 'PasswordAuthentication no\nPermitRootLogin no\n' > /etc/ssh/sshd_config.d/10-haerten.conf
   systemctl reload ssh
   ```
   Before logging out, check in a second terminal that `ssh admin@<IPv4>` works.
4. **Firewall:** only open SSH and the network ports.
   ```sh
   ufw allow 22/tcp
   ufw allow 4711/tcp      # private
   ufw allow 4712/tcp      # seed
   ufw allow 4712/udp
   ufw allow 4713/tcp      # demo, only if used
   ufw allow 4713/udp
   ufw enable
   ```
   If the provider additionally offers a firewall of its own (at netcup in the Server Control Panel), open the same ports there or leave it switched off.
5. **Docker:**
   ```sh
   sudo apt install -y docker.io docker-compose docker-buildx
   sudo usermod -aG docker admin          # then log in again
   ```

## 2. Getting the template

```sh
git clone https://github.com/rainer-sharp/Ai-Swarms.net.git
cd Ai-Swarms.net/deploy/vps
mkdir data-open data-private data-demo
cp open.yaml data-open/config.yaml
cp private.yaml data-private/config.yaml
cp demo.yaml data-demo/config.yaml
```

The containers run as user `nonroot` (UID 65532); it must be allowed to write to the data directories:

```sh
sudo chown -R 65532:65532 data-open data-private data-demo
```

Instead of `git clone`, this also works: build the image on another computer and transfer it (`docker save swarmainet:latest | gzip | ssh vps 'gunzip | docker load'`), then only copy the files from `deploy/vps/` and use `docker compose up -d --no-build …`. This way the VPS needs no access to the repository.

## 3. Entering names

The configurations list the names `seed.ai-swarms.net`, `helper.ai-swarms.net` and `demo.ai-swarms.net` under `announce`. If you use other names, change them there (`sudoedit data-open/config.yaml`).

## 4. Starting the start node

```sh
SWARMAINET_VERSION=$(git describe --tags --always) docker compose build seed
docker compose up -d seed
docker compose exec seed swarmainet status -config /data/config.yaml | grep -m1 peer_id
```

## 5. Helper of your own private swarm

Create an invitation on a node of the swarm (`swarmainet invite -with-key`) and join with it on the VPS. The link contains the network key: do not leave it in files that are backed up or shared.

```sh
docker compose run --rm --no-deps private join -config /data/config.yaml '<link>'
docker compose up -d private
docker compose exec private swarmainet status -config /data/config.yaml | grep -m1 peer_id
```

Then enter the helper on all nodes of the swarm:

```yaml
node:
  static_peers:
    - /dns/helper.ai-swarms.net/tcp/4711/p2p/<peer ID of private>
```

## 5a. Helper of a group network (optional)

The network is created on a computer of the group, not on the VPS. The root key never belongs here.

```sh
# computer of the group
swarmainet network create                # names the ID m-… and the root key

# VPS: enter ID and root key in data-demo/config.yaml, then
docker compose --profile demo run --rm --no-deps demo member request -dir /data -net m-…

# computer of the group: issue for the peer ID shown, copy the file to the VPS
swarmainet member issue -net m-… -role helper -name vps-helper -out helper.cert <peer-id>

# VPS: provide the file for the container, install it, start
sudo install -o 65532 -g 65532 -m 600 helper.cert data-demo/helper.cert
docker compose --profile demo run --rm --no-deps demo member install -dir /data -root <root key> /data/helper.cert
docker compose --profile demo up -d --no-build --no-deps demo
```

Then set `node.static_peers` on the issuer to the helper. The invitation for all further nodes then names it on its own: `swarmainet member invite -net m-…`.

## 6. DNS records

At the domain provider, with the fixed IPv4 and IPv6 of the VPS:

```
seed.ai-swarms.net.            300  IN  A     <IPv4 of the VPS>
seed.ai-swarms.net.            300  IN  AAAA  <IPv6 of the VPS>
helper.ai-swarms.net.          300  IN  A     <IPv4 of the VPS>
helper.ai-swarms.net.          300  IN  AAAA  <IPv6 of the VPS>
demo.ai-swarms.net.            300  IN  A     <IPv4 of the VPS>   (only with demo)
demo.ai-swarms.net.            300  IN  AAAA  <IPv6 of the VPS>   (only with demo)
_dnsaddr.seed.ai-swarms.net.   300  IN  TXT   "dnsaddr=/dns/seed.ai-swarms.net/tcp/4712/p2p/<peer ID of seed>"
_dnsaddr.seed.ai-swarms.net.   300  IN  TXT   "dnsaddr=/dns/seed.ai-swarms.net/udp/4712/quic-v1/p2p/<peer ID of seed>"
```

The two TXT records are the **DNS seed**: every new node in the open network asks `_dnsaddr.seed.ai-swarms.net` and thus finds the start node. If further public helpers are added later, they get TXT records of their own here; if the VPS goes away, its records are removed. The software does not have to be shipped again for this.

> **Global Village (domreg):** the quick mode creates the **whole zone anew**. Existing records, e.g. `helper` (CNAME to MyFRITZ!) and the main domain, must be specified there as well, otherwise they are gone. Better add only the lines above in expert mode. CNAME targets need the trailing dot.

Checking:

```sh
dig +short A seed.ai-swarms.net
dig +short TXT _dnsaddr.seed.ai-swarms.net
```

## 7. Checking

- **Ports from outside** (from another computer): `nc -vz seed.ai-swarms.net 4712`, `nc -vz helper.ai-swarms.net 4711` and, if applicable, `nc -vz demo.ai-swarms.net 4713`.
- **Start node:** a node in the open network with the default configuration (without `dns_seeds`) finds it on its own. Directly: `swarmainet ping /dns/seed.ai-swarms.net/tcp/4712/p2p/<peer ID of seed>`.
- **Status on the VPS:** `docker compose exec seed swarmainet status -config /data/config.yaml`, there `relay_service: true`, `dht.mode: server`, after some time `reachability: public`.

## 8. Admitting and removing members

This happens on the group's issuer, not on the VPS: `swarmainet member invite` and `swarmainet member revoke` ([network types](../../docs/networks.md#membership-network-groups-and-companies)). The helper gets the revocation list on its own and disconnects revoked nodes.

## 9. Operation

- **Logs:** `docker compose logs -f seed` or `demo`
- **Updating:** `git pull && docker compose up -d --build`
- **Restart after changing a configuration:** `docker compose restart seed` or `private` or `demo`
- **Ending the group network helper:** `docker compose rm -sf demo`, `sudo rm -rf data-demo`, remove the DNS record `demo`, `member revoke` on the issuer. The start node keeps running unaffected.
- **Backup:** the peer IDs depend on `data-open/identity.key`, `data-private/identity.key` and `data-demo/identity.key`. With them, a new VPS can take over the same peer IDs, so that the DNS seed stays valid.

## 10. Security

- Only SSH (with a key) and the network ports are reachable from outside. The gateways listen on `127.0.0.1` only.
- Both helpers do not compute and accept no tasks.
- In the open network, the start node as a relay sees who exchanges how much with whom, but no content (end-to-end encrypted).
- The VPS knows the key of your own swarm and is a trusted member there. If it is compromised, that network is compromised: generate a new key and distribute new invitations. In the group network, by contrast, it is sufficient to revoke its helper.
