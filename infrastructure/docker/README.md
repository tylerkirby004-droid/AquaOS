# Docker service stack

This Compose project is intended for the Docker guest, not the Proxmox host. It runs a development telemetry simulator by default; remove the `telemetry-simulator` service before connecting live equipment.

Create a Mosquitto password file before startup. Its user and password must match `MQTT_REEFPI_USERNAME` and `MQTT_REEFPI_PASSWORD` in `.env`:

```text
docker run --rm -it -v ./mosquitto/config:/mosquitto/config eclipse-mosquitto:2 mosquitto_passwd -c /mosquitto/config/passwd reefpi
```

Create a second `services` user and adjust `mosquitto/config/acl` to the least privilege needed. Set restrictive filesystem permissions on `.env` and `passwd`.

The included Telegraf service converts MQTT JSON telemetry to InfluxDB measurements, and Grafana automatically provisions an AquaOS datasource and starter dashboard. Grafana is available at `http://localhost:3000` when running locally.
