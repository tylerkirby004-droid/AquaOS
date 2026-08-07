# MQTT v1 contracts

MQTT is optional external integration. Broker loss degrades integration health
but cannot stop local sensor validation, safety policy, watchdogs, or direct
adapter control.

## Topic grammar and delivery policy

All AquaOS topics use `aquaos/{siteId}/v1/...`. Site and resource segments are
stable lowercase kebab-case. Publishing wildcards is rejected.

| Purpose | Topic | QoS | Retained |
|---|---|---:|:---:|
| Core status | `aquaos/{site}/v1/system/core/status` | 1 | yes |
| Birth/LWT | `aquaos/{site}/v1/system/core/availability` | 1 | yes |
| Sensor state | `aquaos/{site}/v1/sensors/{id}/state` | 1 | yes |
| Equipment reported | `aquaos/{site}/v1/equipment/{id}/reported` | 1 | yes |
| Equipment desired | `aquaos/{site}/v1/equipment/{id}/desired` | 1 | yes |
| Command request | `aquaos/{site}/v1/commands/{target}/request` | 1 | no |
| Command result | `aquaos/{site}/v1/commands/{target}/result` | 1 | no |
| Alarm state | `aquaos/{site}/v1/alarms/{id}/state` | 1 | yes |
| Event stream | `aquaos/{site}/v1/events/{type}` | 1 | no |
| AI observation | `aquaos/{site}/v1/ai/{service}/observations/{kind}` | 1 | no |
| HA discovery | `homeassistant/{component}/{objectId}/config` | 1 | yes |

Commands, command results, events, AI observations, and raw high-rate
telemetry must never be retained. Event QoS 1 is AquaOS Core's conservative v1
choice from the Bible's permitted QoS 0-or-1 range.

## Envelope

All versioned AquaOS payloads use lowerCamelCase fields:

```json
{
  "schemaVersion": "1.0",
  "messageId": "01JEXAMPLE",
  "correlationId": "10000000-0000-4000-8000-000000000001",
  "source": "aquaos-core",
  "siteId": "home-reef",
  "occurredAt": "2026-08-06T18:30:00Z",
  "expiresAt": "2026-08-06T18:30:30Z",
  "revision": 42,
  "data": {}
}
```

The machine-readable base schema is
[`configs/schemas/mqtt-envelope-v1.json`](../configs/schemas/mqtt-envelope-v1.json).
Codecs reject unknown fields, malformed or trailing JSON, wrong sites,
unsupported major versions, expired envelopes, and payloads above the external
limit. Minor v1 additions require compatibility review; incompatible meaning
requires a new topic major version and migration window.

## Commands and idempotency

A command is only a request. The MQTT router strictly decodes it and calls the
Core output service; it contains no equipment or safety logic. The command's
correlation ID must match the envelope, and its expiry cannot exceed envelope
expiry. Duplicate message IDs are suppressed in a bounded window. Handler
failure removes the reservation so explicit QoS redelivery may retry.

Results are always non-retained and retain the correlation ID. An
acknowledgement is not physical success; reported-state reconciliation remains
required by the output service.

## Connection and backpressure

Core configures a retained offline LWT. After initial connection or reconnect,
it restores narrow subscriptions, invokes retained-state reconciliation,
publishes retained online birth state, and drains its bounded offline queue.
Reconnect uses capped exponential backoff with jitter. Expired queued messages
are discarded; a full queue rejects new messages explicitly. No unbounded
goroutine or queue is created.

Transport and codec counters cover connects, reconnects, publishes, receives,
decode failures, duplicates, expired drops, and queue rejection. Logs contain
stable codes but never message payloads, usernames, passwords, or embedded
credentials.

## Broker security

Use authenticated per-service credentials and TLS outside loopback-only
development. Examples are in [`configs/mosquitto`](../configs/mosquitto).
Home Assistant may submit command requests but cannot write desired or reported
state. AI principals can publish only their observation namespace and have no
write access to command, desired-state, result, or equipment topics.
