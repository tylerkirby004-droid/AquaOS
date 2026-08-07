from datetime import datetime, timedelta, timezone
from uuid import uuid4

import pytest

from aquaos_vision.contracts import Observation


def observation(**changes):
    now = datetime.now(timezone.utc)
    values = dict(sourceAssetId=str(uuid4()), serviceVersion="0.1.0",
                  modelVersion="none", confidence=.9,
                  occurredAt=(now-timedelta(seconds=1)).isoformat(),
                  expiresAt=(now+timedelta(minutes=1)).isoformat(),
                  correlationId=str(uuid4()), kind="leak-possible",
                  recommendation="operator review")
    values.update(changes)
    return Observation(**values)


def test_valid_observation_encodes():
    assert len(observation().encode()) < 16_384


@pytest.mark.parametrize("confidence", [0, .79, 1.01])
def test_low_or_invalid_confidence_is_rejected(confidence):
    with pytest.raises(ValueError):
        observation(confidence=confidence).validate()


def test_stale_observation_is_rejected():
    past = (datetime.now(timezone.utc)-timedelta(minutes=1)).isoformat()
    with pytest.raises(ValueError):
        observation(expiresAt=past).validate()
