# ADR-025: Use Home Assistant OS as the operator appliance

## Status

Accepted; supersedes the deployment and UI decisions in ADR-011, ADR-012, and
ADR-022 through ADR-024. Their implementation and history remain intact as the
legacy dedicated-appliance profile.

## Context

The dedicated Debian image repeatedly exposed new operators to networking,
Linux accounts, service repair, IP addresses, certificates, and pairing codes.
Those tasks did not make aquarium operation safer. Operators also had to move
between Home Assistant and a separate AquaOS Admin site.

Home Assistant already provides the daily application shell, users, mobile
access, device integrations, dashboards, automations, and backups. AquaOS still
needs an independent typed safety and policy boundary because general-purpose
automations must not directly bypass commissioning, interlocks, run-time limits,
or fail-safe behavior.

## Decision

The primary installation is a Home Assistant OS app installed from the AquaOS
app repository. The app runs AquaOS Core and its web application and exposes a
single **AquaOS** sidebar panel through Home Assistant Ingress. Ingress supplies
browser authentication. The app publishes no Admin listener to the LAN and does
not use a second pairing code.

All aquarium mutations continue through AquaOS application services and safety
policy. The browser never talks directly to Shelly or ESP32 outputs. AquaOS may
use Home Assistant's discovered inventory and notification capabilities, but
Home Assistant entities and automations are not allowed to bypass the command
pipeline. Core starts simulator-only and hardware remains uncommissioned.

Core's local control loop must not require MQTT, InfluxDB, Grafana, Internet
access, or the Home Assistant frontend process once the app is running. The
Home Assistant OS host is nevertheless a shared physical and Supervisor failure
domain. Independent physical safeguards and tested backup/recovery remain
mandatory.

The legacy Debian/systemd profile remains in Git for recovery and engineering
evaluation but is no longer the recommended installation. Proxmox and custom
ISO installation are not part of the normal user path.

## Consequences

Operators get one account, one sidebar, one backup system, and one installation
surface. AquaOS accepts the Home Assistant OS/Supervisor host failure domain.
The initial app is amd64 and experimental. It must pass Home Assistant app
build, Ingress, backup/restore, upgrade/rollback, hardware bench, failure
injection, and 72-hour soak gates before live-livestock use.
