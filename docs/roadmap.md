# Roadmap

## Phase 0 — Foundation

- Install Proxmox and persistent storage
- Deploy the Docker stack and Home Assistant VM
- Create controller/equipment inventory and backups

## Phase 1 — Observability

- Bridge Reef-Pi telemetry to the MQTT contract
- Calibrate and validate temperature, pH, and salinity measurements
- Build Home Assistant and Grafana dashboards

## Phase 2 — Guarded automation

- Add alerting and acknowledgement flows
- Implement non-critical Node-RED workflows
- Test loss of broker, Docker host, and network while controller safety remains intact

## Phase 3 — Analysis

- Add maintenance trends and anomaly detection
- Evaluate local GPU-backed vision/data workloads
- Publish recommendations only; require explicit human/controller-approved actions

## Phase 4 — Expansion

- Add tanks/sites using the MQTT namespace
- Document replaceable controller bridges and hardware interfaces
- Rehearse migration and disaster recovery
