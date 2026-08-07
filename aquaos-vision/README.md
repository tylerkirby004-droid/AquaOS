# aquaos-vision

This is an optional, non-authoritative deployment unit. Removing it has no
effect on AquaOS Core. It receives only provisioned assets and publishes
versioned, expiring observations. It has no command or actuator credentials.

Models can be biased, unavailable, stale, or wrong. Observations below 0.80
confidence are quarantined, but confidence is not a safety guarantee. Never use
this service as the only leak detector or life-support interlock. The scaffold
does not ship a trained model; unavailable models publish nothing and health
must report degraded in a deployment-specific runner.

Run tests with `python -m pytest`. Container use is permitted because this unit
is optional and outside the critical Core path.
