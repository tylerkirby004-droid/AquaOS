# AquaOS architecture

## System boundaries

```text
Equipment <-> Reef-Pi + Robo-Tank <-> MQTT <-> AquaOS services
                 |                                  |
          local safety logic               dashboards, history, automation, AI
```

The Raspberry Pi controller owns equipment states, sensor polling, and safety fallbacks. It must have sensible local schedules and thresholds that work when MQTT, networking, or the server is unavailable.

## Server services

| Service | Responsibility | Failure impact |
| --- | --- | --- |
| Mosquitto | Message transport | Telemetry/remote integrations unavailable; controller continues locally |
| InfluxDB | Historical measurements | History temporarily unavailable |
| Grafana | Long-term visualization | Visualization unavailable |
| Node-RED | Non-critical orchestration | No server automations; controller safeguards remain active |
| Home Assistant | UI, notifications, smart-home bridge | UI/notifications unavailable |
| AI VM | Analysis and recommendations | No effect on equipment safety |

## Control policy

Commands travelling from server services to equipment are requests, not safety authority. The controller must validate commands, use explicit allow-lists, enforce safe limits, and publish acknowledgements. AI never directly controls equipment.

## Data flow

1. Reef-Pi samples sensors and controls relays/PWM locally.
2. A bridge publishes normalized retained state and telemetry to MQTT.
3. Node-RED subscribes for integration and writes measurements to InfluxDB.
4. Home Assistant and Grafana consume the same contract for user interfaces.
5. AI consumes stored/read-only data and publishes recommendations under `aquaos/analysis/`.
