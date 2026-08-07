# AquaOS Development Rules

The governing specification is `docs/AquaOS_Development_Bible_Edition_1.2.docx`.
Prompts 1–7 predate its post-Prompt-7 deployment pivot and must not be rewritten
solely because the production host changed.

AquaOS Core's primary production target is Linux amd64 in a dedicated minimal
AquaOS Control VM on Proxmox, running natively under systemd. Never install
AquaOS directly on the Proxmox host or make Docker part of the critical control
path.

ADR-022 additionally authorizes a dedicated single-purpose Debian amd64
appliance as the default guided installation. Core must still run natively under
systemd with priority over resource-limited optional containers. Do not install
the appliance profile on a shared desktop/server. Proxmox remains the advanced
strong-isolation profile.

The Raspberry Pi 4B is an optional display/kiosk only. Its failure, and failure
of MQTT, Home Assistant, InfluxDB, Grafana, Node-RED, AI, Internet access, or any
Admin GUI, must not prevent critical local control.

Home Assistant is the daily operational UI. The separate AquaOS Admin GUI is
non-authoritative: every mutation must use authenticated AquaOS APIs/application
services and existing validation and safety policy.

Do not obscure the Proxmox host failure domain. Production guidance must include
independent physical equipment safeguards, UPS planning, automatic VM/service
startup, tested backups/restores, and replacement-host recovery.

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
