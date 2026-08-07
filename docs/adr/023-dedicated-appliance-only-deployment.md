# ADR-023: Use a dedicated Debian appliance as the sole deployment

- Status: Accepted by project owner
- Date: 2026-08-07
- Supersedes: ADR-011, ADR-021, and ADR-022 deployment-profile choices

## Context

Maintaining both a Proxmox multi-VM path and a dedicated-appliance path made
installation, documentation, release packaging, testing, recovery, and support
harder to understand. Home Assistant already supplies the daily UI, history,
long-term statistics, and notifications. AquaOS needs to own the safety-critical
control boundary, not duplicate an infrastructure platform.

## Decision

Support one production layout: a dedicated, single-purpose Debian stable amd64
computer. AquaOS Core runs natively as a least-privilege systemd service.
Home Assistant and Mosquitto are standard noncritical containers. InfluxDB and
Grafana are installed only when the operator explicitly selects Advanced
History. AquaOS Admin remains a native, authenticated, TLS-protected operations
service. No AquaOS component is installed on a shared desktop, NAS, general
Docker host, or hypervisor host.

Remove the Proxmox orchestrator, template tooling, and active Proxmox install
guides. Historical ADRs remain in the repository marked superseded so the
decision history is not falsified.

## Consequences

There is one supported installation and one recovery procedure. Home Assistant
provides default trending, while advanced high-resolution retention costs extra
memory, disk, and maintenance only when selected. The appliance remains a
single physical failure domain. UPS protection, off-host backup/restore,
replacement-machine recovery, manual fallback, and independent physical
equipment safeguards remain mandatory.

Docker, Home Assistant, MQTT, InfluxDB, Grafana, Internet access, and any
display remain outside the critical path. Stopping Docker must not stop direct
sensor polling, safety policy, equipment control, alarm evaluation, or the Core
API.
