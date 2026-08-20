# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

import math

import pytest

from libs.python.errors import InvalidArgument
from libs.python.observability import (
    REDACTED,
    LogEvent,
    MetricKind,
    MetricPoint,
    SpanEvent,
    TraceContext,
)


def test_log_event_is_redacted_and_immutable() -> None:
    event = LogEvent("worker.started", "ready", fields={"token": "secret", "rank": 1})

    assert event.to_document()["fields"] == {"token": REDACTED, "rank": 1}
    with pytest.raises(TypeError):
        event.fields["rank"] = 2  # type: ignore[index]


def test_metric_contract_rejects_non_finite_and_negative_counter() -> None:
    point = MetricPoint("worker.requests", 3, MetricKind.COUNTER, {"result": "ok"})
    assert point.to_document()["value"] == 3

    with pytest.raises(InvalidArgument):
        MetricPoint("worker.requests", math.inf)
    with pytest.raises(InvalidArgument):
        MetricPoint("worker.requests", -1, MetricKind.COUNTER)
    with pytest.raises(InvalidArgument, match="64 bits"):
        MetricPoint("worker.requests", 1 << 80)


def test_metric_labels_are_redacted_before_export() -> None:
    point = MetricPoint(
        "worker.requests",
        1,
        labels={"token": "secret", "endpoint": "https://user:pass@example.com/x?q=secret"},
    )
    assert point.labels == {"token": REDACTED, "endpoint": "https://example.com/x"}


def test_traceparent_round_trip_and_span_redaction() -> None:
    context = TraceContext.root(sampled=True)
    parsed = TraceContext.from_traceparent(context.traceparent())
    span = SpanEvent(parsed.child(), "engine.execute", {"password": "secret"})

    assert parsed == context
    assert span.context.trace_id == context.trace_id
    assert span.attributes["password"] == REDACTED
