# ADR-021: Dry-run-first multi-VM installation

- Status: Superseded by ADR-023
- Date: 2026-08-07

## Context

Edition 1.2 places native AquaOS Core, optional services, and Home Assistant OS
in distinct Proxmox guests. Manual command sequences are error-prone for new
operators, but an installer that silently changes a hypervisor could overwrite
unrelated systems or conceal the single-host failure domain.

## Decision

Ship a workstation-side Go orchestrator with `init`, `plan`, and explicitly
acknowledged `apply` phases. It accepts only external configuration, verifies
signed release inputs, refuses occupied VM IDs before mutation, clones approved
operator-created templates, enables ordered startup, and stops on the first
failed action. Template preparation is a separate checksum-verifying,
dry-run-first script. Core remains a native systemd service; Docker is confined
to the optional-services VM.

Home Assistant onboarding and all physical commissioning remain human actions.
The installer does not claim to remove the Proxmox host failure domain and
requires explicit acknowledgement of off-host backups and independent physical
safeguards.

## Consequences

The common installation becomes repeatable and reviewable without giving a web
application hypervisor credentials. Operators must still obtain and verify
current upstream images, establish SSH trust, perform physical tests, and prove
replacement-host recovery. Existing VM IDs are never automatically replaced or
destroyed.
