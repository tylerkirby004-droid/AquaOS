# AquaOS Easy Installation

This is the shortest supported path for a new installation. It creates three
separate Proxmox VMs: AquaOS Core, optional services, and Home Assistant OS.
It never installs AquaOS on the Proxmox host. Keep independent thermostat,
overflow, dosing, and circuit protection in place; software cannot replace
those safeguards.

## Before you begin

You need a supported Proxmox host on a UPS, a second computer with SSH and SCP,
three reserved private-LAN addresses, and an off-host backup destination. In
the Proxmox web interface, enable automatic VM startup and configure scheduled
backups to storage that does not live only on the same physical host.

Download the current Debian generic cloud image and the Home Assistant OS QCOW2
image from their official projects. Record each version and published SHA-256.
Copy the verified images and this repository's
`scripts/prepare-proxmox-templates.sh` to the Proxmox node. Run the script first
without `--apply`; review its dry-run summary, then repeat with `--apply`.

```sh
sudo ./prepare-proxmox-templates.sh \
  --debian-image ./debian.qcow2 --debian-sha256 PUBLISHED_SHA256 \
  --haos-image ./haos.qcow2 --haos-sha256 PUBLISHED_SHA256 \
  --storage YOUR_STORAGE --bridge YOUR_BRIDGE \
  --debian-vmid UNUSED_TEMPLATE_ID --haos-vmid UNUSED_TEMPLATE_ID
# Review, then repeat the same command with --apply.
```

The script refuses existing VM IDs and verifies both image checksums before it
creates anything. It never deletes a VM.

## Run the guided installer

Use `aquaos-deploy-windows-amd64.exe` on Windows or
`aquaos-deploy-linux-amd64` on Linux. Keep the signed release directory and a
repository checkout beside it. Ensure your SSH host keys are already trusted;
the installer deliberately refuses first-use host-key prompts.

```text
aquaos-deploy-windows-amd64.exe init
aquaos-deploy-windows-amd64.exe plan --config aquaos-deployment.json
```

The first command asks plain-language questions and saves a private
configuration file. The second prints every command without changing the
server. Check the three VM IDs, addresses, storage, bridge, and template IDs.
Then apply the reviewed plan:

```text
aquaos-deploy-windows-amd64.exe apply --config aquaos-deployment.json --ack-create-vms --ack-independent-backups
```

The installer clones and starts the VMs, installs the signed native systemd
Core service, installs Mosquitto, InfluxDB, and Grafana on the optional-services
VM, and reports the Home Assistant onboarding address. A failure stops the
sequence; it does not delete or overwrite VMs to retry.

## Finish the browser setup

1. Open the reported `http://HOME_ASSISTANT_ADDRESS:8123` address and create the
   Home Assistant owner account.
2. On the optional-services VM, run
   `sudo cat /root/aquaos-services-credentials.txt`. Store those credentials in
   a password manager, then add the MQTT integration in Home Assistant using
   the `home-assistant` account.
3. Open the AquaOS Admin GUI through the documented SSH tunnel. Its guided
   setup discovers only addresses you enter, maps Shelly outlets and ESP32
   probes, records calibration and alarm policy, and validates configuration.
4. For every physical output, use a harmless test load. Save the bench-test
   evidence, apply it, and only then use the separate commissioning action.
5. In **Backup destination**, select a mounted directory outside the application
   directory. Run a backup and restore it on a replacement Control VM before
   relying on the installation.

The Admin GUI is not authoritative: all changes pass through authenticated
application services. Home Assistant, MQTT, metrics, Grafana, Internet, AI,
and the display Pi may all fail without stopping critical local control.

## What still requires a person

Home Assistant's first owner account, physical wiring inspection, outlet/load
identification, calibration reference handling, independent safety devices,
and the final commissioning decision cannot be automated safely. Complete the
hardware fault tests and 72-hour soak in the release-candidate checklist before
connecting aquarium-critical loads.

Use the current upstream instructions for [Proxmox VE](https://pve.proxmox.com/pve-docs/pve-admin-guide.html),
[Home Assistant OS virtual machines](https://www.home-assistant.io/installation/alternative),
and [Debian cloud images](https://cloud.debian.org/images/cloud/). The upstream
image and checksum instructions take precedence over copied examples.
