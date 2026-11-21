# Installation and first start

**English** · [Deutsch](de/installation.md)

## Requirements

- **An inference server with an OpenAI-compatible API** on every device that contributes models: Ollama, llama.cpp, vLLM, LM Studio or MLX. Devices without their own models can still submit tasks or carry the network.
- **Go 1.27** for building (version see `go.mod`). There are no prebuilt binaries yet.
- Linux is the main platform. macOS should work but has not been tested. Windows has not been tested either.

## Building

```sh
git clone https://github.com/rainer-sharp/Ai-Swarms.net.git
cd Ai-Swarms.net
go build -o swarmainet ./cmd/swarmainet
go test ./...          # optional
```

Or without cloning, directly into `~/go/bin`:

```sh
go install github.com/rainer-sharp/Ai-Swarms.net/cmd/swarmainet@latest
```

On Ubuntu without Go installed, [`deploy/linux/gruppe-beitreten.sh`](../deploy/linux/README.md#setting-up-a-node-for-a-group-network) also installs Go, but only for joining a group network.

## Setting up the first node

```sh
./swarmainet init               # creates ~/.swarmainet/config.yaml, keys and a private network of your own
$EDITOR ~/.swarmainet/config.yaml
./swarmainet run
```

The OpenAI-compatible API is then available at `http://127.0.0.1:7711/v1`, and a status page appears at `http://127.0.0.1:7711`. How to use it is described in [Usage](usage.md).

> **Note:** `init` sets up a **private network of your own** (ID `p-…`, key in `~/.swarmainet/p-….key`). Further own devices join via an [invitation](networks.md#invitations). If you want to join another network instead, run `swarmainet join '<link>'` before the node runs for the first time; the new private network then gives way. For the **open network**, use `swarmainet init -open`: other participants then see the node's peer ID and public address; it only accepts foreign tasks once you explicitly enable that.

## The most important settings

A complete, commented example is [`config.example.yaml`](../config.example.yaml); all network keys are listed in [Configuration](configuration.md).

1. **Add models:** under `backends[].models`, add the models with family (`family`), skills (`skills`) and strength (`strength`, 1–10). For a verifier, the swarm needs at least two **different model families**, e.g. `qwen` and `llama`.
2. **Offer models:** set `node.provide: true` and `offer: true` on the models if the device should compute for others.
3. **Establish trust:** how devices trust each other depends on the network type, see [Network types](networks.md).
4. **Find devices:** on the LAN, devices find each other via mDNS. Otherwise add an address from `swarmainet status` under `node.static_peers`. The node remembers successfully connected peers in `~/.swarmainet/peers-<network_id>.json` and dials them first after a restart.
5. **Mind the time windows:** in the default configuration, a node only accepts foreign tasks on weekdays from 22:00 to 07:00 and at weekends. To try things out, set `resources.schedule.windows: []`.
6. **Tools:** mark models that can handle tool calls with `tools: true`. They then serve coding agents, see [Usage](usage.md#coding-agents-and-tools).

The resource manager under `resources` limits how much a node computes for others: parallel tasks, GPU utilisation and minutes, temperature, RAM reserve and optionally surplus from a solar (PV) system via Home Assistant. Your own requests always take priority.

## Continuous operation

- **Linux:** as a systemd user service, see [`deploy/linux/`](../deploy/linux/README.md).
- **Docker:** for helper nodes without models, see [Network types – helper nodes with Docker](networks.md#helper-nodes-with-docker).
- **Raspberry Pi** as a helper behind a port forwarding: [`deploy/raspberry-pi/`](../deploy/raspberry-pi/README.md).
- **VPS** with start node and helper: [`deploy/vps/`](../deploy/vps/README.md).

## Updating

Rebuild and restart the node. Configuration and keys under `~/.swarmainet/` are kept. The protocol is still at version 0.1; there may be incompatible changes between development states, so all nodes of a network should run the same version.
