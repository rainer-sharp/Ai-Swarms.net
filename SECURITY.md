# Security

**English** · [Deutsch](de/SECURITY.md)

## Reporting vulnerabilities

Please do **not** report vulnerabilities in public issues; report them confidentially via GitHub: *Security → Report a vulnerability* in this repository. State the affected version (commit), the network type and, if possible, steps to reproduce. You will get a reply as soon as the report has been reviewed.

## What counts as a vulnerability

- A node without a key or valid certificate gets into a private network or a group network.
- A revoked member or a helper can submit, accept or read tasks.
- Requests reach nodes that must not see them according to their privacy class.
- Foreign models see or call tools that are not allowed for their network.
- The gateway can be reached from another computer.
- A foreign node can crash another node or use its resources beyond the limits.

Known limitations are described in the [threat model](spec/threat-model.md) and under [Status](docs/status.md#known-gaps).

## Operating notes

- SwarmAiNet has not been independently audited. Confidential content belongs only in private networks or group networks.
- Key files under `~/.swarmainet/` and invitation links containing a key are credentials. Whoever has them is a member.
- Tools are executed by your application, not by the model. A model that sees tools can get your application to call them. Only allow tools for foreign networks whose invocation is harmless.
- Keep the root key of a group network offline.
