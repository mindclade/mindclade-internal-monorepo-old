# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Numerical tests for implemented reference tasks."""

import pytest
import torch
from torch import nn

from training.contracts import SupervisedBatch
from training.tasks import SupervisedMSETask


def test_mse_task_returns_sum_and_scalar_element_denominator() -> None:
    model = nn.Linear(1, 1, bias=False)
    with torch.no_grad():
        model.weight.fill_(2.0)
    batch = SupervisedBatch(
        torch.tensor([[1.0], [3.0]]),
        torch.tensor([[1.0], [5.0]]),
    )

    result = SupervisedMSETask().compute(model, batch)

    torch.testing.assert_close(result.loss_sum, torch.tensor(2.0), rtol=0.0, atol=0.0)
    assert result.denominator == 2
    (result.loss_sum / result.denominator).backward()
    assert model.weight.grad is not None
    assert torch.isfinite(model.weight.grad).all()


def test_mse_task_supports_batch_size_one() -> None:
    model = nn.Linear(2, 1)
    batch = SupervisedBatch(torch.tensor([[1.0, 2.0]]), torch.tensor([[3.0]]))

    result = SupervisedMSETask().compute(model, batch)

    assert result.loss_sum.shape == ()
    assert result.denominator == 1


def test_mse_task_rejects_output_shape_broadcasting() -> None:
    model = nn.Linear(2, 2)
    batch = SupervisedBatch(torch.ones(1, 2), torch.ones(1, 1))

    with pytest.raises(ValueError, match="shape"):
        SupervisedMSETask().compute(model, batch)


def test_mse_task_rejects_nonfinite_model_output() -> None:
    class Nonfinite(nn.Module):
        def forward(self, inputs: torch.Tensor) -> torch.Tensor:
            return inputs * torch.tensor(float("inf"))

    batch = SupervisedBatch(torch.ones(1, 1), torch.ones(1, 1))

    with pytest.raises(FloatingPointError, match="output"):
        SupervisedMSETask().compute(Nonfinite(), batch)
