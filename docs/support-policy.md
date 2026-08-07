# Compatibility and support policy

Linux amd64 on a dedicated Debian-family Control VM is the primary target.
Linux arm64 is a portability target, not the production recommendation. The
Proxmox host, containers, Windows, macOS, direct Internet exposure, and direct
installation on a hypervisor are unsupported Core environments.

Until v1.0 is approved, every build is pre-release. After v1.0, the latest minor
release line receives security and correctness fixes. Contract compatibility
follows `docs/compatibility-v1.md`. Hardware is supported only for the exact
model, firmware, wiring, and network conditions recorded by approved evidence.

Home Assistant, MQTT, observability, AI, Internet access, and the display Pi are
optional. Operators remain responsible for UPS coverage, independent physical
safeguards, backup testing, and replacement-host recovery.
