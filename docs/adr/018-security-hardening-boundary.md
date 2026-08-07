# ADR 018: Security hardening boundary

- Status: Accepted
- Date: 2026-08-07

## Decision

AquaOS treats every LAN client, MQTT publisher, browser, optional integration,
and artifact source as untrusted. Core mutations remain reachable only through
authenticated application services and the established command/configuration
policy. The Admin GUI is a loopback recovery adapter, uses no cookie session,
does not persist its bearer credential in browser storage, rejects cross-origin
mutations, disables caching and framing, and cannot call device adapters.

Inputs, token buckets, archives, headers, and request durations are bounded.
MQTT ACLs separate device, Home Assistant, AI, and Core privileges. Optional
subsystems remain outside the critical local-control path.

## Consequences

Operators must provision random bearer credentials of at least 32 characters
and use an SSH tunnel for remote Admin access. TLS termination and centralized
identity are deployment responsibilities until a later authenticated gateway is
specified. Security scans are release gates; suppressions require a documented
owner, expiry, and rationale.
