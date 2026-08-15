# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Validated final batch passed to a model engine."""

from __future__ import annotations

from dataclasses import dataclass

from serving.model_worker.protocol import ModelRequest

from .compatibility import TensorCompatibilityKey


@dataclass(frozen=True, slots=True)
class TensorBatch:
    key: TensorCompatibilityKey
    requests: tuple[ModelRequest, ...]
    total_input_units: int
    total_output_units: int
