# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Behavioral coverage for the local training contracts."""

from dataclasses import FrozenInstanceError

import pytest
import torch

from training.contracts import StepResult, SupervisedBatch, TaskResult, TrainingState


def test_supervised_batch_accepts_batch_size_one_and_noncontiguous_tensors() -> None:
    inputs = torch.arange(6, dtype=torch.float32).reshape(2, 3).transpose(0, 1)
    targets = torch.zeros((3, 2), dtype=torch.float32)

    batch = SupervisedBatch(inputs, targets)

    assert batch.batch_size == 3
    assert batch.target_elements == 6
    assert not batch.inputs.is_contiguous()
    with pytest.raises(FrozenInstanceError):
        batch.inputs = torch.zeros_like(inputs)  # type: ignore[misc]


@pytest.mark.parametrize(
    ("inputs", "targets", "message"),
    [
        (torch.ones(1, dtype=torch.float64), torch.ones(1), "CPU or CUDA float32"),
        (torch.ones(2, 1), torch.ones(1, 1), "batch dimension"),
        (torch.tensor([float("nan")]), torch.ones(1), "finite"),
        (torch.ones(1), torch.empty(1, 0), "contain values"),
        (torch.tensor(1.0), torch.ones(1), "rank"),
    ],
)
def test_supervised_batch_rejects_invalid_tensor_contracts(
    inputs: torch.Tensor, targets: torch.Tensor, message: str
) -> None:
    with pytest.raises(ValueError, match=message):
        SupervisedBatch(inputs, targets)


def test_task_result_preserves_differentiable_sum_and_denominator() -> None:
    value = torch.tensor(2.0, requires_grad=True)
    result = TaskResult(value.square(), 4)

    (result.loss_sum / result.denominator).backward()

    torch.testing.assert_close(value.grad, torch.tensor(1.0), rtol=0.0, atol=0.0)


@pytest.mark.parametrize(
    ("loss", "denominator", "message"),
    [
        (torch.ones(1), 1, "scalar"),
        (torch.tensor(1.0, dtype=torch.float64), 1, "CPU or CUDA float32"),
        (torch.tensor(float("inf")), 1, "not finite"),
        (torch.tensor(1.0), 0, "denominator"),
    ],
)
def test_task_result_rejects_invalid_reductions(
    loss: torch.Tensor, denominator: int, message: str
) -> None:
    with pytest.raises((ValueError, FloatingPointError), match=message):
        TaskResult(loss, denominator)


def test_step_result_copies_detached_finite_metrics() -> None:
    source = {"loss": 0.25}
    state = TrainingState(microbatches=1, optimizer_steps=1, samples=1)
    result = StepResult(state, 1.0, 4, 1, 1, True, source)
    source["loss"] = 99.0

    assert result.mean_loss == 0.25
    assert result.metrics == {"loss": 0.25}
    assert isinstance(result.metrics["loss"], float)
    with pytest.raises(TypeError):
        result.metrics["other"] = 1.0  # type: ignore[index]


@pytest.mark.parametrize("value", [float("nan"), float("inf"), True])
def test_step_result_rejects_invalid_metrics(value: float) -> None:
    state = TrainingState(microbatches=1, optimizer_steps=1, samples=1)
    with pytest.raises(ValueError, match="finite"):
        StepResult(state, 1.0, 1, 1, 1, True, {"loss": value})
