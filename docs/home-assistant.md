# Home Assistant integration

Enable `mqtt.home_assistant.enabled` only after MQTT is configured. Discovery documents are retained at `homeassistant/<component>/<object-id>/config`, use stable UUID-derived unique IDs, group entities by AquaOS device identity, and reference AquaOS availability and versioned state topics.

The broker ACL must allow Core to write only
`homeassistant/+/+/config`, allow Home Assistant to read
`homeassistant/#`, and allow its birth message on `homeassistant/status`.
The repository examples include these grants without granting Home Assistant
broad discovery or desired-state writes.

Home Assistant switch commands publish only `ON` or `OFF` to the generated narrow command topic. AquaOS rejects retained, malformed, unknown-equipment, and out-of-namespace commands. Accepted requests are converted into expiring output-service commands, so authorization and safety policy remain in AquaOS Core.

Home Assistant documents that discovery entities require a stable unique ID for device grouping and that availability topics define online/offline state. AquaOS follows those contracts: <https://www.home-assistant.io/integrations/mqtt/>.

## Removal and rename policy

- Names are display data. Renaming does not change a UUID-derived unique ID.
- An entity removed during a running refresh receives an empty retained discovery payload.
- For removal across restart, add its former `component` and `object_id` to `mqtt.home_assistant.tombstones`. Keep the tombstone until a successful broker-connected reconciliation has occurred.
- Never reuse an old unique ID for a different physical or logical entity.

MQTT or Home Assistant outages degrade integration health only. They do not stop direct-LAN adapters, canonical state, safety evaluation, output reconciliation, alarms, or the REST API.
