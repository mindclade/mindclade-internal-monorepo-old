# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

import json
from types import SimpleNamespace

import pytest

from training.runtime.telemetry.exporters.mlflow import DatasetReference, MLflowExporter, RunLineage

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
        model_digest="sha256:" + "b" * 64,
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


@pytest.mark.parametrize("metric", [float("nan"), float("inf"), True])
def test_metrics_must_be_finite(metric: float) -> None:
    exporter = MLflowExporter(Client(), "experiment-1")
    with pytest.raises(ValueError, match="finite"):
        exporter.log_metrics({"loss": metric}, step=0, timestamp_millis=1)
