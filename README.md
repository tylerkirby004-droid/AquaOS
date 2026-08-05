# AquaOS

Open Aquatic Automation Platform — a local-first, modular ecosystem for monitoring, automation, analytics, and assisted operation of advanced aquariums.

## Design principle

**Reef-Pi and Robo-Tank retain safety-critical control.** AquaOS services provide observability, integrations, automation, and recommendations; loss of the server must not stop normal aquarium operation.

## Repository layout

- `docs/` — architecture, hardware, MQTT contract, and project plans
- `infrastructure/` — deployable service definitions and platform notes
- `homeassistant/` — Home Assistant configuration packages
- `reefpi/` — controller integration notes and configuration exports
- `ai/` — non-critical analytics and vision experiments
- `hardware/` — interface inventory and wiring documentation
- `development/` — local development assets

## Start the development stack

1. Copy `infrastructure/docker/.env.example` to `infrastructure/docker/.env` and choose secure credentials.
2. Review the volumes in `infrastructure/docker/compose.yaml` and bind-mount them to persistent storage on the Docker host.
3. From `infrastructure/docker`, run `docker compose up -d`.
4. Configure Reef-Pi or a bridge to publish retained telemetry using the topic contract in [docs/mqtt-topics.md](docs/mqtt-topics.md).

The included Compose stack runs Mosquitto, InfluxDB, Grafana, Node-RED, Telegraf, and a development-only Reef-Pi telemetry simulator. Home Assistant OS remains a dedicated Proxmox VM.

## Documentation

See [docs/local-development.md](docs/local-development.md), [docs/architecture.md](docs/architecture.md), [docs/proxmox-design.md](docs/proxmox-design.md), and [docs/roadmap.md](docs/roadmap.md) for the initial operating design.
