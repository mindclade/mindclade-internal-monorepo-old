# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Stable Python contracts for model-worker inference semantics."""

from .batch import BatchPlan, BatchPlanner, CompatibilityKey
from .model_bundle import ModelBundle
from .model_descriptor import (
    CompatibilityClass,
    ModelDescriptor,
    ResourceEnvelope,
    validate_batch_against_descriptor,
    validate_bundle_against_descriptor,
)
from .request import InferenceRequest, InputDescriptor
from .response import InferenceResult
from .runtime_manifest import RuntimeManifest
from .validation import validate_batch_against_bundle

__all__ = [
    "BatchPlan",
    "BatchPlanner",
    "CompatibilityClass",
    "CompatibilityKey",
    "InferenceRequest",
    "InferenceResult",
    "InputDescriptor",
    "ModelBundle",
    "ModelDescriptor",
    "ResourceEnvelope",
    "RuntimeManifest",
    "validate_batch_against_bundle",
    "validate_batch_against_descriptor",
    "validate_bundle_against_descriptor",
]
