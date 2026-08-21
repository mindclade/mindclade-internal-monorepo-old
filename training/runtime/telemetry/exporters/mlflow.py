# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Bounded MLflow tracking mirror with Mindclade lineage remaining authoritative."""

from __future__ import annotations

import json
import math
from collections.abc import Mapping
from dataclasses import dataclass
from threading import Lock
from typing import Protocol

MAXIMUM_FIELDS = 256
MAXIMUM_KEY_LENGTH = 250
MAXIMUM_VALUE_LENGTH = 4096
MAXIMUM_DATASETS = 4096


class RunInfo(Protocol):
    run_id: str


class Run(Protocol):
    info: RunInfo


class TrackingClient(Protocol):
    """The stable subset implemented by ``mlflow.MlflowClient``."""

    def create_run(
        self,
        experiment_id: str,
        *,
        tags: Mapping[str, str] | None = None,
        run_name: str | None = None,
    ) -> Run: ...

    def log_param(
        self, run_id: str, key: str, value: str, *, synchronous: bool = True
    ) -> object: ...

    def log_metric(
        self,
        run_id: str,
        key: str,
        value: float,
        *,
        timestamp: int,
        step: int,
        synchronous: bool = True,
    ) -> object: ...

    def set_tag(self, run_id: str, key: str, value: str, *, synchronous: bool = True) -> object: ...

    def log_text(self, run_id: str, text: str, artifact_file: str) -> None: ...

    def set_terminated(self, run_id: str, *, status: str) -> None: ...


@dataclass(frozen=True, slots=True)
class DatasetReference:
    name: str
    digest: str
    role: str

    def validate(self) -> None:
        _text(self.name, name="dataset name", maximum=256)
        _text(self.role, name="dataset role", maximum=128)
        _digest(self.digest, name="dataset digest")


@dataclass(frozen=True, slots=True)
class RunLineage:
    mindclade_run_id: str
    resolved_config_digest: str
    model_digest: str | None = None
    datasets: tuple[DatasetReference, ...] = ()
    artifacts: tuple[str, ...] = ()

    def validate(self) -> None:
        _text(self.mindclade_run_id, name="Mindclade run id", maximum=256)
        _digest(self.resolved_config_digest, name="resolved config digest")
        if self.model_digest is not None:
            _digest(self.model_digest, name="model digest")
        if len(self.datasets) > MAXIMUM_DATASETS or len(self.artifacts) > MAXIMUM_DATASETS:
            raise ValueError("MLflow lineage reference count exceeds bounds")
        dataset_identities = {(item.name, item.role) for item in self.datasets}
        if len(dataset_identities) != len(self.datasets):
            raise ValueError("MLflow dataset lineage identities must be unique")
        if len(set(self.artifacts)) != len(self.artifacts):
            raise ValueError("MLflow artifact lineage digests must be unique")
        for dataset in self.datasets:
            dataset.validate()
        for artifact in self.artifacts:
            _digest(artifact, name="artifact digest")

    def canonical_document(self) -> str:
        self.validate()
        value = {
            "schema_version": 1,
            "mindclade_run_id": self.mindclade_run_id,
            "resolved_config_digest": self.resolved_config_digest,
            "model_digest": self.model_digest,
            "datasets": [
                {"name": item.name, "digest": item.digest, "role": item.role}
                for item in self.datasets
            ],
            "artifacts": list(self.artifacts),
            "authority": "mindclade-cas",
        }
        return json.dumps(value, sort_keys=True, separators=(",", ":")) + "\n"


