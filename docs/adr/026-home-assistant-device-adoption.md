# ADR-026: Adopt Home Assistant devices without separate commissioning

## Status

Accepted

## Context

ADR-025 made the Home Assistant OS app the primary operator installation and the
single AquaOS sidebar. Operators expect a device selected from Home Assistant's
inventory to become usable after saving AquaOS setup. Many Home Assistant
integrations already expose native fault, safety, moisture, and problem binary
sensors that should seed AquaOS alarm rules instead of forcing duplicate alarm
entry.

## Decision

The AquaOS sidebar no longer presents a separate commissioning workflow for
imported devices. Adding a supported local adapter enables that adapter on save,
while all mutations still pass through AquaOS APIs, output safety policy,
fail-safe configuration, run limits, and sensor interlocks. Home Assistant
registries remain read-only input; Home Assistant services and automations do
not become an authority for equipment commands.

Existing `commissioning` configuration fields remain schema-compatible legacy
metadata, but they are not an activation gate. Home Assistant problem-style
binary sensors may create default AquaOS alarm rules during import.

## Consequences

Setup is shorter: if an operator adds a supported device, AquaOS attempts to use
it after validation and restart. Safety remains in Core rather than in the UI
workflow. Production guidance must keep emphasizing independent safeguards, UPS
planning, automatic app startup, tested backups/restores, and replacement-host
recovery because Home Assistant OS remains a shared failure domain.
