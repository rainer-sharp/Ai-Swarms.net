# Contributing

**English** · [Deutsch](de/CONTRIBUTING.md)

Thank you for your interest. SwarmAiNet is a research prototype; the following are especially helpful:

- **Bug reports** with the output of `swarmainet status`, the log (`-v`) and a description of the network (network type, NAT, IPv6). Please redact peer IDs, addresses and invitation links first.
- **Experience from other networks:** other routers, mobile networks, DS-Lite, corporate networks, Windows and macOS.
- **Measurements** with other models following [`spec/measurement-plan.md`](spec/measurement-plan.md) and [`bench/`](bench/README.md).

For larger changes, please open an issue first so we can agree on the approach.

## Build and test

```sh
go build -o swarmainet ./cmd/swarmainet
go vet ./...
go test -race ./...
```

The tests start several nodes in the same process and need neither a network nor models. `internal/node` takes about a minute.

## Releases

For maintainers: complete the version's section in `CHANGELOG.md` and `de/CHANGELOG.md`, then push an annotated tag:

```sh
git tag -a v0.4.0 -m "SwarmAiNet 0.4.0"
git push origin v0.4.0
```

The [release workflow](.github/workflows/release.yml) then tests, builds the archives for all platforms with [`scripts/build-release.sh`](scripts/build-release.sh) and attaches them with `SHA256SUMS` to the GitHub release; it takes the release text from the changelog. If the release already exists, only the files are added. The script also runs locally (`scripts/build-release.sh v0.4.0` writes to `dist/`).

## Conventions

- **Language:** identifiers in code, configuration keys and values, protocol and API names in English (e.g. `node.trusted_peers`, `tasks.pattern: draft_verify_synthesize`). Log messages, user-facing texts and code comments in German. The documentation exists in English and German: the English version sits at the main path, the German one under the same file name in the `de/` subfolder next to it (e.g. `docs/networks.md` and `docs/de/networks.md`). Please change both versions, or mention in the pull request that the other one still needs updating.
- **Protocol:** check changes to `internal/protocol` and to the libp2p protocols against [`spec/`](spec/README.md) and update the specification in the same pull request.
- **Security:** the gateway listens on localhost only; that stays so. Every new check (admission, rights, signatures) gets a test that fails when the check is removed.
- **Dependencies:** sparingly; justify new dependencies in the pull request, see below for licensing.
- **Formatting:** `gofmt`.

## License and contributor agreement

SwarmAiNet is licensed under the [GNU AGPL-3.0](LICENSE) and additionally under a [commercial license](COMMERCIAL.md) (in German). To keep both possible, we only accept contributions under the [contributor agreement](CLA.md) (in German): you keep your rights but grant the project a right of use that also covers the commercial license. Agree by commenting in your first pull request.

Contributions to the documentation (`docs/`, `spec/`, guides) are licensed under [CC BY 4.0](LICENSE-DOCS); the contributor agreement applies to them as well.

New source files start with:

```go
// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Rainer Sestig. Kommerzielle Lizenz: siehe COMMERCIAL.md.
```

New dependencies must be compatible with the AGPL-3.0 and with the commercial license, e.g. MIT, BSD, ISC, Apache-2.0 or MPL-2.0; third-party GPL and AGPL libraries are not possible.
