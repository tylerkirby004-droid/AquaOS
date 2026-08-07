# AquaOS scoped threat model

This model covers the v0.9 attack surface. A high or critical unresolved finding
blocks release unless this document names an owner and an explicit remediation.

| Threat | Mitigation | Evidence | Owner/status |
|---|---|---|---|
| Malicious LAN/API client | bearer authentication, role authorization, bounded bodies and mutation rate | `internal/api/*_test.go` | Core/mitigated |
| Compromised Home Assistant or AI | MQTT ACL privilege separation; optional integrations cannot issue Core commands | `internal/mqtt/acl_test.go` | Integrations/mitigated |
| Malicious MQTT publisher | versioned validation, idempotency, ACL-denial cases, bounded payloads | MQTT codec/ACL tests | Messaging/mitigated |
| Admin browser abuse | loopback default, no cookies, same-origin mutations, CSP, no-store, anti-framing headers | `internal/admin/server_test.go` | Operations/mitigated |
| Policy bypass | HTTP and Admin handlers call application-service interfaces only; adapters are not exposed | API/Admin service-call tests and package boundaries | Core/mitigated |
| Secret leakage | external token files, redacted diagnostics, no browser persistence, secret scanning | config/API tests and CI | Security/mitigated |
| Supply-chain compromise | minimal modules, vulnerability/license/static/secret CI gates, signed upgrade artifacts | CI and operations signature tests | Release/mitigated |
| Disk/archive exhaustion | request limits, strict backup entry allowlist, checksums, bounded telemetry queues | API/operations/storage tests | Operations/mitigated |
| Denial of service | server timeouts, header/body bounds, bounded token maps, bounded queues | API and subsystem tests | Core/mitigated |
| Control VM or Proxmox host loss | UPS/startup/restore planning plus independent physical equipment safeguards | physical recovery evidence | Operations/**release-blocking evidence pending** |

Accepted residual risks are limited to the explicitly documented single-host
failure domain and bearer-token administration on a trusted management path.
Internet exposure of Core or Admin is unsupported. Prompt 8 hardware bench and
Prompt 12 clean-VM/replacement-host evidence remain release-blocking.
