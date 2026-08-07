# AquaOS installation guide

This is the supported installation path for AquaOS. Use a **dedicated x86-64
computer running Debian stable**. AquaOS is an appliance: do not install it on
a daily-use desktop, NAS, hypervisor host, or Internet-facing server.

The standard installation includes native AquaOS Core, its Admin GUI, Home
Assistant, and Mosquitto. Home Assistant provides live values and built-in
history graphs. Add `--advanced-history` only when you want longer retention
and richer Grafana dashboards backed by InfluxDB.

| Profile | Included | Recommended host |
| --- | --- | --- |
| Standard | Core, Admin GUI, Home Assistant, Mosquitto | 4 CPU threads, 8 GB RAM, 128 GB SSD |
| Advanced history | Standard plus InfluxDB and Grafana | 4+ CPU threads, 16 GB RAM, 512 GB SSD |

## 1. Prepare the computer

1. Install the current Debian stable amd64 release with OpenSSH enabled.
2. Connect Ethernet and reserve the computer's address in your router.
3. Put the computer, network switch, and router on a suitable UPS.
4. Update Debian: `sudo apt update && sudo apt full-upgrade -y`.
5. Copy the extracted, signed AquaOS release bundle to the computer.

Use independent heater thermostats, overflow protection, and appropriate
electrical protection. One computer failure can stop AquaOS; software is not a
substitute for independent physical safeguards.

## 2. Preview the installation

From the release directory, run the installer without `--apply`. Replace the
four uppercase examples with the values for your release and network:

```sh
sudo ./scripts/install-appliance.sh \
  --version vVERSION \
  --sha256 CORE_SHA256 \
  --site-id home-reef \
  --address APPLIANCE_LAN_ADDRESS
```

The preview changes nothing. It checks the host, release, address, and planned
services and prints the URLs that will be created. For advanced history, add
`--advanced-history` to this command and the apply command below.

## 3. Install

After reviewing the preview, repeat the same command with the two required
safety acknowledgements:

```sh
sudo ./scripts/install-appliance.sh \
  --version vVERSION \
  --sha256 CORE_SHA256 \
  --site-id home-reef \
  --address APPLIANCE_LAN_ADDRESS \
  --apply \
  --ack-dedicated-appliance \
  --ack-independent-safeguards
```

The installer places Core under systemd, creates credentials and certificates,
starts the selected noncritical services, and generates the Home Assistant
dashboard. It deliberately does not energize or commission equipment.

## 4. Open the setup pages

The installer prints the exact addresses. Normally they are:

- AquaOS Admin: `https://APPLIANCE_LAN_ADDRESS:8090`
- Home Assistant: `http://APPLIANCE_LAN_ADDRESS:8123`
- Grafana, advanced history only: `http://APPLIANCE_LAN_ADDRESS:3000`

Read `/root/aquaos-appliance-credentials.txt` and
`/root/aquaos-services-credentials.txt` locally, put the credentials in a
password manager, and do not copy them into ordinary notes.

In Home Assistant, create the first owner account. Add the MQTT integration
using the `home-assistant` username, its generated password, and the appliance
address. The generated **AquaOS** dashboard contains system health, alarms,
sensors, equipment, safe controls, and 24-hour Home Assistant history. In the
advanced profile it also links the provisioned Grafana dashboard.

## 5. Add aquarium hardware

Use AquaOS Admin in this order:

1. Discover a Shelly outlet or ESP32 sensor node.
2. Confirm its address, identity, firmware, and capabilities.
3. Map ports or channels to generic sensors and equipment.
4. Choose units, limits, stale-data behavior, alarm delay/severity/latching,
   and notification destinations.
5. Calibrate sensors with known references and save the evidence.
6. Run the dry-run and bench-test commissioning wizard with aquarium loads
   disconnected.
7. Commission one device at a time only after its physical fail-safe is proven.

All Admin and Home Assistant changes are requests to AquaOS Core. Neither UI
can bypass validation, safety policy, or the sole output manager.

## 6. Back up and prove failure behavior

Configure the Admin backup wizard to send encrypted backups to storage outside
this computer. Test restoration on replacement hardware before relying on it.
Then run:

```sh
sudo /opt/aquaos/bin/aquaosctl verify
sudo ./scripts/verify-appliance-isolation.sh \
  /var/lib/aquaos/appliance-isolation.txt \
  --ack-stop-optional-services
```

Reboot the computer and verify Core starts automatically. Stop Home Assistant,
Mosquitto, Grafana, InfluxDB, Internet access, and any display Pi, and verify
direct local sensing and control continue. Record real-hardware tests, power
recovery, backup restoration, independent cutoff tests, and the required
72-hour soak. Until these pass, treat the system as a bench installation.

## Updating or repairing

Use AquaOS Admin's upgrade and rollback workflows with a signed release, or the
equivalent headless `aquaosctl` commands. Always create an off-host backup first
and repeat readiness, outage, and device reconciliation checks afterward.

For diagnostics, run `systemctl status aquaos`,
`journalctl -u aquaos --since today`, and
`curl --fail http://127.0.0.1:8080/health/ready`. Never expose Core or Admin
directly to the Internet; use a trusted VPN for remote access.
