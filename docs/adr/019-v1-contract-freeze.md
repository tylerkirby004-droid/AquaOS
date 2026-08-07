# ADR 019: Freeze v1 external contracts

- Status: Accepted for release-candidate validation
- Date: 2026-08-07

Configuration schema 1, REST `/api/v1`, MQTT `aquaos/v1`, ESP32 schema 1,
backup validation, and stable Home Assistant entity identity are frozen as
described in `docs/compatibility-v1.md`. Incompatible changes require a new
major contract, migration and rollback documentation, coexistence testing, and
an ADR. This freeze does not assert production readiness; physical and soak
evidence remains release-blocking.
