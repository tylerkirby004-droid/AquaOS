# Frozen v1 compatibility contracts

The v1 contract surface is frozen for the release-candidate period. Compatible
additions may add optional fields or endpoints; incompatible changes require a
new major contract, migration window, rollback instructions, and an ADR.

| Surface | Frozen contract | Compatibility rule |
|---|---|---|
| Configuration | `schema_version: 1` | unknown fields and unsupported versions rejected |
| REST | `/api/v1`, `docs/openapi-v1.yaml` | additive optional responses only within v1 |
| MQTT | `aquaos/v1/...`, documented envelopes/topics | incompatible payload or topic changes require v2 |
| ESP32 | documented node schema v1 | unsupported schema rejected; additive JSON tolerated only as documented |
| Backup | manifest plus configuration/version checksums | restore rejects unknown paths and checksum mismatch |
| Linux | amd64 primary, arm64 portability | native systemd; no Docker dependency for Core |
| Shelly | Plug US Gen4 RPC subset | firmware versions require bench evidence before support claim |

Go package internals, Admin HTML, and diagnostics are not public compatibility
contracts. Home Assistant entity unique IDs are stable and retained discovery
cleanup is part of the integration contract.
