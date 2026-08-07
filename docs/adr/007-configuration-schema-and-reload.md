# ADR-007: Configuration schema and reload strategy

- Status: Accepted
- Date: 2026-08-06
- Bible baseline: Edition 1.1, Prompt 2

## Context

Configuration can affect lifecycle bounds, network listeners, integrations,
future adapters, and equipment identity. Partial or ambiguous activation would
be unsafe, while exposing secrets through diagnostics or digests would create
an avoidable security risk.

## Decision

Configuration uses strict, versioned YAML with conservative defaults and a
checked-in v1 JSON Schema. Go semantic validation is authoritative. Effective
snapshots are deep-copied, secrets are redacted, and the active digest covers
redacted canonical JSON.

Reloads are planned before activation. Version 1 hot-reloads only log level;
all connection, lifecycle, adapter, and inventory changes require restart.
Reload processing is serialized, publishes a non-secret audit event, updates
the live logger, and swaps the snapshot once.

## Consequences

Operators receive stable paths and reasons instead of partial changes. Adding a
hot-reloadable field requires an atomic live applier and tests proving rollback.
Adding schema v2 requires migration, compatibility, and rollback documentation.

## Migration and rollback

Existing pre-versioned development files must add `schema_version: 1`; the
repository samples already do. To roll back this change, restore the previous
binary and its matching configuration file together. Never feed a newer schema
to an older binary without its documented migration path.
