"""Strict version-one observation contracts."""

from dataclasses import asdict, dataclass
from datetime import datetime, timezone
import json
from uuid import UUID


@dataclass(frozen=True)
class Observation:
    """An expiring, advisory AI observation with traceable provenance."""

    sourceAssetId: str
    serviceVersion: str
    modelVersion: str
    confidence: float
    occurredAt: str
    expiresAt: str
    correlationId: str
    kind: str
    recommendation: str

    def validate(self, now: datetime | None = None) -> None:
        """Reject malformed, stale, low-confidence, or untraceable output."""
        UUID(self.sourceAssetId)
        UUID(self.correlationId)
        occurred = datetime.fromisoformat(self.occurredAt.replace("Z", "+00:00"))
        expires = datetime.fromisoformat(self.expiresAt.replace("Z", "+00:00"))
        current = now or datetime.now(timezone.utc)
        if not self.serviceVersion or not self.modelVersion or not self.kind:
            raise ValueError("versions and kind are required")
        if self.confidence < 0.8 or self.confidence > 1:
            raise ValueError("confidence is outside the accepted range")
        if occurred > current or expires <= current or expires <= occurred:
            raise ValueError("observation time window is invalid")

    def encode(self) -> bytes:
        """Encode a validated observation as bounded JSON."""
        self.validate()
        payload = json.dumps(asdict(self), separators=(",", ":")).encode()
        if len(payload) > 16_384:
            raise ValueError("observation exceeds 16 KiB")
        return payload
