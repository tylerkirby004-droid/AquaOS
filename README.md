# AquaOS

AquaOS is a local-first, safety-first reef aquarium control platform. AquaOS
Core is an authoritative Go process whose primary production target is a
dedicated minimal Linux amd64 AquaOS Control VM on Proxmox, running natively
under systemd. It will validate sensor data, apply deterministic safety
policy, manage equipment state machines, issue commands through local hardware
adapters, reconcile reported state, raise alarms, and expose local APIs.

> **Safety status:** AquaOS is pre-release foundation software. It is not ready
> to control live aquarium equipment or livestock. It is not a substitute for
> independent physical safeguards, proper electrical protection, or husbandry
> oversight. Use only the hardware-incapable simulator until later bench and
> safety gates pass.

The governing project specification is the checked-in
[AquaOS Development Bible, Edition 1.2](docs/AquaOS_Development_Bible_Edition_1.2.docx).
The older Edition 1.1 file remains only as historical context for Prompts 1–7.

## Authority and failure boundary

```text
Shelly / Ethernet-PoE ESP32 devices on the local LAN
                         |
                         v
       AquaOS Core in dedicated Control VM <----> local API
                         |
                         +---- optional MQTT ---- Home Assistant / integrations
                         +---- optional storage - InfluxDB / Grafana
                         +---- optional AI ------ observations only

Optional Raspberry Pi display/kiosk ---- reads status and dashboards only
```

AquaOS Core is the authoritative controller. MQTT is the primary external
integration and telemetry backbone, but broker loss must never stop local
sensing, safety evaluation, state machines, output commands, or reconciliation.
Home Assistant routes all control and configuration requests through AquaOS
Core. Optional storage, dashboards, remote servers, Internet access, and AI are
outside the critical control path. Reef-Pi is reserved for possible future
compatibility and is not the baseline hardware authority.

Docker is supported only for noncritical development and integration work; it
is not a production dependency for AquaOS Core. The display Pi is likewise
noncritical. A physical Proxmox-host failure does stop the Control VM, so
independent equipment safeguards, appropriate UPS coverage, automatic VM and
service startup, tested backups/restores, and replacement-host recovery remain
mandatory operational controls. AquaOS must never be installed directly on the
Proxmox host.

Home Assistant is the normal operational UI. The AquaOS Admin GUI is for
installation, configuration, diagnostics, backup/restore, upgrades, rollback,
and repair. It is non-authoritative: every mutation must pass through
authenticated AquaOS APIs/application services and existing safety policy.

## Foundation architecture

The process is a modular monolith with explicit construction in `internal/app`,
structured JSON logging, strict versioned YAML configuration, ordered and
bounded lifecycle startup/reverse shutdown, four-state aggregate health, and
context-owned goroutines.
Canonical health routes are:

- `GET /health/live` — process liveness without dependency checks
- `GET /health/ready` — readiness across enabled components

Health distinguishes process liveness, required-component readiness, optional
integration degradation, and unhealthy required services. MQTT failure can
degrade integration health without disabling authoritative local control.

The older `/healthz` and `/readyz` paths remain temporary compatibility aliases.
The lifecycle simulator adapter starts no goroutines and opens no hardware
connections. Prompt 7 adds a separate deterministic workbench and fake output
adapter; both remain in-memory and structurally incapable of contacting real
hardware.

Prompt 3 adds transport-independent typed IDs, quantities, units, quality,
capabilities, ownership-aware registries, and revisioned canonical state with
bounded non-blocking subscriptions. It does not add equipment commands, safety
policy, persistence, or protocol adapters.

Prompt 6 defines the MQTT v1 integration contract: generated versioned topics,
strict envelopes, fixed QoS/retain policy, bounded idempotent consumption,
command request/result routing through the Core output service, retained
availability, and reconnect reconciliation. MQTT remains outside the survival
path. See [docs/mqtt-topics.md](docs/mqtt-topics.md) for the contract and
Mosquitto ACL guidance.

Prompt 7 adds reproducible tank and fault scenarios for temperature, level,
heater, pumps, ATO, leaks, sensor faults, acknowledgements, and optional-service
loss. Run the normal trace with `make simulate`; see
[docs/simulator.md](docs/simulator.md) before authoring or changing fixtures.

Prompt 8 introduces explicitly activated direct-LAN boundaries for Shelly Plug
US Gen4 and versioned Ethernet/PoE ESP32 dual-probe nodes. The safe default still
uses the simulator and rejecting output router. See
[docs/prompt-8-bench-checklist.md](docs/prompt-8-bench-checklist.md) and
[docs/esp32-node-protocol.md](docs/esp32-node-protocol.md). Physical bench
evidence is required before Prompt 8 can be declared complete.

Prompt 9 adds the authenticated, versioned REST boundary used by integrations
and the non-authoritative Admin GUI. See [docs/rest-api.md](docs/rest-api.md)
and [docs/openapi-v1.yaml](docs/openapi-v1.yaml).

