"""Cancellable orchestration with no actuator capability."""

from dataclasses import dataclass
import logging
from threading import Event
from typing import Protocol

from .contracts import Observation


class Transport(Protocol):
    """The service-owned, observation-only transport boundary."""

    def receive(self, timeout_seconds: float) -> bytes | None: ...
    def publish_observation(self, payload: bytes) -> None: ...


class Model(Protocol):
    """A replaceable model boundary that may be unavailable."""

    def analyze(self, asset: bytes) -> Observation | None: ...


@dataclass
class Service:
    """Processes permitted assets until cancellation; failures are isolated."""

    transport: Transport
    model: Model
    logger: logging.Logger

    def run(self, stopped: Event) -> None:
        """Run bounded polling without affecting AquaOS Core."""
        while not stopped.is_set():
            asset = self.transport.receive(0.5)
            if asset is None:
                continue
            try:
                result = self.model.analyze(asset)
                if result is not None:
                    self.transport.publish_observation(result.encode())
            except (ValueError, RuntimeError):
                self.logger.exception("vision analysis quarantined")
