# ADR-017: Control VM operations service, CLI, and Admin GUI

- Status: Accepted
- Date: 2026-08-07

## Context

Installation and recovery must be safe, repeatable product behavior rather than copied shell fragments. Browser availability cannot be required for recovery, and a GUI cannot bypass configuration or deployment policy.

## Decision

`internal/operations` is the single application-service boundary for installation, candidate configuration, verification, repair, backup/restore, signed upgrades, rollback, and uninstall. It modifies only an enumerated path set, validates complete candidate configuration before activation, uses atomic file replacement, preserves unrelated files, and rolls back failed configuration or binary activation.

`aquaosctl` is the supported headless client. `aquaos-admin` is a separately launched authenticated recovery GUI with embedded static assets and no Node.js build. Both invoke the same operations service. The Core service remains least privilege; privileged deployment operations are not silently added to its normal runtime.

The Admin GUI exposes a structured, redacted editable snapshot rather than
requiring users to author YAML. Transport code converts the structured request
back into a complete candidate, and the existing operations service performs
strict validation and atomic activation. This keeps browser presentation out of
configuration policy while retaining a raw preview for advanced diagnosis.

Production installation is refused unless the operator acknowledges a dedicated Control VM, the platform is Linux amd64, and `/etc/pve` does not identify a Proxmox host. Release installation and upgrade require matching SHA-256 and Ed25519 signature evidence. Signing keys are external and never shipped in the repository.

## Consequences

The Admin GUI is optional and may be stopped without affecting Core. Headless recovery remains available. Clean-VM, Proxmox startup, UPS, replacement-host, and first-time-user acceptance evidence must be collected outside CI before Prompt 12 is fully exited.