Prompt 10 adds optional Home Assistant MQTT Discovery and a narrow command
bridge that always routes through the Core output service. See
[docs/home-assistant.md](docs/home-assistant.md).

Prompt 11 adds bounded optional InfluxDB persistence and sample Grafana
provisioning. Storage failure never blocks control; see
[docs/persistence.md](docs/persistence.md).

Prompt 12 adds the native Control VM operations service, headless `aquaosctl`,
and separately launched embedded Admin GUI. See
[docs/control-vm-installation.md](docs/control-vm-installation.md). Physical
clean-VM and cross-server acceptance evidence remains required.

Prompts 13–16 add security gates, frozen v1 contracts, a traceable acceptance
checklist, the removable `aquaos-vision` scaffold, and reproducible candidate
packaging. AquaOS is still not v1.0: physical and 72-hour evidence remains
release-blocking. See [docs/easy-test.md](docs/easy-test.md) for the shortest
safe evaluation path.

## Safe development startup

Requirements:

- Go version pinned by `go.mod`
- No MQTT broker, database, dashboard, hardware, or Python service

Run the single broker-free bootstrap command:

```sh
make bootstrap
```

It validates the development configuration, confirms MQTT is disabled and the
hardware-incapable simulator is enabled, constructs the dependency graph,
starts the process components, verifies readiness, and performs bounded graceful
shutdown. It fails instead of continuing when a checkpoint does not pass.

To run AquaOS manually with the safe sample configuration:

```sh
go run ./cmd/aquaos -config configs/aquaos.yaml
```

Then, from another terminal:

```sh
go run ./cmd/healthcheck -url http://localhost:8080/health/ready
```

Stop with `Ctrl+C`. Expected result: logs show reverse-order graceful shutdown.
Do not connect live equipment.

## Configuration

A complete external YAML file with `schema_version: 1` is required through
`-config` or `AQUAOS_CONFIG`.
The safe sample binds HTTP to localhost, enables the foundation simulator, and
leaves MQTT disabled. Supported environment overrides are documented in the
configuration package and use `AQUAOS_UPPER_SNAKE_CASE`. Secrets must stay out
of committed YAML and logs. Inline MQTT passwords are rejected. Configuration
reload is atomic: only `application.log_level` is hot-reloadable in v1; unsafe
changes are rejected with restart reasons and leave the active digest and
snapshot unchanged. See [docs/configuration.md](docs/configuration.md) and
[configs/schema-v1.json](configs/schema-v1.json).

## Native Control VM foundation deployment

The Linux amd64 binary, sample configuration, hardened systemd unit, local
verification command, rollback, and host-failure guidance are documented
in [deployments/systemd/README.md](deployments/systemd/README.md). This is a
manual dedicated-Control-VM deployment, not the guided installer, Admin GUI,
or full backup/restore system scheduled for later milestones.

## Repository layout

- `cmd/aquaos` — process entry point and signal handling
- `cmd/devbootstrap` — broker-free development preflight and verification
- `internal/app` — dependency composition and lifecycle ownership
- `internal/config` — typed external configuration
- `internal/domain` — typed IDs, quantities, units, quality, and capabilities
- `internal/devices`, `internal/sensors`, `internal/equipment` — ownership-aware inventory
- `internal/state` — revisioned desired/reported/observation state
- `internal/events` — typed envelopes and bounded, failure-visible delivery
- `internal/alarms` — generic debounced, hysteretic, and latching alarm lifecycle
- `internal/telemetry` — build metadata and observability boundary
- `internal/mqtt` — optional external broker transport skeleton
- `internal/api`, `internal/health` — operational HTTP and health
- `internal/output` — sole validated command, acknowledgement, and reconciliation path
- `internal/safety` — operating modes, interlocks, overrides, and watchdog policy
- `internal/adapters/{shelly,esp32,simulator}` — adapter boundaries
- `internal/bench` — Prompt 8 adapter-to-state/alarm/safe-response coordination
- `deployments/systemd` — minimal native Linux Control VM deployment
- `docs/adr` — architecture decision records
- `configs` — non-secret example configurations

Event backpressure, handler-failure policy, alarm transition semantics, and the
future clustering boundary are documented in
[docs/typed-events-and-alarms.md](docs/typed-events-and-alarms.md).
The equipment state machines, command acknowledgement boundary, and hard-limit
policy are documented in [docs/equipment-safety.md](docs/equipment-safety.md).

## Development checks

```sh
make fmt
make vet
make staticcheck
make test
make lint
make build-all
```

`make build-all` produces primary Linux amd64 and portability Linux arm64
AquaOS, verification-helper, and simulator binaries. CI runs formatting, vet,
staticcheck, golangci-lint, race-enabled tests, coverage, and both Core target
builds.

See [CONTRIBUTING.md](CONTRIBUTING.md), [SECURITY.md](SECURITY.md), and
[AGENTS.md](AGENTS.md) before making changes. Licensed under the
[MIT License](LICENSE).
