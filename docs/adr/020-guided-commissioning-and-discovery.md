# ADR-020: Keep discovery read-only and commissioning explicit

- Status: Accepted
- Date: 2026-08-07

## Context

The first Admin GUI accepted a complete candidate configuration but did not
provide safe workflows for discovering, mapping, calibrating, or commissioning
physical equipment. A browser-only implementation would allow presentation
code to become safety policy. Automatic subnet scanning and automatic output
activation would also create unacceptable reach and ambiguity.

## Decision

AquaOS probes only operator-supplied local addresses, permits at most 64
candidates per request, bounds concurrency and deadlines, and performs only
read operations. Candidate URLs are restricted to private, loopback,
link-local, or `.local` destinations. Transport errors are converted to
redacted operator messages.

The configuration v1 contract receives additive optional fields for friendly
device identity, equipment profiles and hard limits, calibration evidence,
typed alarm thresholds, commissioning evidence, and backup destinations. The
strict decoder continues to reject unknown fields. Older v1 configurations
remain valid.

Physical output commissioning follows an explicit state machine:

```text
discovered -> mapped -> configured -> validated -> bench-tested -> commissioned
```

Disabling an endpoint removes authorization and invalidates its prior evidence.
Hazardous equipment requires safe-load, fail-safe, power-return, and independent
physical-safeguard evidence. The Admin GUI remains a client of authenticated
application services and never sends output commands during discovery.

## Consequences

Setup takes more deliberate steps than consumer smart-home pairing, but an
address typo or discovery response cannot energize equipment. Browser loss does
not affect Core, and the same typed candidates remain usable by headless tools.
Automatic multicast discovery may be added later only behind the same bounded
read-only service.
