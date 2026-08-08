# Install AquaOS in Home Assistant

## What you need

- A dedicated Intel/AMD computer running Home Assistant OS
- A wired network connection and Internet access during installation
- No aquarium equipment connected to AquaOS during setup

## Installation

1. Open [Add the AquaOS repository](https://my.home-assistant.io/redirect/supervisor_add_addon_repository/?repository_url=https%3A%2F%2Fgithub.com%2Ftylerkirby004-droid%2FAquaOS) while signed in to Home Assistant.
2. Confirm the repository, select **AquaOS**, and choose **Install**.
3. Turn on **Start on boot**, **Watchdog**, and **Show in sidebar**.
4. Choose **Start**, then open **AquaOS** in the sidebar.

There is no AquaOS pairing code and no AquaOS IP address to enter. Home
Assistant authenticates the panel. The initial configuration uses the
hardware-incapable simulator and does not activate equipment.

## One place for daily use

Use the AquaOS sidebar for health, sensors, equipment, alarms, device mapping,
calibration, commissioning, configuration, diagnostics, and AquaOS backups.
Home Assistant continues to provide its mobile app, user accounts, and native
device discovery. AquaOS-controlled outputs must always be mapped through the
AquaOS panel so its safety policy remains in the command path.

Home Assistant's recorder supplies normal graphs. InfluxDB and Grafana are
optional advanced-history services; they are not required for control.

## Safety and current limitations

This app is experimental and is not approved for live aquarium control. The
Home Assistant OS computer is one physical failure domain. Use independent
heater thermostats, mechanical/independent ATO limits, dosing limits, suitable
GFCI/RCD and circuit protection, leak mitigation, a UPS where appropriate, and
tested Home Assistant backups.

Before a production release, AquaOS still requires real Shelly and ESP32 bench
validation, backup/restore and upgrade/rollback tests, failure isolation tests,
physical safety tests, and a 72-hour soak.
