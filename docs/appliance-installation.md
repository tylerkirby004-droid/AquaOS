# Dedicated Debian appliance installation

For a new computer, prefer the [bootable USB installer](usb-installation.md).
The procedure below is the recovery and developer installation path.

The easiest complete AquaOS installation uses a dedicated x86-64 computer with
current Debian stable. It is not suitable for a shared desktop, NAS, Docker
host, or hypervisor host. The machine remains one physical failure domain.

## What the installer provides

- Native AquaOS Core under systemd; Docker is not in its control path.
- TLS-protected AquaOS Admin GUI on the operator-selected LAN address.
- Home Assistant Container with a generated AquaOS dashboard.
- Local Home Assistant persistent notifications for active AquaOS alarms.
- Mosquitto and Home Assistant's built-in history in the standard profile.
- Optional InfluxDB and provisioned Grafana dashboards with
  `--advanced-history`.
- Automatic dashboard regeneration after accepted AquaOS configuration changes.
- Root-only generated credentials and resource priority favoring Core.

Install Debian amd64 with OpenSSH, reserve the computer's LAN address in the
router, connect it to a UPS, and copy the signed AquaOS release and repository
checkout onto it. Run the installer without `--apply` first:

```sh
sudo ./scripts/install-appliance.sh \
  --version vVERSION --sha256 CORE_SHA256 --site-id home-reef \
  --address RESERVED_LAN_ADDRESS
```

Review the displayed URLs and roles. Then repeat the identical command with:

```text
--apply --ack-dedicated-appliance --ack-independent-safeguards
```

The installer does not activate Shelly or ESP32 adapters. When it finishes:

1. Read `/root/aquaos-appliance-credentials.txt` locally and store it in a
   password manager.
2. Open the printed Admin URL, accept the locally generated certificate only
   after comparing its host/address, and enter the Admin access code.
3. Open Home Assistant on port 8123 and create its first owner account.
4. Add the MQTT integration using the `home-assistant` credential from
   `/root/aquaos-services-credentials.txt` and the appliance address.
5. Use the AquaOS Admin wizard to discover, map, calibrate, and bench-test
   devices. Apply bench evidence before the separate commissioning action.
6. Configure an off-host backup destination and prove restoration on another
   computer.

Home Assistant automatically receives stable entities. The AquaOS sidebar
dashboard displays Core/alarm state, validated sensors, equipment switches, and
AquaOS history. The advanced profile also displays the provisioned Grafana
history view. Every switch request still traverses the
AquaOS command and safety pipeline. Home Assistant persistent notifications are
noncritical and may be extended in Home Assistant with its mobile, email, or
webhook notification actions.

## Required acceptance tests

Run `sudo /opt/aquaos/bin/aquaosctl verify`, restart Core, and reboot the
machine. Stop Docker and confirm Core readiness plus direct device operation:

```sh
sudo systemctl stop docker
sudo /opt/aquaos/bin/aquaosctl verify
curl --fail http://localhost:8080/health/ready
sudo systemctl start docker
```

Record rather than assume the clean-machine install, real Shelly/ESP32 firmware
results, physical cutoff tests, backup restoration, power recovery, and 72-hour
soak. Until those are complete, the installation is a simulator/bench system.

The same outage test can be run with automatic service restoration and a
timestamped evidence file:

```sh
sudo ./scripts/verify-appliance-isolation.sh \
  /var/lib/aquaos/appliance-isolation.txt --ack-stop-optional-services
```
