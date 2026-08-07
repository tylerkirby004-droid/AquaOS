# Dedicated Control VM foundation deployment

This is the minimal native systemd deployment for a dedicated minimal Linux
amd64 AquaOS Control VM on Proxmox. It does not enable live aquarium loads and
is not the guided installer or Admin GUI planned for later milestones. Use the
broker-free simulator configuration until the Prompt 8 safety and bench gates
pass.

Do not install AquaOS directly on the Proxmox host. Docker, MQTT, Home
Assistant, storage, AI, Internet access, and the Raspberry Pi display are not
required for this Core service.

## Prepare the Control VM and artifact

Create a dedicated supported minimal Linux amd64 VM with bridged LAN networking.
Use a reserved/static address, configure automatic Proxmox startup ahead of
noncritical guests, and include the host and required network infrastructure in
UPS planning. The Edition 1.2 initial resource estimate is 1–2 vCPU, 1–2 GB RAM,
and 8–16 GB disk; revise it only from measured soak evidence.

Run on: developer workstation

```sh
make build-all
```

Expected result: `bin/aquaos-linux-amd64` and
`bin/aquaos-healthcheck-linux-amd64` exist. Copy them, `configs/aquaos.yaml`, and
`deployments/systemd/aquaos.service` to the Control VM using a secure transfer
method. Verify release checksums when release artifacts become available.

## Install the foundation service

Run on: AquaOS Control VM

```sh
sudo useradd --system --home /var/lib/aquaos --create-home --shell /usr/sbin/nologin aquaos
sudo install -o root -g root -m 0755 aquaos-linux-amd64 /usr/local/bin/aquaos
sudo install -o root -g root -m 0755 aquaos-healthcheck-linux-amd64 /usr/local/bin/aquaos-healthcheck
sudo install -d -o root -g aquaos -m 0750 /etc/aquaos
sudo install -o root -g aquaos -m 0640 aquaos.yaml /etc/aquaos/aquaos.yaml
sudo install -o root -g root -m 0644 aquaos.service /etc/systemd/system/aquaos.service
sudo systemctl daemon-reload
sudo systemctl enable --now aquaos.service
```

Expected result: `systemctl status aquaos.service` reports `active (running)`.
The sample configuration binds HTTP to localhost, enables only the
hardware-incapable simulator, and leaves MQTT disabled.

## Verify locally

Run on: AquaOS Control VM

```sh
/usr/local/bin/aquaos-healthcheck -url http://localhost:8080/health/ready
sudo systemctl restart aquaos.service
/usr/local/bin/aquaos-healthcheck -url http://localhost:8080/health/ready
```

Expected result: both health checks exit successfully and the journal shows an
ordered, bounded shutdown followed by clean startup. Also verify automatic
service startup after a Control VM reboot and automatic VM startup after a
controlled Proxmox stop/start. Stop if any checkpoint fails. Do not connect or
enable live loads.

## Supervision and physical failure boundary

The supplied unit has bounded restart and shutdown behavior. Do not set systemd
`WatchdogSec` yet: the process does not implement `sd_notify`, so enabling it
would create a restart loop rather than a valid watchdog guarantee.

Neither systemd nor a VM protects against physical Proxmox-host failure.
Independent physical equipment safeguards remain mandatory. Before future live
use, prove configuration and VM restore onto replacement hardware and maintain
an emergency recovery runbook. Full supported backup/restore, upgrade/rollback,
repair, and installer workflows belong to Edition 1.2 Prompt 12.

## Rollback

Run on: AquaOS Control VM

```sh
sudo systemctl disable --now aquaos.service
```

Expected result: AquaOS is stopped and disabled. Prompts 1–7 include no real
hardware adapter, so this foundation rollback cannot leave an AquaOS output
path enabled.
