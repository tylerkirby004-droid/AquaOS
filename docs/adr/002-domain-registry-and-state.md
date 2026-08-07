# ADR-002: Typed domain, ownership registry, and canonical state

- Status: Accepted
- Date: 2026-08-06
- Bible baseline: Edition 1.1, Prompt 3

## Context

The foundation contained experimental generic IDs, unchecked capability
strings, independent registries without ownership validation, and arbitrary
JSON current state. Those contracts could allow identity confusion, ambiguous
ownership, or desired state to be mistaken for reported hardware state.

## Decision

Use distinct UUID-backed ID types and closed, validated quantity, unit, quality,
and capability values in `internal/domain`. Keep devices, adapter endpoints,
sensors, and equipment separate. Sensor and equipment packages own the narrow
endpoint lookup interface they consume.

Canonical state separates observation, desired, and reported planes. One locked
global revision advances on every mutation. Reads and stable-order snapshots
are immutable. Subscriptions use bounded channels with drop-oldest/latest-state
delivery and never block writers.

## Compatibility impact

This intentionally replaces unreleased experimental `core.Device`, generic
sensor/equipment IDs, arbitrary JSON state, and unconstrained capability
strings. There is no released external API, MQTT, or stored-data contract to
migrate. Any downstream development branch must update to the typed contracts.

## Consequences

New quantities, units, quality values, or capabilities require explicit domain
review. The in-memory state revision remains local to one process. Durable
history, cluster ordering, safety policy, and output commands are not provided.

## Rollback

Rollback requires restoring the Prompt 2 binary and code together. No persistent
state migration is needed because Prompt 3 storage is in-memory only.
