# ADR-010: Deterministic simulator boundary

- Status: Accepted
- Date: 2026-08-06

## Context

AquaOS needs repeatable safety and failure testing before any live adapter or
livestock system is permitted. A simulator that imports network or hardware
clients could accidentally cross the safety boundary, while a wall-clock or
unbounded model would make CI evidence unreliable.

## Decision

Keep the workbench under `internal/adapters/simulator` as a synchronous,
seeded, bounded model. Scenario start time, step, seed, physics, thresholds, and
faults are external versioned inputs. The fake adapter implements the existing
consumer-owned output interface and stores state only in memory. Desired and
reported state remain distinct. Broker and storage status are observable but
cannot influence the local supervisory path.

The lifecycle adapter remains inert until a caller explicitly invokes the
scenario runner or fake output adapter. The CLI reads strict bounded fixtures
and emits JSON Lines traces suitable for CI artifacts.

## Consequences

Runs are reproducible and structurally unable to contact real equipment. The
physics intentionally favors transparent first-order behavior over aquarium
fidelity. Scenario evidence validates policy trajectories, not electrical or
hydraulic performance; later bench gates remain mandatory.

## Verification

Unit and CLI tests cover determinism, temperature bounds, safe leak and stale
input behavior, relay divergence, pump failure, acknowledgement faults,
optional-service loss, strict input limits, and in-memory-only adapter state.
