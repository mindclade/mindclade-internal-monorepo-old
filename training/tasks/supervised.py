# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Reference supervised objectives."""

from __future__ import annotations

import torch
from torch import nn
from torch.nn import functional as F

from libs.python.errors import FailedPrecondition, InvalidArgument
from training.contracts import SupervisedBatch, TaskResult


class SupervisedMSETask:
    """Mean-squared-error semantics expressed as a sum and exact denominator."""

    def compute(self, model: nn.Module, batch: SupervisedBatch) -> TaskResult:
        if not isinstance(model, nn.Module):
            raise InvalidArgument(
                "supervised task model must be an nn.Module",
                reason="training_task_model",
            )
        if not isinstance(batch, SupervisedBatch):
            raise InvalidArgument(
                "supervised task batch must be SupervisedBatch",
                reason="training_task_batch",
            )
        prediction = model(batch.inputs)
        if not isinstance(prediction, torch.Tensor):
            raise FailedPrecondition(
                "supervised model must return a tensor",
                reason="training_task_output",
            )
        if prediction.device.type != "cpu" or prediction.dtype is not torch.float32:
            raise FailedPrecondition(
                "supervised model output must be CPU float32",
                reason="training_task_output_placement",
            )
        if prediction.shape != batch.targets.shape:
            raise FailedPrecondition(
                "supervised model output shape must equal target shape",
                reason="training_task_output_shape",
                fields={
                    "prediction": str(tuple(prediction.shape)),
                    "target": str(tuple(batch.targets.shape)),
                },
            )
        if not bool(torch.isfinite(prediction.detach()).all().item()):
            raise FloatingPointError("supervised model output is not finite")
        return TaskResult(
            F.mse_loss(prediction, batch.targets, reduction="sum"),
            batch.target_elements,
        )
