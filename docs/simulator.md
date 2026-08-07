# Deterministic simulator workbench

The Prompt 7 workbench is a test and development tool. It is not an equipment
controller and does not replace independent physical safeguards. Its model and
fake adapter have no GPIO, network, MQTT, database, subprocess, or vendor-client
dependency. The fixture loader performs bounded read-only file access. An
import-boundary test rejects network, process, hardware, and real-adapter
dependencies. The fake adapter implements only the Core-owned `output.Executor`
interface and stores reported state in memory.

## Running scenarios

Run the normal fixture:

```sh
make simulate
```

Run a fault fixture and save its JSON Lines trace:

```sh
go run ./cmd/aquaos-sim -scenario configs/scenarios/safety-faults.json
```

The command exits nonzero when a fixture is malformed, exceeds its resource
limit, or fails to produce a declared alarm or safe transition. Scenarios are
strict JSON conforming to
[`configs/schemas/simulator-scenario-v1.json`](../configs/schemas/simulator-scenario-v1.json).
Unknown fields and versions are rejected. Runs are bounded to 100,000 steps and
one MiB of source JSON.

## Model and trace contract

Every fixture supplies its start time, fixed step, random seed, physical rates,
supervisory thresholds, and fault schedule. A run is synchronous: it starts no
worker and never sleeps. Identical input produces byte-identical structured
results.

The thermal model combines ambient exchange and heater influence. The level
model combines evaporation and ATO fill. Return and circulation pumps have
desired/reported state; dosing is represented but remains off because dosing
chemistry is outside this milestone. Temperature uses hysteresis. Stale input
blocks heater activation, and a leak requests every modeled output off.

Supported faults are sensor noise, drift, staleness, relay stuck-on,
acknowledgement delay/loss, leak, return-pump failure, broker loss, and storage
loss. Broker and storage availability appear in traces but are deliberately not
inputs to local control.

Trace alarm codes and safe transitions are workbench acceptance evidence. They
do not claim that a physical action succeeded: desired and reported states stay
separate, and a stuck relay remains visibly divergent after an off request.

## Fixture maintenance

- Keep seeds explicit and stable.
- Add expected stable codes for safety-relevant faults.
- Never weaken a fixture merely to accommodate a changed implementation.
- Add a new schema version for incompatible fixture or trace meaning.
- Do not add real addresses, credentials, GPIO access, or vendor clients.

The normal fixture proves configured temperature supervision remains within its
declared tolerance. Safety fixtures prove leak, stale-sensor, stuck-relay, and
pump faults produce expected evidence and safe requests. Integration-loss tests
prove broker and storage failure do not change the local control trajectory.
