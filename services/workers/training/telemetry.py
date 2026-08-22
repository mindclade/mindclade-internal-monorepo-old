# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Optional MLflow projection for reference training.

The exporter is a mirror only. Every call is contained here so an unavailable mirror cannot
change authoritative model, checkpoint, bundle, or run-evidence publication.
"""

from __future__ import annotations

from collections.abc import Mapping
from dataclasses import dataclass
from typing import Literal

from libs.python.identifiers import ArtifactRef
from training.runtime.telemetry.exporters import DatasetReference, MLflowExporter, RunLineage


@dataclass(frozen=True, slots=True)
class MirrorIdentity:
    run_id: str
    resolved_config_digest: str
    source_revision: str
    runtime_image_digest: str
    attempt: int
    model_digest: str
    dataset: ArtifactRef
    resume_checkpoint: ArtifactRef | None
    classification: str


class OptionalMLflowMirror:
    """Best-effort adapter over the bounded MLflow exporter."""

    def __init__(self, exporter: MLflowExporter | None) -> None:
        if exporter is not None and not isinstance(exporter, MLflowExporter):
            raise TypeError("exporter must be an MLflowExporter or None")
        self._exporter = exporter
        self._active = False
        self._failures = 0

    @property
    def failures(self) -> int:
        return self._failures

    def start(self, identity: MirrorIdentity, parameters: Mapping[str, object]) -> None:
        if self._exporter is None:
            return
        resume_digest = (
            identity.resume_checkpoint.digest.text
            if identity.resume_checkpoint is not None
            else None
        )
        lineage = RunLineage(
            mindclade_run_id=identity.run_id,
            resolved_config_digest=identity.resolved_config_digest,
            source_revision=identity.source_revision,
            runtime_image_digest=identity.runtime_image_digest,
            attempt=identity.attempt,
            model_digest=identity.model_digest,
            resume_checkpoint_digest=resume_digest,
            classification=identity.classification,
            datasets=(
                DatasetReference(
                    "reference-affine-training",
                    identity.dataset.digest.text,
                    "training",
                ),
            ),
        )
        try:
            started = self._exporter.start(
                lineage,
                run_name=f"reference-affine-{identity.run_id}",
            )
            self._active = bool(started)
            if not started:
                self._failures += 1
            if self._active and not self._exporter.log_parameters(parameters):
                self._failures += 1
        except Exception:
            # The composition root rejects required exporters. CAS/checkpoint publication remains
            # authoritative even if the optional mirror implementation itself raises.
            self._active = False
            self._failures += 1

    def log_step(self, metrics: Mapping[str, float], *, step: int, timestamp_millis: int) -> None:
        if not self._active or self._exporter is None:
            return
        try:
            if not self._exporter.log_metrics(
                metrics,
                step=step,
                timestamp_millis=timestamp_millis,
            ):
                self._failures += 1
        except Exception:
            self._failures += 1

    def finish(self, *, status: Literal["FINISHED", "FAILED", "KILLED"]) -> None:
        """Best-effort termination with the caller's authoritative fault classification."""

        if not self._active or self._exporter is None:
            return
        try:
            if not self._exporter.finish(status=status):
                self._failures += 1
        except Exception:
            self._failures += 1
        finally:
            self._active = False


__all__ = ["MirrorIdentity", "OptionalMLflowMirror"]
