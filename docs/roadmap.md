# AquaOS delivery roadmap

The AquaOS Development Bible, Edition 1.2, governs milestone detail and exit
criteria. Work proceeds in order and stops when a required gate lacks evidence.

1. v0.1 Foundation — repository, logging, optional MQTT skeleton, CI,
   broker-free bootstrap, and the original minimal deployment foundation.
2. v0.1 Configuration and lifecycle — schema v1, validation, redaction,
   atomic reload planning, bounded lifecycle, and aggregate health.
3. v0.2 Core domain — typed IDs, registries, revisioned state, bounded typed
   events and the generic alarm engine.
4. v0.3 Equipment and safety — explicit equipment/command state machines,
   command policy, interlocks, watchdogs, and alarm lifecycle foundations.
5. v0.4 MQTT contracts — implemented: versioned topics, envelopes, codecs,
   bounded idempotency, ACLs, and reconnect/reconciliation behavior.
6. v0.5 Simulator — implemented: deterministic tank physics, fake adapter,
   bounded scenario fixtures, fault traces, and hardware isolation tests.
7. Post-Prompt-7 pivot — dedicated Linux amd64 Control VM becomes the native
   systemd production target; the Pi becomes an optional display/kiosk.
8. v0.6 Shelly and ESP32 bench — direct-LAN adapters and safe lamp-load evidence
   from the dedicated Control VM. Software boundaries and fake-LAN contracts are
   implemented; physical Control VM/lamp-load evidence remains required.
9. v0.7 Integrations — REST v1 and Home Assistant MQTT Discovery are
   implemented with authenticated/application-service-only mutations, stable
   entity identity, retained cleanup, and optional-outage isolation. Optional
   InfluxDB/Grafana follows.
10. v0.8 Operations beta — guided installation, Admin GUI, and multi-server
    operations.
11. v0.9 Release candidate — hardening, matrix tests, and 72-hour soak.
12. v1.0 Stable — approved release evidence, artifacts, and compatibility.

Optional Python AI work is Prompt 15 and may not be introduced earlier or
become necessary for critical operation.
