# Proxmox design

| Guest | Suggested starting resources | Purpose |
| --- | --- | --- |
| Home Assistant OS VM | 2 vCPU, 4 GB RAM, 32 GB disk | UI, notifications, integrations |
| Docker VM/LXC | 4 vCPU, 8 GB RAM, 80 GB+ disk | Mosquitto, InfluxDB, Grafana, Node-RED |
| AI VM | 4–8 vCPU, 16 GB+ RAM, GPU as available | Optional non-critical analytics |

Keep service data on the dedicated storage volume. Snapshot before upgrades, back up guest configuration and data independently, and test restore procedures before relying on the system.

Network services should use reserved addresses. Only expose remote access through an authenticated VPN or equivalent secure gateway; never expose MQTT or Node-RED directly to the internet.
