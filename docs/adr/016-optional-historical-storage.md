# ADR-016: Optional bounded historical storage

- Status: Accepted
- Date: 2026-08-07

## Context

Historical telemetry is useful but may never delay or prevent local control. InfluxDB can be unavailable, slow, full, or misconfigured for long periods.

## Decision

Core events feed a non-blocking bounded queue. Overflow drops the newest point and increments an observable counter. One explicitly owned goroutine batches writes. A failed batch remains bounded, retries with capped exponential backoff, and causes further arrivals to encounter the normal queue bound. Shutdown makes one timeout-bounded final attempt.

InfluxDB v2 is implemented through its HTTP line-protocol endpoint using a token loaded from an external file. It is an optional lifecycle component: startup and runtime failures degrade health but cannot roll back or block Core. Event handlers always return success after recording a storage drop.

Measurements and tags are versioned. Entity IDs may be tags because configured inventory is finite. Correlation IDs, command IDs, alarm IDs, payloads, error text, timestamps, and arbitrary metadata are fields, never tags. Secrets are neither points nor logs.

## Consequences

Historical gaps are possible and explicit. The queue is not durable across Core restart. A future durable spool requires a separate ADR and must retain the same non-blocking control boundary.
