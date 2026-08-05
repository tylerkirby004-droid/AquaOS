# MQTT topic contract

Broker namespace: `aquaos`. Use UTF-8 JSON payloads, UTC ISO-8601 timestamps, QoS 1 for alerts and state changes, and retained messages only for current state/configuration.

## Naming

```text
aquaos/<site>/<tank>/<domain>/<entity>/<metric>
```

The initial single-tank deployment uses site `home` and tank `reef`. For compatibility with early hardware bridges, aliases may be kept temporarily; all new integrations should use the canonical format.

## Canonical examples

```text
aquaos/home/reef/sensor/temperature/value
aquaos/home/reef/sensor/ph/value
aquaos/home/reef/equipment/heater/state
aquaos/home/reef/alert/leak/state
aquaos/home/reef/command/pump/return/set
aquaos/home/reef/event/pump/return/ack
```

## Telemetry payload

```json
{"value": 78.4, "unit": "F", "timestamp": "2026-08-05T21:00:00Z", "source": "reefpi-01"}
```

## Command payload

```json
{"request_id": "uuid", "value": "on", "requested_by": "node-red", "timestamp": "2026-08-05T21:00:00Z"}
```

Commands must be authenticated, non-retained, and acknowledged on the corresponding event topic. Do not publish credentials, tokens, or personally identifying data.
