# Third-party notices

AquaOS depends on `github.com/eclipse/paho.mqtt.golang` v1.5.1 for its optional
MQTT integration. Paho is made available under the Eclipse Public License 2.0
and Eclipse Distribution License 1.0. Its source and complete license text are
available from the [Eclipse Paho Go repository](https://github.com/eclipse-paho/paho.mqtt.golang/tree/v1.5.1).

The MQTT integration is optional and outside the critical local-control path.
The CI license allowlist names EPL-2.0 explicitly so this reviewed dependency
cannot be confused with a blanket acceptance of unknown licenses.
