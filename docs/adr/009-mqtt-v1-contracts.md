# ADR 009: MQTT v1 integration contracts

- Status: Accepted
- Date: 2026-08-06

## Decision

MQTT is an optional external integration transport under the versioned
`aquaos/{site}/v1` registry. Topic constructors own QoS and retained policy.
Strict envelopes, configured payload limits, bounded idempotency and offline
queues, explicit expiry, birth/LWT, and reconnect reconciliation are mandatory.

Command consumers delegate to the sole output service. They cannot execute
equipment behavior. Commands, results, events, and AI observations are never
retained. Per-service broker ACLs deny AI command and desired-state writes.

## Consequences

Broker loss does not affect local control. QoS 1 duplicates are safe within a
bounded idempotency window. Queue exhaustion and expired work are visible
rather than silently consuming memory. Incompatible semantics require a new
topic major version and a documented coexistence/rollback window.
