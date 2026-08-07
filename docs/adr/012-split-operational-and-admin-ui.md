# ADR-012: Separate operational and administrative user interfaces

- Status: Accepted
- Date: 2026-08-06
- Decision owners: AquaOS maintainers
- Governing specification: AquaOS Development Bible, Edition 1.2

## Context

Home Assistant remains the familiar daily aquarium interface, while installation
and lifecycle administration need different workflows and recovery properties.
Neither UI may become a hardware authority or duplicate Core safety policy.

## Decision

Home Assistant is the normal operational GUI for status, daily commands,
notifications, and integrations. A separate lightweight AquaOS Admin GUI will
support installation, configuration, diagnostics, backup/restore, upgrades,
rollback, and repair.

Both are non-authoritative clients. Every mutation passes through authenticated
AquaOS APIs/application services, authorization, validation, and existing safety
policy. HTTP or MQTT handlers contain no equipment policy and never write
hardware or raw active configuration directly. Headless CLI recovery remains
available when browsers or either GUI are unavailable.

## Alternatives considered

- Use Home Assistant for all administration. Rejected because it conflates
  daily operation with installation, repair, and recovery authority.
- Put policy in the Admin GUI. Rejected because browser loss, version skew, or a
  compromised client could bypass Core invariants.
- Require the Admin GUI for recovery. Rejected because headless recovery is an
  operational safety requirement.

## Consequences

Prompt 9 defines authenticated API contracts as the only supported mutation
path for the future Admin GUI. Prompt 10 integrates Home Assistant without
bypassing command policy. Prompt 12 implements the Admin GUI and recovery-safe
CLI. Prompt 13 threat-models GUI authentication, sessions, CSRF/Origin policy,
LAN exposure, caching, and recovery access.

This ADR introduces no UI or API implementation before its ordered milestone.
