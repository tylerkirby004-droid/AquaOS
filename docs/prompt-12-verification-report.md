# Prompt 12 cross-server verification report

Status: **Not yet executed on physical infrastructure**

Record date, operator, release digest, signature key fingerprint, Proxmox host, Control VM OS/kernel, network topology, UPS scope, and all server roles. Attach pass/fail evidence for:

- Clean Linux amd64 Control VM Quick Start
- Refusal to install on the Proxmox host
- Bridged-LAN and reserved/static-address preflight
- systemd automatic start after VM reboot
- Proxmox VM automatic start after host maintenance
- Idempotent install and interrupted-install recovery
- Preservation of unrelated services/files
- Candidate configuration validation and failed-activation rollback
- Backup, replacement-VM restore, and replacement-Proxmox-host restore
- Signed upgrade, failed-upgrade rollback, explicit rollback, repair, and uninstall
- Admin GUI authentication and application-service-only mutations
- Headless recovery with GUI unavailable
- Optional role outage/removal without Core impact
- First-time-user execution without undocumented assistance

Do not change this status to passed until every item has linked evidence.
