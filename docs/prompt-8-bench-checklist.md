# Prompt 8 Control VM bench checklist

This checklist is for a dedicated minimal Linux amd64 AquaOS Control VM, one
Shelly Plug US Gen4, one Ethernet/PoE ESP32 node, two independently identified
DS18B20 probes, and a lamp or other safe resistive test load. Never use a live
heater, production aquarium output, or livestock system for Prompt 8 evidence.

The planned production design may use two 400 W titanium heaters, but each
channel requires its own independent physical high-temperature cutoff. AquaOS
software, the Control VM, and a shared cutoff are not substitutes.

## Preconditions

- [ ] Record Control VM OS, kernel, AquaOS commit, CPU/RAM/disk, and bridged-LAN address.
- [ ] Record Proxmox host and VM automatic-start ordering; AquaOS starts before noncritical guests.
- [ ] Record Shelly model, hardware revision, firmware, RPC methods, channel, power-return configuration, reserved address, and rollback procedure.
- [ ] Record ESP32 hardware, Ethernet/PoE path, firmware commit, protocol schema, node/boot IDs, both DS18B20 IDs, and reserved address.
- [ ] Confirm the Shelly load is a lamp/safe resistive load and is within plug, cord, outlet, and GFCI ratings.
- [ ] Confirm no path from the bench configuration reaches production equipment.
- [ ] Confirm MQTT is disabled and Home Assistant, storage, AI, Internet access, and the display Pi can all be absent.
- [ ] Confirm configuration backup and previous known-safe binary are available.

Stop immediately on identity mismatch, unexpected output activation, excessive
heating, damaged wiring, wet electrical equipment, failed GFCI protection, or
an unclassified adapter error.

## Verification sequence

Each case records timestamps, correlation/command IDs, expected safe state,
reported state, alarm code, health state, recovery result, and pass/fail.

- [ ] Normal dual-probe agreement produces two good, fresh Celsius observations.
- [ ] Removal/failure of probe A marks A invalid without relabeling probe B.
- [ ] Removal/failure of probe B marks B invalid without relabeling probe A.
- [ ] Probe disagreement above the configured threshold marks both suspect and raises `esp32.probe_disagreement`.
- [ ] Frozen/stale timestamps produce stale quality and block dependent hazardous activation through existing safety policy.
- [ ] Duplicate and out-of-order sequences are rejected; an ESP32 reboot with a new boot ID safely permits sequence reset.
- [ ] A lamp on/off request is acknowledged but becomes successful only after matching `Switch.GetStatus` reported state.
- [ ] An already-on lamp at AquaOS startup is observed and reconciled without assuming the command succeeded.
- [ ] A manual Shelly button change is observed as reported-state divergence and never silently rewrites desired state.
- [ ] A Shelly reboot and configured power-return behavior are observed and reconciled.
- [ ] Command timeout or lost acknowledgement expires pending work, raises the specific adapter alarm, and requests the configured safe state through Core policy.
- [ ] LAN/Wi-Fi interruption makes the affected adapter unavailable and triggers its configured safe response without MQTT.
- [ ] AquaOS process restart reconstructs reported state before declaring commands successful.
- [ ] Control VM reboot restores systemd service and direct-LAN reconciliation.
- [ ] Controlled Proxmox VM stop/start restores the Control VM in the configured order.
- [ ] MQTT broker loss/recovery has no effect on direct sensing, safety evaluation, Shelly control, or reconciliation.
- [ ] Home Assistant, InfluxDB/Grafana, Node-RED, AI, Internet, and display-Pi outages have zero critical-control effect.
- [ ] Shelly or ESP32 recovery clears only the availability condition after verified fresh observations; acknowledgement never clears an active fault.
- [ ] Rollback disables the real adapters, restores the prior binary/configuration, starts the hardware-incapable simulator or rejecting executor, and verifies no partially enabled output path.

## Host failure evidence

A physical Proxmox-host failure takes Core down; this test must not claim
otherwise. Document UPS runtime/behavior for the host, router, switch, and
wireless access point; independent physical heater safeguards; manual fallback;
configuration and VM backup locations; restore proof; replacement-host recovery
time; and emergency contacts/runbook location.

Prompt 8 can pass bench gates without completing the polished installer, Admin
GUI, or full replacement-host automation assigned to Prompt 12, but it cannot
omit or hide this failure domain.
