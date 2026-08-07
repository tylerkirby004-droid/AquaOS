# AquaOS architecture

The governing source is the checked-in AquaOS Development Bible, Edition 1.2.
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

AquaOS Core on a dedicated Linux amd64 control host is the authoritative local
controller and runs natively under systemd. ADR-022 makes a dedicated Debian
appliance the default guided profile; the separate Proxmox Control VM remains
the advanced strong-isolation profile. AquaOS is never installed directly on
the Proxmox host, and Docker is not part of the critical Core path. The baseline
adapters are Ethernet/PoE ESP32 sensor nodes and
Shelly Plug US Gen4 outputs communicating directly over the local LAN. Reef-Pi
is only a possible future compatibility adapter.

MQTT, Home Assistant, InfluxDB, Grafana, remote servers, Internet access, and AI
are outside the critical control path. Losing all of them must not stop local
sensing, safety evaluation, output control, or reconciliation. UI and external
commands are requests that must pass through AquaOS Core policy.

The Raspberry Pi 4B is an optional display/kiosk for Home Assistant, Grafana,
and AquaOS status pages; its failure has zero control impact. Home Assistant is
the normal operational UI. The AquaOS Admin GUI is a non-authoritative
operations client, and all of its mutations pass through authenticated Core
APIs/application services and safety validation.

Neither deployment profile eliminates its physical-host failure domain. A
Proxmox-host or dedicated-appliance failure stops Core. Independent physical
equipment safeguards, UPS planning for the host and network, automatic service
startup, tested backups/restores, and replacement-host recovery mitigate that
risk without hiding it.

## Foundation scope

Prompt 1 supplies repository and runtime foundations. Prompt 2 adds strict
configuration schema v1, semantic validation, redaction, active digests, atomic
reload planning, bounded lifecycle orchestration, injected time, and health
that distinguishes liveness, readiness, degradation, and failure. Neither
milestone contains accepted equipment-control or safety behavior. Prompt 3–7
domain, safety, event, MQTT, and simulator contracts remain valid after the
deployment pivot. Later code
must use `internal/output` as the sole authorized equipment command path.
