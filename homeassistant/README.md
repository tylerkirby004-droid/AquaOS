# Home Assistant

The dedicated-appliance installer deploys Home Assistant Container with the
checked-in dashboard and local alarm automation under `homeassistant/appliance`.
The standard profile uses Home Assistant's built-in history graphs; the
`--advanced-history` profile also provisions InfluxDB and Grafana.

Home Assistant may issue convenience commands only through the guarded MQTT command contract; it is not the safety controller.
