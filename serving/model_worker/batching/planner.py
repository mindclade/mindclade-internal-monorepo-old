# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Bounded final tensor-aware batch planner."""

from __future__ import annotations

from collections import defaultdict

from serving.model_worker.config import WorkerLimits
from serving.model_worker.protocol import ModelRequest

from .compatibility import TensorCompatibilityKey, compatibility_key
from .tensor_batch import TensorBatch


class BatchPlanner:
    def __init__(self, limits: WorkerLimits) -> None:
        limits.validate()
        self._limits = limits

    def plan(self, requests: tuple[ModelRequest, ...]) -> tuple[TensorBatch, ...]:
        if len(requests) > self._limits.maximum_active_requests:
            raise ValueError("request set exceeds worker active-request bound")
        groups: dict[TensorCompatibilityKey, list[ModelRequest]] = defaultdict(list)
        for request in requests:
            request.validate(maximum_request_id_bytes=self._limits.maximum_request_id_bytes)
            groups[compatibility_key(request)].append(request)

        output: list[TensorBatch] = []
        for key in sorted(groups):
            pending = groups[key]
            while pending:
                batch: list[ModelRequest] = []
                input_units = 0
                output_units = 0
                while pending and len(batch) < self._limits.maximum_batch_size:
                    candidate = pending[0]
                    next_input = input_units + candidate.input_units
                    next_output = output_units + candidate.output_units
                    if next_input > self._limits.maximum_input_units_per_batch:
                        if not batch:
                            raise ValueError("single request exceeds batch input-unit limit")
                        break
                    if next_output > self._limits.maximum_output_units_per_batch:
                        if not batch:
                            raise ValueError("single request exceeds batch output-unit limit")
                        break
                    pending.pop(0)
                    batch.append(candidate)
                    input_units = next_input
                    output_units = next_output
                output.append(TensorBatch(key, tuple(batch), input_units, output_units))
        return tuple(output)
