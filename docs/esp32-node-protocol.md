# ESP32 sensor-node protocol v1

This contract is owned by AquaOS and is not an assertion about existing ESP32
firmware. Prompt 8 firmware and fake clients must implement it explicitly.

## Request

```http
GET /aquaos/v1/snapshot
Accept: application/json
Authorization: Bearer <externally supplied token>
```

The token is optional only for an isolated bench. It is never put in a URL,
committed YAML, event, diagnostic bundle, or log.

## Response

```json
{
  "schemaVersion": "1.0",
  "nodeId": "22222222-2222-4222-8222-222222222222",
  "firmware": "aquaos-node/0.1.0",
  "bootId": "random-per-boot-value",
  "sequence": 42,
  "observedAt": "2026-08-06T18:30:00Z",
  "probes": [
    {
      "sensorId": "33333333-3333-4333-8333-333333333333",
      "celsius": 25.1,
      "valid": true
    },
    {
      "sensorId": "44444444-4444-4444-8444-444444444444",
      "celsius": 25.2,
      "valid": true
    }
  ]
}
```

The response is capped at 64 KiB. Schema `1.x` may add fields, but Core accepts
only the explicitly supported schema version. Unknown major/minor versions are
rejected until compatibility is reviewed. `nodeId` and both `sensorId` values
must match external inventory. `bootId` changes on every node reboot. Sequence
starts above zero and increases for every published snapshot within one boot.

Core rejects duplicate/out-of-order snapshots, future timestamps outside the
configured clock-skew allowance, wrong identities, duplicate/missing probes,
and malformed or oversized responses. A stale timestamp produces stale quality.
One invalid probe remains individually invalid. Two otherwise valid probes that
exceed the configured temperature difference are both suspect and raise the
stable `esp32.probe_disagreement` condition.

## Node health and security

Stable failure codes include `esp32.unreachable`, `esp32.schema_unsupported`,
`esp32.identity_invalid`, `esp32.timestamp_invalid`, `esp32.sequence_stale`,
`esp32.probe_count_invalid`, `esp32.probe_identity_invalid`,
`esp32.probe_invalid`, `esp32.probe_disagreement`, and
`esp32.snapshot_stale`.

The node must not accept equipment commands through this endpoint. Production
authentication, certificate provisioning, replay window, firmware signing, and
network segmentation require bench evidence and the Prompt 13 threat model.

## Future Atlas Scientific boundary

Atlas Scientific pH and conductivity integration will be a separate adapter or
an explicitly versioned extension after the electrical interface, isolation,
firmware library, calibration ownership, temperature compensation, units, and
error semantics are verified. Do not reuse DS18B20 fields or fabricate Atlas
commands. Chemistry readings will enter Core as generic validated measurements
with independent identity, calibration metadata, timestamp, freshness, and
quality.
