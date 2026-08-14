"""Validated final batch passed to a model engine."""
from __future__ import annotations

from dataclasses import dataclass

from .compatibility import TensorCompatibilityKey
from serving.model_worker.protocol import ModelRequest


@dataclass(frozen=True, slots=True)
class TensorBatch:
    key: TensorCompatibilityKey
    requests: tuple[ModelRequest, ...]
    total_input_units: int
    total_output_units: int
