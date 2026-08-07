# Proxmox design

| Guest | Suggested starting resources | Purpose |
| --- | --- | --- |
| AquaOS Control VM | 1–2 vCPU, 1–2 GB RAM, 8–16 GB disk | Authoritative native Linux amd64 Core under systemd |
| Home Assistant OS VM | 2 vCPU, 4 GB RAM, 32 GB disk | UI, notifications, integrations |
| Docker VM/LXC | 4 vCPU, 8 GB RAM, 80 GB+ disk | Mosquitto, InfluxDB, Grafana, Node-RED |
| AI VM | 4–8 vCPU, 16 GB+ RAM, GPU as available | Optional non-critical analytics |

The Control VM is dedicated, minimal Linux with bridged LAN networking and a
reserved/static address. Install AquaOS Core inside it as a native systemd
service. Never install AquaOS on the Proxmox host, and never place Docker in the
critical Core path. Configure automatic VM startup with the Control VM ahead of
noncritical guests and preserve it as long as practical during shutdown.

A VM does not survive physical host failure. Protect the host and required
network infrastructure with an appropriate UPS strategy, keep configuration and
VM backups on a separate physical target, and regularly prove restore onto a
replacement host. Independent physical equipment safeguards remain mandatory.

Keep optional-service data on the dedicated storage volume. Snapshot before
upgrades, back up guest configuration and data independently, and test restore
procedures before relying on the system.

Network services should use reserved addresses. Only expose remote access through an authenticated VPN or equivalent secure gateway; never expose MQTT or Node-RED directly to the internet.
