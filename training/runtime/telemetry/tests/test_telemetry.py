# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

import json
from types import SimpleNamespace

import pytest

from training.runtime.telemetry.exporters.mlflow import DatasetReference, MLflowExporter, RunLineage
from training.runtime.telemetry.exporters.mlflow_tracing import (
    MLflowTraceExporter,
    SpanHandle,
    TraceIdentity,
)

DIGEST = "sha256:" + "a" * 64


class Client:
    def __init__(self) -> None:
        self.calls: list[tuple] = []

    def create_run(self, experiment_id, *, tags=None, run_name=None):
        self.calls.append(("create", experiment_id, tags, run_name))
        return SimpleNamespace(info=SimpleNamespace(run_id="mlflow-run"))

    def log_text(self, run_id, text, artifact_file):
        self.calls.append(("text", run_id, text, artifact_file))

    def log_param(self, run_id, key, value, *, synchronous=True):
        self.calls.append(("param", run_id, key, value, synchronous))

    def log_metric(self, run_id, key, value, *, timestamp, step, synchronous=True):
        self.calls.append(("metric", run_id, key, value, timestamp, step, synchronous))

    def set_tag(self, run_id, key, value, *, synchronous=True):
        self.calls.append(("tag", run_id, key, value, synchronous))

    def set_terminated(self, run_id, *, status):
        self.calls.append(("terminated", run_id, status))


def lineage() -> RunLineage:
    return RunLineage(
        "run_019c0000000070008000000000000001",
        DIGEST,
        "1" * 40,
        "sha256:" + "2" * 64,
        2,
        model_digest="sha256:" + "b" * 64,
        resume_checkpoint_digest="sha256:" + "e" * 64,
        datasets=(DatasetReference("train", "sha256:" + "c" * 64, "training"),),
        artifacts=("sha256:" + "d" * 64,),
    )


def test_mlflow_mirror_preserves_cas_authority_and_explicit_run_lifecycle() -> None:
    client = Client()
    exporter = MLflowExporter(client, "experiment-1")
    assert exporter.start(lineage(), run_name="training-run")
    assert exporter.log_parameters({"optimizer": "adamw"})
    assert exporter.log_metrics({"loss": 1.25}, step=4, timestamp_millis=5_000)
    assert exporter.set_tags({"mindclade.release_candidate": "true"})
    assert exporter.finish()
    document = json.loads(next(call[2] for call in client.calls if call[0] == "text"))
    assert document["authority"] == "mindclade-cas"
    assert document["datasets"][0]["digest"].startswith("sha256:")
    assert document["attempt"] == 2
    assert document["runtime_image_digest"].startswith("sha256:")
    assert exporter.run_id is None


def test_optional_mlflow_outage_does_not_fail_authoritative_training() -> None:
    class Failing(Client):
        def create_run(self, experiment_id, *, tags=None, run_name=None):
            raise ConnectionError("unavailable")

    exporter = MLflowExporter(Failing(), "experiment-1", required=False)
    assert not exporter.start(lineage(), run_name="training-run")
    assert exporter.failures == 1


def test_required_mlflow_outage_is_visible() -> None:
    class Failing(Client):
        def create_run(self, experiment_id, *, tags=None, run_name=None):
            raise ConnectionError("unavailable")

    with pytest.raises(ConnectionError):
        MLflowExporter(Failing(), "experiment-1", required=True).start(
            lineage(), run_name="training-run"
        )


def test_failed_lineage_upload_terminates_the_partial_mirror_run() -> None:
    class Failing(Client):
        def log_text(self, run_id, text, artifact_file):
            raise ConnectionError("artifact path unavailable")

    client = Failing()
    exporter = MLflowExporter(client, "experiment-1")
    assert not exporter.start(lineage(), run_name="training-run")
    assert exporter.run_id is None
    assert ("terminated", "mlflow-run", "FAILED") in client.calls


@pytest.mark.parametrize("metric", [float("nan"), float("inf"), True])
def test_metrics_must_be_finite(metric: float) -> None:
    exporter = MLflowExporter(Client(), "experiment-1")
    with pytest.raises(ValueError, match="finite"):
        exporter.log_metrics({"loss": metric}, step=0, timestamp_millis=1)


