# Compatibility and support policy

Linux amd64 on a dedicated Debian appliance is the sole supported production target.
Linux arm64 is a portability target, not the production recommendation. The
Containers, Windows, macOS, direct Internet exposure, shared general-purpose
hosts, and installation on a hypervisor host are unsupported Core environments.

Until v1.0 is approved, every build is pre-release. After v1.0, the latest minor
release line receives security and correctness fixes. Contract compatibility
follows `docs/compatibility-v1.md`. Hardware is supported only for the exact
model, firmware, wiring, and network conditions recorded by approved evidence.

Home Assistant, MQTT, observability, AI, Internet access, and the display Pi are
optional. Operators remain responsible for UPS coverage, independent physical
safeguards, backup testing, and replacement-host recovery.
