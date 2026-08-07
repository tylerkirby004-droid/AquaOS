# ADR-015: Home Assistant as an optional MQTT client

- Status: Accepted
- Date: 2026-08-07

## Context

Home Assistant is the daily operational UI but cannot be an equipment authority or a dependency of local control. MQTT delivery is at least once, retained discovery survives restarts, and entity identity must remain stable across releases.

## Decision

AquaOS publishes one retained MQTT Discovery document per entity using UUID-derived `unique_id` values and stable object topics. Entity commands use a narrow AquaOS Home Assistant namespace. The bridge accepts only non-retained `ON` or `OFF` messages for currently registered equipment and constructs a bounded command for the existing output service. It has no hardware reference and contains no safety policy.

Discovery publication and MQTT startup are optional lifecycle operations. Their failure degrades health but never rolls back Core startup. Reconnect invokes discovery reconciliation. Removed entities are cleared with an empty retained payload during an in-process refresh. Removal across a process restart uses explicit configuration tombstones; renames preserve the UUID-derived unique ID and therefore update the existing entity.

## Consequences

Home Assistant, MQTT, and the display Pi may be stopped or removed without changing local control. A tombstone must remain configured through at least one successful broker-connected reconciliation before deletion from configuration. Home Assistant is not an installer or configuration authority.
