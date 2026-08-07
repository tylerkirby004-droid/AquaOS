# AquaOS Control VM installation and recovery

## Supported baseline

- Dedicated minimal Debian or Ubuntu Linux amd64 VM on Proxmox
- Bridged LAN interface with local access to configured Shelly and ESP32 nodes
- Reserved DHCP lease or documented static address
- Proxmox VM automatic start enabled and ordered after required network/storage
- Host and network equipment on an appropriately sized UPS
- AquaOS is never installed on the Proxmox host

## Quick Start candidate

The commands below require a signed release bundle and must first be exercised on a non-livestock bench VM:

```sh
sudo ./aquaosctl-linux-amd64 install \
  --binary ./aquaos-linux-amd64 \
  --config ./aquaos.yaml \
  --version v0.8.0 \
  --sha256 "$(cut -d' ' -f1 aquaos-linux-amd64.sha256)" \
  --signature ./aquaos-linux-amd64.sig.hex \
  --public-key ./aquaos-ed25519-public-key.hex \
  --ack-control-vm

sudo /opt/aquaos/bin/aquaosctl verify
```

Run `scripts/verify-control-vm.sh` afterward. The installer creates the `aquaos` service account, managed directories, native systemd unit, validated configuration, and atomic executable. Re-running the same install is idempotent and does not overwrite an existing configuration.

## Operations

`aquaosctl` supports `install`, `status`, `verify`, `configure`, `repair`, `backup`, `restore`, `upgrade`, `rollback`, `diagnostics`, `remove-role`, and `uninstall`. Every mutating command supports either a dry-run directly or a validation phase before activation. Backups contain validated configuration and version metadata with per-file SHA-256 checksums; restore rejects extra paths and checksum mismatch.

Start the optional recovery GUI only when needed:

```sh
sudo /opt/aquaos/bin/aquaos-admin \
  -address 127.0.0.1:8090 \
  -token-file /etc/aquaos/secrets/admin.token \
  -authentication-rate 5 -authentication-burst 10 \
  -mutation-rate 2 -mutation-burst 4
```

Open the displayed `/admin/` address through the documented SSH tunnel and
enter the administrator access code. The guided interface loads the active
configuration through the operations application service and walks through:

1. Control VM verification.
2. Optional MQTT/Home Assistant and InfluxDB settings.
3. Generic device, sensor, and equipment inventory.
4. Complete candidate validation and a required bench-safety acknowledgement.
5. Application through the same atomic configuration path as `aquaosctl`.

The advanced configuration preview is diagnostic only. The browser never
writes AquaOS files directly and never switches physical equipment. Adapter
endpoint commissioning and user-defined alarm-policy editing remain separate
release gates until their typed configuration and safety services are complete.

Use an SSH tunnel for remote browser access. Do not expose this recovery
listener broadly on the LAN. Rate limits are external flags so operators can
tighten them for their management path; both client maps are bounded. Prompt 13
also requires long random tokens, same-origin mutations, no browser credential
persistence, restrictive security headers, and redacted client errors.

## Replacement-host recovery

1. Restore Proxmox networking and UPS protections on the replacement host.
2. Create a clean supported Control VM with bridged LAN and automatic start.
3. Install the exact signed AquaOS release matching the backup metadata.
4. Run `aquaosctl restore --file <backup>` and then `verify`.
5. Confirm direct device reachability and reconcile every reported output using lamp loads only.
6. Re-run the Prompt 8 fault checklist before authorizing any aquarium equipment.

## Evidence still required

CI tests sandboxed idempotence, interruption, unrelated-file preservation, failed-upgrade rollback, archive validation, and replacement-host restore. Prompt 12 is not fully passed until a clean Linux amd64 VM Quick Start, first-time-user instruction test, Proxmox VM automatic-start/recovery exercise, backup/restore drill, and complete cross-server verification report are recorded.
