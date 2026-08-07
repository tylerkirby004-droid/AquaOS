# AquaOS Development Rules

The governing specification is `docs/AquaOS_Development_Bible_Edition_1.2.docx`.
Prompts 1–7 predate its post-Prompt-7 deployment pivot and must not be rewritten
solely because the production host changed.

AquaOS Core's sole supported production target is Linux amd64 on a dedicated,
single-purpose Debian appliance, running natively under systemd. ADR-023
supersedes the earlier Proxmox deployment choices. Never install AquaOS on a
shared desktop, NAS, general Docker host, or hypervisor host, and never make
Docker part of the critical control path.

The Raspberry Pi 4B is an optional display/kiosk only. Its failure, and failure
of MQTT, Home Assistant, InfluxDB, Grafana, Node-RED, AI, Internet access, or any
Admin GUI, must not prevent critical local control.

Home Assistant is the daily operational UI. The separate AquaOS Admin GUI is
non-authoritative: every mutation must use authenticated AquaOS APIs/application
services and existing validation and safety policy.

Do not obscure the dedicated appliance's physical failure domain. Production
guidance must include independent physical equipment safeguards, UPS planning,
automatic service startup, tested off-host backups/restores, and replacement-
machine recovery.

The normal end-user installation artifact is the bootable Debian appliance ISO
defined by ADR-024. Its temporary first-boot service must use per-machine
credentials, TLS, explicit safety acknowledgements, signed installer payloads,
and simulator-safe defaults. Never hide disk-erasure confirmation or commission
equipment during operating-system installation.

Never sacrifice reliability for convenience.

Never introduce breaking architectural changes without documenting why.

Always write unit tests for new packages.

Never mix business logic into MQTT handlers.

Never mix HTTP handlers with equipment logic.

Keep interfaces small.

Prefer composition over inheritance.

Prefer explicit errors over panics.

All critical operations must be logged.

No hidden goroutines.

All goroutines must be cancellable through `context.Context`.

All configuration must be external.

No hardcoded IP addresses.

No hardcoded MQTT topics.

Document every public type and function.

Keep packages focused.

Business logic belongs in services, not handlers.

Write code that another engineer could maintain in five years.
