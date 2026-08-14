"""Stable Python contracts for model-worker inference semantics."""

from .batch import BatchPlan, BatchPlanner, CompatibilityKey
from .model_bundle import ModelBundle
from .request import InferenceRequest, InputDescriptor
from .response import InferenceResult
from .runtime_manifest import RuntimeManifest
from .validation import validate_batch_against_bundle

__all__ = [
    "BatchPlan",
    "BatchPlanner",
    "CompatibilityKey",
    "InferenceRequest",
    "InferenceResult",
    "InputDescriptor",
    "ModelBundle",
    "RuntimeManifest",
    "validate_batch_against_bundle",
]
