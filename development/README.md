# Development

The supported v0.1 developer entry point is broker-free:

```sh
make bootstrap
```

Expected result: preflight, dependency composition, hardware-incapable simulator
readiness, and bounded shutdown all report success. No MQTT broker, database,
dashboard, Python service, or hardware is required.

The Docker infrastructure under `infrastructure/` predates Edition 1.2 and is
retained for future noncritical integration work. It is not the authoritative
development or production control path and must not be connected to live
equipment. Production Core runs natively under systemd in the dedicated Linux
amd64 AquaOS Control VM.
