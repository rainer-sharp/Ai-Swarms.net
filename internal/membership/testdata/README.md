# Membership test vectors

**English** · [Deutsch](de/README.md)

`vectors.json` contains language-independent test vectors for membership networks ([specification](../../../spec/membership.md)), so that other implementations can check their bytes against them:

| Field | Contents |
|---|---|
| `root_seed_hex` | fixed Ed25519 seed of the root key |
| `root_pub` | its public key (base64url) |
| `network_id` | network ID derived from it |
| `node_peer_id` | peer ID of the node in the certificate |
| `cert_signed_bytes`, `cert_sig` | signed bytes of a certificate and the signature |
| `crl_signed_bytes`, `crl_sig` | signed bytes of a revocation list and the signature |
| `cert_json` | the certificate as it is transmitted |

The Go implementation checks them in `TestVectors` (`membership_test.go`).

**License:** like the specification, these test vectors are licensed under [CC BY 4.0](../../../LICENSE-DOCS).
