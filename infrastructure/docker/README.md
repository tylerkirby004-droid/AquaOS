# Docker service stack

This Compose project supplies noncritical services on the dedicated AquaOS
appliance. The standard installer starts Mosquitto and Home Assistant. The
`--advanced-history` profile additionally starts InfluxDB and Grafana. AquaOS
Core runs natively under systemd and continues when this entire stack is down.
The obsolete Reef-Pi Telegraf bridge and publisher are not started.

Service images are pinned to a major or minor release line so a routine pull
cannot silently cross a breaking major-version boundary. Review release notes,
back up persistent volumes, and validate rollback before changing these pins.

Create the three Mosquitto users before startup. Run `-c` only for the first
user because it overwrites an existing file:

```text
docker run --rm -it -v ./mosquitto/config:/mosquitto/config eclipse-mosquitto:2 mosquitto_passwd -c /mosquitto/config/passwd aquaos-core
docker run --rm -it -v ./mosquitto/config:/mosquitto/config eclipse-mosquitto:2 mosquitto_passwd /mosquitto/config/passwd home-assistant
docker run --rm -it -v ./mosquitto/config:/mosquitto/config eclipse-mosquitto:2 mosquitto_passwd /mosquitto/config/passwd aquaos-vision
```

The ACL is scoped to site `home-reef`; replace that value consistently if your
`mqtt.site_id` differs. Set restrictive permissions on `.env` and `passwd`.

Copy `.env.example` to `.env`, replace every placeholder, then run `docker
compose config` and `docker compose up -d`. Grafana provisions the InfluxDB
datasource and starter dashboard and is available on port 3000. See
`docs/installation.md` for firewall, AquaOS, and Home Assistant steps.

## Advanced Node-RED add-on

Node-RED is excluded from the standard AquaOS installation. It is neither an
AquaOS control component nor a supported location for aquarium business logic.
An advanced operator who needs a non-authoritative integration bridge may start
the separately defined add-on explicitly:

```text
docker compose -f compose.yaml -f compose.nodered.yaml config --quiet
docker compose -f compose.yaml -f compose.nodered.yaml up -d nodered
```

Secure port 1880 and configure Node-RED authentication before LAN access. Never
give it AquaOS administrator, adapter, or actuator credentials. AquaOS must
continue functioning when this add-on is absent or stopped.