class TraceClient:
    def __init__(self) -> None:
        self.calls: list[tuple] = []
        self.next_span = 0

    def start_trace(self, name, **kwargs):
        self.calls.append(("start-trace", name, kwargs))
        return SimpleNamespace(trace_id="trace-1", span_id="root-1")

    def start_span(self, name, trace_id, parent_id, **kwargs):
        self.next_span += 1
        self.calls.append(("start-span", name, trace_id, parent_id, kwargs))
        return SimpleNamespace(trace_id=trace_id, span_id=f"span-{self.next_span}")

    def end_span(self, trace_id, span_id, **kwargs):
        self.calls.append(("end-span", trace_id, span_id, kwargs))

    def end_trace(self, trace_id, **kwargs):
        self.calls.append(("end-trace", trace_id, kwargs))


def trace_identity() -> TraceIdentity:
    return TraceIdentity(
        request_id="request_019c0000000070008000000000000001",
        workspace="research-team",
        operation="inference.evaluate",
        source_revision="3" * 40,
        runtime_image_digest="sha256:" + "4" * 64,
        model_digest="sha256:" + "5" * 64,
        evidence_digest="sha256:" + "6" * 64,
        classification="confidential",
    )


def test_trace_mirror_exports_identity_but_never_payloads() -> None:
    client = TraceClient()
    exporter = MLflowTraceExporter(client, "experiment-1")
    trace = exporter.start(
        "inference.request",
        trace_identity(),
        attributes={"mindclade.route": "candidate-a"},
        start_time_ns=10,
    )
    assert trace is not None
    span = exporter.start_span(
        trace,
        "model.invoke",
        span_type="LLM",
        attributes={"mindclade.provider": "internal"},
        start_time_ns=20,
    )
    assert span is not None
    assert exporter.end_span(trace, span, end_time_ns=30)
    assert exporter.end(trace, status="OK", end_time_ns=40)
    start = client.calls[0]
    assert start[2]["inputs"] is None
    assert start[2]["tags"]["mindclade.authority"] == "mirror"
    assert start[2]["tags"]["mindclade.model_digest"].startswith("sha256:")
    assert all(
        call[-1].get("outputs") is None for call in client.calls if call[0].startswith("end-")
    )
    assert exporter.active_trace_count == 0


@pytest.mark.parametrize(
    "attributes",
    [
        {"prompt": "raw"},
        {"mindclade.prompt": "raw"},
        {"mindclade.safe": "x\nunsafe"},
    ],
)
def test_trace_attributes_fail_closed_on_payload_or_unbounded_text(attributes) -> None:
    with pytest.raises(ValueError):
        MLflowTraceExporter(TraceClient(), "experiment-1").start(
            "inference.request", trace_identity(), attributes=attributes
        )


def test_trace_requires_children_to_end_and_rejects_foreign_spans() -> None:
    exporter = MLflowTraceExporter(TraceClient(), "experiment-1")
    trace = exporter.start("inference.request", trace_identity())
    assert trace is not None
    child = exporter.start_span(trace, "model.invoke")
    assert child is not None
    with pytest.raises(RuntimeError, match="active child"):
        exporter.end(trace)
    with pytest.raises(ValueError, match="foreign"):
        exporter.end_span(trace, SpanHandle("trace-other", child.span_id))


def test_optional_trace_failure_is_counted_without_becoming_authority() -> None:
    class Failing(TraceClient):
        def start_trace(self, name, **kwargs):
            raise ConnectionError("unavailable")

    exporter = MLflowTraceExporter(Failing(), "experiment-1")
    assert exporter.start("inference.request", trace_identity()) is None
    assert exporter.failures == 1


def test_required_trace_failure_preserves_the_client_error() -> None:
    class Failing(TraceClient):
        def start_trace(self, name, **kwargs):
            raise ConnectionError("trace mirror unavailable")

    exporter = MLflowTraceExporter(Failing(), "experiment-1", required=True)
    with pytest.raises(ConnectionError, match="trace mirror unavailable"):
        exporter.start("inference.request", trace_identity())
    assert exporter.failures == 1
