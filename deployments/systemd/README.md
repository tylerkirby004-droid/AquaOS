# Raspberry Pi OS Lite foundation deployment

This is the minimal v0.1 deployment for a Raspberry Pi 4B running Raspberry Pi
OS Lite 64-bit. It does not enable live aquarium loads and is not a guided
installer. Use the broker-free simulator configuration until later safety and
bench milestones pass.

## Prepare the artifact

Run on: developer workstation

```sh
make build-all
```

Expected result: `bin/aquaos-linux-arm64` and
`bin/aquaos-healthcheck-linux-arm64` exist. Copy them, `configs/aquaos.yaml`, and
`deployments/systemd/aquaos.service` to the Pi using your normal secure transfer
method. Verify release checksums when release artifacts become available.

## Install the foundation service

Run on: Raspberry Pi control node

```sh
sudo useradd --system --home /var/lib/aquaos --create-home --shell /usr/sbin/nologin aquaos
sudo install -o root -g root -m 0755 aquaos-linux-arm64 /usr/local/bin/aquaos
sudo install -o root -g root -m 0755 aquaos-healthcheck-linux-arm64 /usr/local/bin/aquaos-healthcheck
sudo install -d -o root -g aquaos -m 0750 /etc/aquaos
sudo install -o root -g aquaos -m 0640 aquaos.yaml /etc/aquaos/aquaos.yaml
sudo install -o root -g root -m 0644 aquaos.service /etc/systemd/system/aquaos.service
sudo systemctl daemon-reload
sudo systemctl enable --now aquaos.service
```

Expected result: `systemctl status aquaos.service` reports `active (running)`.
The sample configuration binds HTTP to localhost, enables only the
hardware-incapable foundation simulator, and leaves MQTT disabled.

## Verify locally

Run on: Raspberry Pi control node

```sh
/usr/local/bin/aquaos-healthcheck -url http://localhost:8080/health/ready
sudo systemctl stop aquaos.service
sudo systemctl start aquaos.service
/usr/local/bin/aquaos-healthcheck -url http://localhost:8080/health/ready
```

Expected result: both health checks exit successfully and the journal shows an
ordered, bounded shutdown followed by clean startup. Stop here if verification
fails. Do not connect or enable live loads.

## Hardware watchdog guidance

Raspberry Pi hardware-watchdog configuration is platform and bootloader
dependent. Before bench use, confirm the current Raspberry Pi OS documentation,
enable the hardware watchdog deliberately, and test forced process and Pi
failure using only the simulator and safe test loads. Do not set systemd
`WatchdogSec` for AquaOS yet: the v0.1 process does not implement `sd_notify`, so
doing so would create a restart loop rather than a valid watchdog guarantee.

The later Shelly/ESP32 bench milestone must verify process restart, Pi restart,
watchdog recovery, network loss, and independent physical heater cutoffs before
any livestock or live heater depends on AquaOS.

## Rollback

Run on: Raspberry Pi control node

```sh
sudo systemctl disable --now aquaos.service
```

Expected result: AquaOS is stopped and disabled. Because v0.1 has no equipment
behavior, rollback cannot leave a partially enabled AquaOS output path.
