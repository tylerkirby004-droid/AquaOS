"""MQTT observation transport with externally supplied, least-privilege topics."""

from queue import Empty, Queue

import paho.mqtt.client as mqtt


class MQTTTransport:
    """Consumes assets and publishes observations; it exposes no command API."""

    def __init__(self, client: mqtt.Client, asset_topic: str,
                 observation_topic: str, maximum_asset_bytes: int = 4_194_304):
        if not asset_topic or not observation_topic or maximum_asset_bytes < 1:
            raise ValueError("topics and a positive asset limit are required")
        self._client = client
        self._asset_topic = asset_topic
        self._observation_topic = observation_topic
        self._maximum_asset_bytes = maximum_asset_bytes
        self._assets: Queue[bytes] = Queue(maxsize=2)
        client.on_message = self._on_message

    def subscribe(self) -> None:
        """Subscribe only to the configured camera/event input."""
        self._client.subscribe(self._asset_topic, qos=1)

    def _on_message(self, _client, _userdata, message) -> None:
        if len(message.payload) > self._maximum_asset_bytes or self._assets.full():
            return
        self._assets.put_nowait(bytes(message.payload))

    def receive(self, timeout_seconds: float) -> bytes | None:
        """Receive one bounded asset or return when polling times out."""
        try:
            return self._assets.get(timeout=timeout_seconds)
        except Empty:
            return None

    def publish_observation(self, payload: bytes) -> None:
        """Publish only to the configured versioned observation topic."""
        result = self._client.publish(self._observation_topic, payload, qos=1)
        if result.rc != mqtt.MQTT_ERR_SUCCESS:
            raise RuntimeError("observation publish failed")
