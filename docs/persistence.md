# Historical persistence and visualization

InfluxDB and Grafana are optional. Their failure affects historical visibility, not sensing, safety, commands, reconciliation, alarms, REST, or direct-LAN control.

Enable `storage.influxdb.enabled` only after setting the URL, organization, bucket, and an external `token_file`. Queue capacity, batch size, flush interval, retry bounds, and write timeout are finite configuration values. Overflow uses a drop-newest policy exposed by storage metrics.

## Schema v1

- `aquaos_measurements_v1`: validated quantity observations
- `aquaos_equipment_state_v1`: desired and reported equipment transitions
- `aquaos_command_outcomes_v1`: command lifecycle outcomes
- `aquaos_alarms_v1`: alarm lifecycle transitions

Stable bounded tags include entity identity, quantity, canonical unit, event type, status, reason code, severity, source component, and subject kind. High-cardinality causal IDs and free text are fields. Never add credentials, URLs containing credentials, raw arbitrary metadata, error text, or correlation IDs as tags.

Example Grafana provisioning is under
`infrastructure/docker/grafana/provisioning`. It expects the separate services
stack's InfluxDB datasource configured through environment variables. These
assets are not installed in the Control VM critical path.
