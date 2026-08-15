from __future__ import annotations

import json
from pathlib import Path

ROOT = Path(__file__).resolve().parents[3]


def test_event_envelope_is_versioned_bounded_and_replayable():
    schema = json.loads(
        (ROOT / "protocols/events/generated/runtime/v1/event-envelope.schema.json").read_text()
    )
    required = set(schema["required"])
    assert {
        "event_id",
        "schema",
        "aggregate_id",
        "aggregate_version",
        "ordering_key",
        "occurred_at_unix_millis",
        "payload",
        "payload_digest",
    } <= required
    assert schema["additionalProperties"] is False
    proto = (ROOT / "protocols/proto/mindclade/events/v1/envelope.proto").read_text()
    assert "message EventEnvelope" in proto and "payload_digest" in proto
