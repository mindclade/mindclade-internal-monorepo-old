"""Cross-contract validation helpers."""

from __future__ import annotations

from .batch import BatchPlan
from .model_bundle import ModelBundle


def validate_batch_against_bundle(batch: BatchPlan, bundle: ModelBundle) -> None:
    batch.validate()
    bundle.validate()
    if batch.compatibility.model_bundle_digest != bundle.model_digest:
        raise ValueError("batch references a different model bundle")