class MLflowExporter:
    """Serializes low-level client calls and contains optional-backend failures."""

    def __init__(
        self, client: TrackingClient, experiment_id: str, *, required: bool = False
    ) -> None:
        if not callable(getattr(client, "create_run", None)):
            raise ValueError("MLflow tracking client is invalid")
        if not isinstance(required, bool):
            raise ValueError("MLflow required mode must be boolean")
        _text(experiment_id, name="MLflow experiment id", maximum=256)
        self._client = client
        self._experiment_id = experiment_id
        self._required = required
        self._run_id: str | None = None
        self._failures = 0
        self._lock = Lock()

    @property
    def run_id(self) -> str | None:
        with self._lock:
            return self._run_id

    @property
    def failures(self) -> int:
        with self._lock:
            return self._failures

    def start(self, lineage: RunLineage, *, run_name: str) -> bool:
        lineage.validate()
        _text(run_name, name="MLflow run name", maximum=256)
        tags = {
            "mindclade.run_id": lineage.mindclade_run_id,
            "mindclade.config_digest": lineage.resolved_config_digest,
            "mindclade.authority": "mirror",
        }
        if lineage.model_digest is not None:
            tags["mindclade.model_digest"] = lineage.model_digest

        def operation() -> None:
            if self._run_id is not None:
                raise RuntimeError("MLflow run is already active")
            run = self._client.create_run(self._experiment_id, tags=tags, run_name=run_name)
            run_id = run.info.run_id
            _text(run_id, name="MLflow run id", maximum=256)
            self._run_id = run_id
            self._client.log_text(run_id, lineage.canonical_document(), "mindclade/lineage.json")

        return self._call(operation)

    def log_parameters(self, values: Mapping[str, object]) -> bool:
        normalized = _fields(values, numeric=False)

        def operation() -> None:
            run_id = self._active_run_id()
            for key, value in normalized.items():
                self._client.log_param(run_id, key, value, synchronous=True)

        return self._call(operation)

    def log_metrics(self, values: Mapping[str, float], *, step: int, timestamp_millis: int) -> bool:
        if isinstance(step, bool) or not 0 <= step < 2**63:
            raise ValueError("MLflow metric step is outside bounds")
        if isinstance(timestamp_millis, bool) or not 0 <= timestamp_millis < 2**63:
            raise ValueError("MLflow metric timestamp is outside bounds")
        normalized = _fields(values, numeric=True)

        def operation() -> None:
            run_id = self._active_run_id()
            for key, value in normalized.items():
                self._client.log_metric(
                    run_id,
                    key,
                    float(value),
                    timestamp=timestamp_millis,
                    step=step,
                    synchronous=True,
                )

        return self._call(operation)

    def set_tags(self, values: Mapping[str, object]) -> bool:
        normalized = _fields(values, numeric=False)

        def operation() -> None:
            run_id = self._active_run_id()
            for key, value in normalized.items():
                self._client.set_tag(run_id, key, value, synchronous=True)

        return self._call(operation)

    def finish(self, *, status: str = "FINISHED") -> bool:
        if status not in {"FINISHED", "FAILED", "KILLED"}:
            raise ValueError("MLflow terminal status is invalid")

        def operation() -> None:
            run_id = self._active_run_id()
            self._client.set_terminated(run_id, status=status)
            self._run_id = None

        return self._call(operation)

    def _active_run_id(self) -> str:
        if self._run_id is None:
            raise RuntimeError("MLflow run is not active")
        return self._run_id

    def _call(self, operation) -> bool:
        with self._lock:
            try:
                operation()
                return True
            except Exception:
                self._failures += 1
                if self._required:
                    raise
                return False


def _fields(values: Mapping[str, object], *, numeric: bool) -> dict[str, str | float]:
    if not isinstance(values, Mapping) or len(values) > MAXIMUM_FIELDS:
        raise ValueError("MLflow field mapping is outside bounds")
    normalized: dict[str, str | float] = {}
    for key, value in values.items():
        _text(key, name="MLflow field key", maximum=MAXIMUM_KEY_LENGTH)
        if numeric:
            if (
                isinstance(value, bool)
                or not isinstance(value, int | float)
                or not math.isfinite(value)
            ):
                raise ValueError("MLflow metric values must be finite numbers")
            normalized[key] = float(value)
        else:
            rendered = str(value)
            _text(rendered, name="MLflow field value", maximum=MAXIMUM_VALUE_LENGTH)
            normalized[key] = rendered
    return normalized


def _text(value: object, *, name: str, maximum: int) -> str:
    if not isinstance(value, str) or not value or len(value) > maximum:
        raise ValueError(f"{name} is invalid")
    if any(ord(character) < 0x20 for character in value):
        raise ValueError(f"{name} contains control characters")
    return value


def _digest(value: object, *, name: str) -> str:
    if (
        not isinstance(value, str)
        or len(value) != 71
        or not value.startswith("sha256:")
        or any(character not in "0123456789abcdef" for character in value[7:])
    ):
        raise ValueError(f"{name} is invalid")
    return value
