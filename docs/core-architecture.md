# AquaOS core domain and canonical state

Edition 1.1 Prompt 3 establishes the transport-independent domain contracts.
No type or service in this milestone is authorized to command equipment.

## Dependency direction

```text
composition root
    |
    +--> device + endpoint registry
    |         |
    |         +--> sensor registry
    |         +--> equipment registry
    |
    +--> canonical state store --> bounded snapshot subscribers
    |
    +--> event publisher

domain types <--- registries and state
domain types -X-> HTTP, MQTT, storage, Shelly, ESP32, vendor protocols
```

Sensor and equipment registries own narrow `EndpointLookup` interfaces. The
composition root injects the device registry implementation. This permits
ownership validation without importing adapters or creating a global registry.

## Stable types and invariants

`DeviceID`, `SensorID`, `EquipmentID`, `EndpointID`, `AlarmID`, `CommandID`, and
`CorrelationID` are distinct UUID-backed Go types. Accidental assignment across
identity classes is a compile-time error. `EntityID` is used only for
heterogeneous canonical-state keys and is always paired with `EntityKind`.

Quantities contain an explicit kind, finite value, and canonical unit. The
domain currently recognizes temperature/celsius, pH/pH, salinity/ppt,
flow/liters-per-hour, and level/percent. pH and percentage boundaries are
validated. New units require a reviewed domain contract rather than free-form
strings.

Measurements contain observed and received timestamps, quality, and a positive
freshness duration. At the exact expiry boundary their effective quality is
`stale`. Quality is explicit: `good`, `suspect`, `stale`, `invalid`, or
`unavailable`.

Capabilities are similarly closed for this milestone: `observe`, `switch`, and
`variable-output`. Registries reject empty, duplicate, unsupported, or
ownership-exceeding capability sets.

## Registry ownership

A device declares the capabilities its endpoints may expose. An adapter
endpoint must reference an existing device and may expose only a subset of that
device's capabilities. Sensors and equipment must reference an existing
endpoint, agree with its device owner, and remain within its capability set.
Devices with registered endpoints cannot be removed.

All registry reads return deep immutable copies and list operations use stable
ID ordering. Sensors, equipment, and adapter endpoints are distinct records;
registration never grants command authority.

## Canonical state

State keys contain entity kind, entity ID, plane, and attribute. Sensors may
write only the observation plane. Equipment desired and reported state are
stored separately, preventing a request from being mistaken for physical
confirmation.

Every successful set or delete advances one process-local global `Revision`
under the store lock. Snapshots contain values from exactly one revision, use
stable key ordering, and deep-copy all pointer-backed values. Revision is not a
cluster ordering primitive.

Subscriptions have caller-selected bounded capacity from 1 through 1024. The
store never waits for a subscriber: when a channel is full, it discards the
oldest pending snapshot, delivers the newest snapshot, and reports the number
of superseded deliveries. Closing a subscription is explicit and idempotent;
the implementation starts no subscription goroutines.

Freshness is evaluated through an injected clock. Historical persistence and
durable replay are outside this store and remain later milestones.
