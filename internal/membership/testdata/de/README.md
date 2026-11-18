# Testvektoren der Mitgliedschaft

[English](../README.md) · **Deutsch**

`vectors.json` enthält sprachunabhängige Testvektoren für Mitgliedsnetze ([Spezifikation](../../../../spec/de/membership.md)), damit andere Umsetzungen ihre Bytes gegen diese prüfen können:

| Feld | Inhalt |
|---|---|
| `root_seed_hex` | fester Ed25519-Seed des Wurzelschlüssels |
| `root_pub` | dessen öffentlicher Schlüssel (base64url) |
| `network_id` | daraus abgeleitete Netz-Kennung |
| `node_peer_id` | Peer-ID des Knotens im Zertifikat |
| `cert_signed_bytes`, `cert_sig` | signierte Bytes eines Zertifikats und die Signatur |
| `crl_signed_bytes`, `crl_sig` | signierte Bytes einer Sperrliste und die Signatur |
| `cert_json` | das Zertifikat, wie es übertragen wird |

Die Go-Umsetzung prüft sie in `TestVectors` (`membership_test.go`).

**Lizenz:** Diese Testvektoren stehen wie die Spezifikation unter [CC BY 4.0](../../../../LICENSE-DOCS).
