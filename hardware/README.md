# Hardware baseline

The governing Edition 1.2 bench direction is:

- dedicated minimal Linux amd64 AquaOS Control VM on Proxmox, with AquaOS Core
  running natively under systemd;
- Ethernet/PoE ESP32 sensor node with two independently identified waterproof
  DS18B20 probes;
- Shelly Plug US Gen4 outputs using direct local control;
- lamp or safe resistive bench load, never a live heater or livestock system;
- independent physical high-temperature protection for each future heater
  channel.

The Raspberry Pi 4B is an optional fish-room display/kiosk only. The display Pi,
Docker, MQTT, Home Assistant, storage, AI, and Internet access are not in the
critical control path. A Proxmox host failure does stop Core and must be
mitigated operationally rather than obscured.

No production adapter behavior was implemented in Prompts 1–7. Exact wiring,
firmware, protocol, cutoff choice/setpoint, and Atlas Scientific chemistry
integration require later bench evidence and ADRs. Do not infer them from
legacy files.
