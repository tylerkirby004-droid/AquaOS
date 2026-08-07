# Dedicated Debian appliance foundation

The supported production host is a dedicated Debian stable amd64 computer.
AquaOS Core runs natively under systemd; optional containers must never become
part of its sensing, safety, state-machine, or output path.

Most operators should use [the guided installer](../../docs/installation.md).
These files support manual recovery and package development:

- `aquaos.service` is the hardened native Core unit.
- `aquaos-admin.service` runs the non-authoritative Admin GUI.
- `aquaos-admin.socket` can socket-activate that GUI.
- `tmpfiles.conf` creates runtime directories with explicit ownership.

Install the signed amd64 binaries in `/opt/aquaos/bin`, configuration in
`/etc/aquaos`, and mutable state in `/var/lib/aquaos`. Enable Core with
`systemctl enable --now aquaos.service`, then verify
`http://127.0.0.1:8080/health/ready`.

The appliance, network, and power source remain real failure domains. Use a
UPS, off-host tested backups, replacement-machine recovery instructions, and
independent physical equipment safeguards. Never commission live equipment
from an unproven manual installation.
