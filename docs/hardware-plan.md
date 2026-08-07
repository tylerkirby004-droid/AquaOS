# Hardware plan

## Current platform roles

| Component | Role | Notes |
| --- | --- | --- |
| Dedicated Linux amd64 VM | AquaOS Control VM | Native systemd Core; bridged LAN; automatic Proxmox startup |
| Raspberry Pi 4B | Optional display/kiosk | Home Assistant, Grafana, and AquaOS status only; zero control authority |
| Raspberry Pi 3B | Legacy/compatibility reserve | Reserve for Reef-Pi research only |
| Robo-Tank hardware | Relay/PWM and sensor interface | Document channel mapping before deployment |
| Intel i7-6700 / 64 GB | Physical Proxmox host | Host failure stops Core; never install AquaOS directly here |
| AMD RX 6700 | AI evaluation | NVIDIA remains preferred for broad local-model compatibility |

## Storage

Use the existing 500 GB SSD for Proxmox and add a 2 TB NVMe drive for VM disks,
time-series data, and models. AquaOS configuration and Control VM backups must
also be copied to a separate physical target, with restore proven on replacement
hardware. Plan appropriate UPS protection for the host, router, switch, and
wireless access point.

## Required inventory before live control

- Control VM hostname and reserved/static address
- Proxmox and systemd automatic-start behavior, shutdown ordering, and recovery runbook
- Tested configuration backup, VM backup, restore, and replacement-host procedure
- Relay/PWM channel, equipment, normal state, maximum on-time, and local fallback
- Sensor type, calibration procedure, units, and sampling interval
- Power and GFCI/protection topology
- Independent physical safety devices for hazardous equipment
- Optional MQTT broker credentials and TLS policy, when MQTT is enabled
