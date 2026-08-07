# ADR-013: Use bounded direct-LAN protocols for Prompt 8 adapters

- Status: Accepted for bench implementation
- Date: 2026-08-06
- Decision owners: AquaOS maintainers
- Governing specification: AquaOS Development Bible, Edition 1.2 Prompt 8

## Context

Critical local control must continue without MQTT, Home Assistant, storage,
Internet access, AI, or the display Pi. Shelly Plug US Gen4 has a documented
local RPC API. The project controls the ESP32 firmware contract and therefore
needs an explicit, versioned boundary rather than inferred payloads.

## Decision

- Use Shelly's local HTTP RPC `Switch.Set` and `Switch.GetStatus` methods through
  a small client interface. Treat `Switch.Set` success only as acknowledgement;
  reported-state polling remains required for command success.
- Verify the configured power-return behavior with `Switch.GetConfig`
  `initial_state`; do not silently mutate plug configuration. The implemented
  fields follow Shelly's official [Switch component documentation](https://shelly-api-docs.shelly.cloud/gen2/ComponentsAndServices/Switch/)
  and [Plug US Gen4 device documentation](https://shelly-api-docs.shelly.cloud/gen2/Devices/Gen4/ShellyPlugUSG4/).
- Poll the AquaOS ESP32 endpoint `/aquaos/v1/snapshot` over HTTP or HTTPS using
  schema `1.0`. Authenticate with an optional bearer token supplied outside
  committed configuration. Never place credentials in URLs or logs.
- Bound every response, request deadline, retry count, queue, and freshness
  window. Ignore additive JSON fields within a supported schema version and
  reject unsupported schema versions.
- Identify node boot sessions explicitly. Sequence must increase within one
  boot; a new boot ID permits sequence reset and triggers normal reconciliation.
- Require exactly two independently configured DS18B20 identities. Preserve
  individual quality and mark otherwise valid readings suspect when their
  difference exceeds the external agreement threshold.
- Keep protocol DTOs inside adapter packages. Emit only generic domain
  measurements, reported equipment state, availability, and stable failure
  codes to application services.

## Alternatives considered

- MQTT-only device communication. Rejected because broker loss cannot disable
  critical sensing or control.
- Shelly WebSocket notifications as the sole reported-state source. Rejected
  for the first bench because reconnect and notification-loss handling expands
  the failure surface; bounded polling is deterministic and independently
  verifies state.
- Invent an Atlas Scientific transport. Rejected until hardware and protocol
  selection are verified.

## Consequences

Polling creates bounded LAN traffic and reconciliation latency. Prompt 8 bench
evidence must measure suitable intervals and timeouts. HTTP without transport
encryption is acceptable only on the isolated trusted bench LAN; production
network and credential policy remains an explicit security item for Prompt 13.
Firmware compatibility is limited to the tested Shelly Gen4 versions and
ESP32 schema versions recorded in bench evidence.

Rollback is disabling the configured adapter and returning to the
hardware-incapable simulator or rejecting executor. It must never silently
activate a different hardware transport.
