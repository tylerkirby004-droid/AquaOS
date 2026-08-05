# Hardware plan

## Current control hardware

| Component | Role | Notes |
| --- | --- | --- |
| Raspberry Pi 3B | Dedicated controller | Reserve for Reef-Pi only |
| Robo-Tank hardware | Relay/PWM and sensor interface | Document channel mapping before deployment |
| Intel i7-6700 / 64 GB | Development server | Proxmox host |
| AMD RX 6700 | AI evaluation | NVIDIA remains preferred for broad local-model compatibility |

## Storage

Use the existing 500 GB SSD for Proxmox and add a 2 TB NVMe drive for VM disks, time-series data, models, and backups. Confirm backups are copied to a separate physical target.

## Required inventory before live control

- Controller hostname and static/reserved IP
- Relay/PWM channel, equipment, normal state, maximum on-time, and local fallback
- Sensor type, calibration procedure, units, and sampling interval
- Power and GFCI/protection topology
- MQTT broker credentials and TLS policy
