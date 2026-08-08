# AquaOS Development Rules

The governing specification is `docs/AquaOS_Development_Bible_Edition_1.2.docx`
plus the user-approved post-Bible architecture decision in ADR-025. Preserve
completed milestone history; do not rewrite it solely because deployment
changed.

The primary operator installation is the AquaOS Home Assistant OS app on Linux
amd64. Home Assistant Ingress provides the single AquaOS sidebar and user
authentication. Do not add a second login, pairing code, exposed Admin port,
custom Debian ISO, Proxmox requirement, or terminal step to the normal path.
The older Debian/systemd profile is legacy only.

Core's running control loop must not depend on MQTT, InfluxDB, Grafana,
Node-RED, AI, Internet access, or the Home Assistant frontend process. The Home
Assistant OS host and Supervisor are an acknowledged shared failure domain.

The AquaOS sidebar is the single user-facing surface for operations, setup,
diagnostics, backup/restore, and repair. It is non-authoritative: every mutation
must use AquaOS APIs/application services and existing validation and safety
policy. Never let Home Assistant entities or automations bypass that pipeline.

Do not obscure the shared Home Assistant OS failure domain. Production guidance
must include independent safeguards, UPS planning, automatic app startup,
tested backups/restores, and replacement-host recovery. Every first start must
remain simulator-safe and must not commission equipment.

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
