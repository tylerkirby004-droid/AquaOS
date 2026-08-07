# Local development

## Safe foundation path

Run on: developer workstation

```sh
make bootstrap
```

Expected result: the command verifies Go/config prerequisites, starts AquaOS
with MQTT disabled and the hardware-incapable simulator enabled, confirms
readiness, then shuts every component down within the configured timeout.

For manual inspection, run:

```sh
make run
```

In another terminal:

```sh
go run ./cmd/healthcheck -url http://localhost:8080/health/ready
```

Expected result: the health check succeeds. Stop AquaOS with `Ctrl+C` and verify
ordered shutdown logs. No live hardware may be connected during foundation
development.

## Optional legacy integration stack

The Compose files under `infrastructure/docker` are retained as noncritical
integration experiments. They are not required by AquaOS Core, are not used by
`make bootstrap`, and must not be treated as an active MQTT or equipment
contract until the corresponding Development Bible prompts are completed.
The root Compose file is likewise a developer convenience only. Docker must not
become a dependency of the production Core control path.
