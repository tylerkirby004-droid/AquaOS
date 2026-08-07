# Equipment state machines and safety engine

Prompt 5 establishes the only authorized path from a control request to a
hardware adapter. It does not implement an adapter or aquarium-specific
thresholds.

## Command path

```text
API / service / future MQTT consumer
              |
              v
      output command service
       | structural validity
       | idempotency and expiry
       | optimistic revision
       | safety decision
       v
       local adapter port --------> physical device
              |                         |
              | acknowledgement         | reported state
              v                         v
         acknowledged ------------> reconciled success
```

An accepted adapter acknowledgement is not success. A command becomes
`succeeded` only when a later reported-state observation matches the requested
state. A mismatch is explicit failure. The application composition root uses a
rejecting, hardware-incapable executor until an adapter is deliberately wired.

Every command carries a UUID, idempotency key, correlation ID, requester,
issue/expiry timestamps, optional expected canonical-state revision, and a
stable result reason. Reusing an idempotency key with equivalent intent returns
the existing result and never dispatches twice. Reusing it for different intent
is a conflict.

## Equipment and command state machines

```text
unknown --reported off--> off --command on--> starting
                                      acknowledgement on
                                               |
                                               v
                              starting --reported on--> on

on --command off--> stopping --acknowledgement off--> stopping
                                      |
                                      +--reported off--> off

any state --fault--> fault --reset--> unknown
```

Acknowledgement deliberately leaves equipment in `starting` or `stopping`.
Only reconciliation establishes `on` or `off`. Uncommanded reported changes
are represented explicitly so later adapters can reconcile manual buttons and
restarts.

## Safety policy

Profiles distinguish generic outlets, heater supervision, return pumps,
circulation pumps, ATO, and dosing pumps. Profiles and limits are constructor
inputs: no tank-specific threshold or hardware address is compiled into Core.
Hazardous heater, ATO, and dosing profiles require a maximum-on duration and at
least one canonical sensor input. Dosing additionally requires a daily runtime
limit. Hazardous equipment always fails safe off.

Modes are `normal`, `maintenance`, `manual`, and `emergency-stop`.
Maintenance and manual activation require a reasoned, scoped, expiring
override. Emergency stop, maximum-on, maximum-daily-on, minimum-off, and
hazardous input validity are hard constraints. No mode or override bypasses a
hard constraint.

Required hazardous inputs must exist, be fresh, have `good` quality, and match
any required Boolean state. Missing, stale, suspect, invalid, unavailable, or
mismatched inputs reject activation with stable reason codes. Safe-state/off
commands remain available so a bad sensor cannot trap hazardous equipment on.

## Watchdogs and ownership

The safety engine starts no goroutines. Its owner explicitly polls
`CheckWatchdogs` using a lifecycle-owned scheduler in a future integration.
This returns deterministic safe-state actions for maximum runtime, daily
runtime, and override expiry. `RunWatchdogs` submits those actions through the
same command pipeline, retaining audit, acknowledgement, and reconciliation
semantics.

Runtime accounting is currently process-local. A restart therefore loses
maximum-on and daily-runtime history until adapter reconciliation restores the
reported state. Hardware-independent protection and durable runtime recovery
must be completed before live-load approval.
