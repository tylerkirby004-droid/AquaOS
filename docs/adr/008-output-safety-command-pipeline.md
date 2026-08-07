# ADR 008: Single output command and safety pipeline

- Status: Accepted
- Date: 2026-08-06

## Context

Equipment commands can harm livestock or property if UI, transport, adapter,
or manual-mode paths bypass safety policy or mistake dispatch for physical
success.

## Decision

`internal/output` is the sole command path. It owns command identity,
idempotency, expiry, optimistic revision validation, lifecycle state, adapter
dispatch, acknowledgement, and reconciliation. `internal/safety` owns modes,
hard limits, required-input validation, overrides, and watchdog decisions.
Adapters implement only the consumer-owned executor interface.

Hard constraints are evaluated in every mode. Overrides are equipment-scoped,
reasoned, and expiring; they bypass only mode restrictions. Adapter
acknowledgement advances a command to `acknowledged`, never `succeeded`.
Matching reported state is required for success.

Watchdogs are explicitly polled rather than implemented as hidden goroutines.
The process starts with a rejecting executor and no active equipment profiles.

## Consequences

- HTTP, MQTT, and automation handlers cannot execute equipment logic directly.
- Safe shutdown/off requests remain possible when sensor input is invalid.
- Adapter implementations must provide acknowledgement and reported-state
  reconciliation separately.
- Runtime limits are not restart-durable yet; production use remains blocked
  until persistence and startup reconciliation are implemented and tested.
