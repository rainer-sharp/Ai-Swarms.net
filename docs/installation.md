# Installation and first start

**English** · [Deutsch](de/installation.md)

## Requirements

- **An inference server with an OpenAI-compatible API** on every device that contributes models: Ollama, llama.cpp, vLLM, LM Studio or MLX. Devices without their own models can still submit tasks or carry the network.
- **Go 1.27** only for building from source (version see `go.mod`); from version 0.4.0, [prebuilt binaries](#prebuilt-binaries) are available.
- Linux is the main platform. macOS should work but has not been tested. Windows has not been tested either.

## Prebuilt binaries

From version 0.4.0, every [release](https://github.com/rainer-sharp/Ai-Swarms.net/releases) contains archives for Linux, macOS and Windows, each for `amd64` and `arm64` (`arm64`, for example, for a Raspberry Pi 4 or 5 with a 64-bit system or a Mac with Apple silicon). Each archive contains the program, the licences, `README.md`, `CHANGELOG.md` and `config.example.yaml`. On Linux:

```sh
v=0.4.0
curl -LO https://github.com/rainer-sharp/Ai-Swarms.net/releases/download/v$v/swarmainet_${v}_linux_amd64.tar.gz
curl -LO https://github.com/rainer-sharp/Ai-Swarms.net/releases/download/v$v/SHA256SUMS
sha256sum -c SHA256SUMS --ignore-missing
tar -xzf swarmainet_${v}_linux_amd64.tar.gz
install -m 755 swarmainet_${v}_linux_amd64/swarmainet ~/.local/bin/
swarmainet version
```

- **macOS:** the archives are named `…_darwin_arm64.tar.gz` (Apple silicon) or `…_darwin_amd64.tar.gz` (Intel); check with `shasum -a 256 -c SHA256SUMS --ignore-missing`. The program is not signed; macOS blocks it until the quarantine flag is removed: `xattr -d com.apple.quarantine swarmainet`.
- **Windows:** a ZIP archive with `swarmainet.exe`; check with `Get-FileHash swarmainet_….zip` in PowerShell and compare with `SHA256SUMS`. SmartScreen may warn about an unknown publisher.
- **Reproducible:** the archives are built by [`scripts/build-release.sh`](../scripts/build-release.sh) on GitHub. Checking out the tag and running the script with the Go version from `go.mod` should produce byte-identical archives.

`swarmainet version` shows the version; a running node also reports it in `swarmainet status` and at start-up. A build of your own reports the Git commit instead (`dev-1a2b3c4d`).

## Building from source

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
- **Docker:** for helper nodes without models, see [Network types – helper nodes with Docker](networks.md#helper-nodes-with-docker). For a gateway that splits long recordings, the image variant with `ffmpeg`: `docker build --target audio -t swarmainet:audio .` ([transcription](usage.md#transcription)).
- **Raspberry Pi** as a helper behind a port forwarding: [`deploy/raspberry-pi/`](../deploy/raspberry-pi/README.md).
- **VPS** with start node and helper: [`deploy/vps/`](../deploy/vps/README.md).

## Updating

Rebuild and restart the node. Configuration and keys under `~/.swarmainet/` are kept. The protocol is still at version 0.1; there may be incompatible changes between development states, so all nodes of a network should run the same version.
