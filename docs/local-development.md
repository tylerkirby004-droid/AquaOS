# Local development

You can develop the AquaOS data pipeline on a Windows computer before building the Proxmox server. This starts **simulated data only** and does not connect to aquarium equipment.

## Prerequisites

- Docker Desktop with its Linux container engine running
- A terminal in `infrastructure/docker`

## First run

```powershell
Copy-Item .env.example .env
```

Edit `.env` and replace each placeholder password/token. The MQTT username needs to remain `reefpi` unless you also update the ACL and simulator environment.

Create the broker password file. Enter the same password as `MQTT_REEFPI_PASSWORD` when prompted:

```powershell
docker run --rm -it -v "${PWD}/mosquitto/config:/mosquitto/config" eclipse-mosquitto:2 mosquitto_passwd -c /mosquitto/config/passwd reefpi
docker compose up --build -d
```

After one or two minutes, open Grafana at `http://localhost:3000`, sign in with user `admin` and `GRAFANA_ADMIN_PASSWORD`, then select **Dashboards → AquaOS → AquaOS Overview**.

## What is running

```text
telemetry-simulator -> Mosquitto -> Telegraf -> InfluxDB -> Grafana
                                     |
                                     +-> Node-RED (available for workflow development)
```

The simulator publishes synthetic values every 15 seconds. It is intentionally distinct from any live controller. Remove `telemetry-simulator` from `compose.yaml` before configuring Reef-Pi.

## Development checks

```powershell
docker compose ps
docker compose logs telemetry-simulator telegraf --tail 50
docker compose down
```

`docker compose down` retains named volumes and data. Add `--volumes` only when deliberately resetting local development data.
