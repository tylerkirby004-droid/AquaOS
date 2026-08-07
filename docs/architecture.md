# AquaOS architecture

The governing source is the checked-in AquaOS Development Bible, Edition 1.1.
This page is a compact orientation, not an alternate specification.

## Authority boundary

```text
physical state
    -> direct local adapter observation
    -> validated canonical state
    -> safety engine and equipment state machine
    -> sole output manager
    -> direct local device command
    -> reported-state reconciliation
```

AquaOS Core on a Raspberry Pi 4B is the authoritative local controller. The
baseline future adapters are Ethernet/PoE ESP32 sensor nodes and Shelly Plug US
Gen4 outputs. Reef-Pi is only a possible future compatibility adapter.

MQTT, Home Assistant, InfluxDB, Grafana, remote servers, Internet access, and AI
are outside the critical control path. Losing all of them must not stop local
sensing, safety evaluation, output control, or reconciliation. UI and external
commands are requests that must pass through AquaOS Core policy.

## Foundation scope

Prompt 1 supplies repository and runtime foundations. Prompt 2 adds strict
configuration schema v1, semantic validation, redaction, active digests, atomic
reload planning, bounded lifecycle orchestration, injected time, and health
that distinguishes liveness, readiness, degradation, and failure. Neither
milestone contains accepted equipment-control or safety behavior. Later code
must use `internal/output` as the sole authorized equipment command path.
