# ADR-022: Support a dedicated Debian appliance profile

- Status: Accepted by project owner
- Date: 2026-08-07

## Context

The Edition 1.2 production reference uses a dedicated AquaOS Control VM on
Proxmox. That provides strong workload isolation but asks a first-time aquarium
operator to understand a hypervisor, templates, three VMs, networking, and
multiple installation procedures. The project owner requested a simpler
complete-system installation while preserving the safety boundary.

## Decision

Support a dedicated, single-purpose Debian amd64 computer as the default guided
appliance profile. AquaOS Core remains a native, least-privilege systemd service
and receives higher CPU/OOM priority. Home Assistant Container, Mosquitto,
InfluxDB, and Grafana run as explicitly optional, resource-limited Docker
services. Stopping Docker must not stop direct Shelly/ESP32 sensing, safety,
state, alarms, outputs, or the Core API.

The installer refuses the Proxmox host, non-Debian systems, non-amd64 systems,
and unacknowledged physical safeguards. It verifies the signed Core artifact,
runs a dry run before mutation, leaves hardware adapters in simulator mode, and
does not commission equipment. The existing Proxmox multi-VM profile remains
the advanced option for stronger software isolation.

## Consequences

The appliance has one physical-host failure domain and less workload isolation
than separate VMs. A host, kernel, storage, or power failure can stop all local
software. UPS planning, off-host backups, replacement-machine recovery, manual
fallback, and independent physical heater/ATO/dosing safeguards remain
mandatory. Home Assistant Container does not provide the Home Assistant add-on
store; AquaOS installs the required services separately.

This ADR extends the Edition 1.2 deployment choices. It does not change the
authority chain, make Docker critical, or authorize installation on a shared
general-purpose machine or directly on a Proxmox host.
