# Docker service stack

This Compose project is intended for a separate optional-services Docker guest,
not the Proxmox host or AquaOS Control VM. It runs Mosquitto, InfluxDB, and
Grafana. Node-RED is opt-in with the `nodered` profile. Core writes directly to
InfluxDB; the obsolete Reef-Pi Telegraf bridge and publisher are not started.

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
