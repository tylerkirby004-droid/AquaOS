# ADR-011: Move the production controller to a dedicated Control VM

- Status: Accepted
- Date: 2026-08-06
- Decision owners: AquaOS maintainers
- Governing specification: AquaOS Development Bible, Edition 1.2

## Context

Prompts 1–7 were completed with Raspberry Pi OS Lite 64-bit on a Raspberry Pi
4B as the reference controller. Edition 1.2 deliberately changes the reference
deployment after Prompt 7. The production controller is now a dedicated minimal
Linux amd64 AquaOS Control VM on Proxmox. This changes deployment, testing, and
operations, but it does not invalidate the implemented Go domain, safety,
event, MQTT, or simulator contracts.

A VM improves service isolation and operational consistency but does not remove
the physical Proxmox host as a single failure domain.

## Decision

- Run AquaOS Core natively under systemd in a dedicated minimal Linux amd64 VM.
- Never install AquaOS directly on the Proxmox host.
- Use bridged LAN networking and a reserved/static Control VM address.
- Make Linux amd64 the primary production artifact. Retain Linux arm64 as a
  portability, emergency, and development target.
- Keep Docker outside the critical Core path. Container support may remain for
  development and optional integration services.
- Repurpose the Raspberry Pi 4B as an optional fish-room display/kiosk only.
  Its loss must have zero effect on aquarium control.
- Connect Shelly Plug US Gen4 and Ethernet/PoE ESP32 nodes directly to Core over
  the local LAN; MQTT is not required for critical local control.
- Explicitly mitigate physical host failure with independent equipment
  safeguards, UPS planning, automatic VM and systemd startup, configuration and
  VM backups, tested restore, replacement-host recovery, and an emergency
  runbook.

## Alternatives considered

- Keep the Raspberry Pi 4B as the production controller. Rejected as the new
  reference deployment, while arm64 remains a supported portability direction.
- Install AquaOS on the Proxmox host. Rejected because it weakens isolation and
  couples controller lifecycle to the hypervisor.
- Run Core primarily in Docker. Rejected because the critical path must not
  acquire a container-runtime dependency.
- Claim VM high availability resolves host failure. Rejected because no such
  cluster is implemented and the physical host can stop Core.

## Consequences

Production documentation, build ordering, systemd installation, bench tests,
backup/restore, and recovery guidance use the Control VM baseline from Prompt 8
forward. Existing Prompt 1–7 implementation and milestone commits are
preserved. Prompt 8 owns direct adapter and Control VM bench behavior; Prompt 12
owns the supported installer, Admin GUI, backup/restore, upgrade/rollback, and
replacement-host workflows.

Rollback of this documentation decision means adopting a replacement deployment
ADR and migration plan; it does not justify rewriting completed domain or safety
milestones.
