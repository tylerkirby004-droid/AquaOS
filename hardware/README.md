# Hardware baseline

The governing Edition 1.1 bench direction is:

- Raspberry Pi 4B running Raspberry Pi OS Lite 64-bit and AquaOS Core;
- Ethernet/PoE ESP32 sensor node with two independently identified waterproof
  DS18B20 probes;
- Shelly Plug US Gen4 outputs using direct local control;
- lamp or safe resistive bench load, never a live heater or livestock system;
- independent physical high-temperature protection for each future heater
  channel.

No hardware behavior is implemented in Prompt 1. Exact wiring, firmware,
protocol, cutoff choice/setpoint, and Atlas Scientific chemistry integration
require later bench evidence and ADRs. Do not infer them from legacy files.
