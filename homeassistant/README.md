# Home Assistant

For the dedicated-appliance profile, the installer deploys Home Assistant
Container with the checked-in dashboard and local alarm automation under
`homeassistant/appliance`. For the advanced Proxmox profile, deploy Home
Assistant OS as its own VM and import the same generated dashboard.

Home Assistant may issue convenience commands only through the guarded MQTT command contract; it is not the safety controller.
