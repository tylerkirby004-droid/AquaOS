# Typed events and alarm lifecycle

AquaOS uses stable event codes and a transport-neutral JSON envelope. Every
event carries UTC time, source, severity, payload, and a correlation ID. Codes
are additive contracts: an existing code must never be reused with different
meaning.

The in-process bus performs synchronous handler delivery. A configured
semaphore bounds concurrent publications; excess work receives
`events.ErrBackpressure` immediately. All matching handlers run in registration
order, their errors are joined and returned to the publisher, and no automatic
retry occurs. This makes failure visible and avoids unsafe duplicate side
effects. A consumer requiring retry must implement idempotency and durable retry
at its own boundary.

```text
publisher -- validated event --> bounded bus --> handler A
                                      |--------> handler B
             error/backpressure <----+
```

The alarm engine consumes generic boolean observations for registered rules.
Aquarium-specific thresholds remain outside this package. A rule defines its
subject, base severity, debounce interval, clear hysteresis interval, and
latching policy.

```text
healthy -> debouncing -> active -> acknowledged
                         |             |
                         +-- healthy --+--> clear pending --> cleared
                                              (explicit clear when latching)
```

Observation timestamps, rather than processing time, determine transitions.
Equal or older observations are ignored. Acknowledgement records operator
awareness and never changes the underlying `conditionActive` value. Severity
may only escalate within an episode. Evidence is defensively copied and kept in
a bounded history. Alarm/event logs include stable transition codes and the
originating correlation ID.

The stores are process-local. Typed IDs and transport-neutral contracts leave
room for a durable event log and clustered ownership later, but no clustering
behavior or distributed-consensus assumptions exist today.
