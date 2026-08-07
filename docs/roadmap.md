# AquaOS delivery roadmap

The AquaOS Development Bible, Edition 1.1, governs milestone detail and exit
criteria. Work proceeds in order and stops when a required gate lacks evidence.

1. v0.1 Foundation — repository, logging, optional MQTT skeleton, CI,
   broker-free bootstrap, and minimal Pi deployment.
2. v0.1 Configuration and lifecycle — schema v1, validation, redaction,
   atomic reload planning, bounded lifecycle, and aggregate health.
3. v0.2 Core domain — typed IDs, registries, revisioned state, bounded typed
   events, the generic alarm engine, and documented Pi control-node verification.
4. v0.3 Equipment and safety — explicit equipment/command state machines,
   command policy, interlocks, watchdogs, and alarm lifecycle foundations.
5. v0.4 MQTT contracts — implemented: versioned topics, envelopes, codecs,
   bounded idempotency, ACLs, and reconnect/reconciliation behavior.
6. v0.5 Simulator — deterministic tank and fault scenarios without real hardware.
7. v0.6 Shelly and ESP32 bench — safe lamp-load hardware evidence on Pi 4B.
8. v0.7 Integrations — REST v1, Home Assistant, optional InfluxDB/Grafana.
9. v0.8 Operations beta — guided installation and multi-server operations.
10. v0.9 Release candidate — hardening, matrix tests, and 72-hour soak.
11. v1.0 Stable — approved release evidence, artifacts, and compatibility.

Optional Python AI work is Prompt 15 and may not be introduced earlier or
become necessary for critical operation.
