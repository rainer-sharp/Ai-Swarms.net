# AI-Swarms.net

**English** · [Deutsch](de/README.md)

**Local AI models form a team.** AI-Swarms.net connects the computers on which you run models with Ollama, llama.cpp, vLLM or LM Studio into a swarm. For each request it assembles a team: one model drafts, a model from a different family reviews, and revisions happen only if flaws are found. Your applications talk to the swarm as if it were a single OpenAI endpoint.

> **Status: research prototype.** The local swarm as well as private networks, group networks and the open network run on real hardware across multiple sites. Whether the swarm delivers better answers than the best single model has not yet been shown. Protocol version 0.1; incompatible changes are possible. Details: [Status](docs/status.md).

> The documentation and specification are available in English and German: each German version sits under the same file name in a `de/` subfolder, starting with [de/README.md](de/README.md).

## What it does

- **One endpoint for all models:** OpenAI-compatible API at `http://127.0.0.1:7711/v1` for Open WebUI, IDEs and coding agents, including tool calls.
- **Collaboration by pattern:** best specialist, several models in parallel with synthesis, or a draft reviewed by a different model family. The response shows which model contributed what.
- **Three kinds of network:**
  - **Private network** for your own devices, closed off with a shared key.
  - **Group network** for clubs, teams and companies: a certificate per node, members admitted via invitation link and removed individually within seconds, without a central server.
  - **Open network** for everyone, with restricted rights for strangers.
- **Across sites:** nodes behind NAT, CGNAT or DS-Lite find each other without open ports, directly, via hole punching or through a helper.
- **Your data stays with you:** for each request you decide who may see it (local only, trusted devices, public). You decide per network which tools foreign models get to see.
- **Your hardware stays yours:** your own requests take priority. A node only computes for others if you switch it on, and only within time windows, under GPU and temperature limits, or when there is surplus solar power.

## Quick start

Requirements: a running inference server with an OpenAI-compatible API, e.g. Ollama. From version 0.4.0, [releases](https://github.com/rainer-sharp/Ai-Swarms.net/releases) contain the program ready to run for Linux, macOS and Windows ([installation](docs/installation.md#prebuilt-binaries)); building it yourself needs Go 1.27:

```sh
git clone https://github.com/rainer-sharp/Ai-Swarms.net.git
cd Ai-Swarms.net
go build -o swarmainet ./cmd/swarmainet

./swarmainet init                    # creates ~/.swarmainet/config.yaml and a private network of your own
$EDITOR ~/.swarmainet/config.yaml    # add your models under backends
./swarmainet run
```

An example of the `backends` section with two model families is in [`config.example.yaml`](config.example.yaml). Then:

```sh
curl -s http://127.0.0.1:7711/v1/chat/completions -H 'Content-Type: application/json' \
  -d '{"model":"swarmainet","messages":[{"role":"user","content":"What is a prime sieve?"}]}'
```

Or in Open WebUI, enter the URL `http://127.0.0.1:7711/v1` under *Settings → Connections* and select the model `swarmainet`.

> `init` sets up a **private network of your own** with a fresh key: only your devices that you invite can see the node. For the open network, run `swarmainet init -open` instead; there the node is visible to others with its peer ID and address, but accepts no foreign tasks until you enable that ([network types](docs/networks.md)).

**Connecting more devices:** on a running node, run `swarmainet invite -with-key`, and on the new device `swarmainet join '<link>'`. On a LAN the devices then find each other on their own; for multiple sites see [Network types](docs/networks.md#several-sites).

## Documentation

| | |
|---|---|
| [Installation](docs/installation.md) | Building, setup, continuous operation on Linux and Docker |
| [Usage](docs/usage.md) | Model names and patterns, Open WebUI, coding agents, commands, diagnostics |
| [Network types](docs/networks.md) | Private network, group network, open network, multiple sites, helpers |
| [Configuration](docs/configuration.md) | All keys and files |
| [Status](docs/status.md) | What has been tested, what is missing |
| [Concept](docs/concept.md) | Vision, decisions, roles, trust, roadmap |
| [Specification](spec/README.md) | Protocol, network layer, security model |
| [Changelog](CHANGELOG.md) | Changes per version |

## Security

AI-Swarms.net has not been audited. Share confidential content only with your own devices or members of a group network. In the open network, foreign nodes see whatever you send as `public`. Please do not report vulnerabilities publicly; follow the process described in [SECURITY.md](SECURITY.md).

## Contributing

Bug reports, measurements with other models and experience from other networks are welcome, see [CONTRIBUTING.md](CONTRIBUTING.md). Code contributions require the [contributor agreement](CLA.md) (in German).

## License

AI-Swarms.net is free software under the [GNU AGPL-3.0](LICENSE). Anyone who modifies and distributes it, or offers it as a network service, must disclose the source code. A [commercial license](COMMERCIAL.md) (in German) is available for use in closed products.

The documentation and specification are licensed under [CC BY 4.0](LICENSE-DOCS); for the distinction see [COMMERCIAL.md](COMMERCIAL.md#dokumentation-cc-by-40) (in German).

The benchmark tasks in `bench/` are taken from HumanEval and GSM8K (© OpenAI, MIT).
