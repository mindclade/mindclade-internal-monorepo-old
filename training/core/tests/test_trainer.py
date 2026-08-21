# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Numerical and lifecycle coverage for the authoritative trainer."""

from collections.abc import Callable

import pytest
import torch
from torch import nn
from torch.nn import functional as F

from libs.python.errors import Canceled
from training.contracts import SupervisedBatch, TaskResult, TrainingState
from training.core import Trainer, TrainerConfig
from training.optim import SGDConfig, build_optimizer
from training.tasks import SupervisedMSETask


def linear_model(*, bias: bool = True) -> nn.Linear:
    model = nn.Linear(1, 1, bias=bias)
    with torch.no_grad():
        model.weight.zero_()
        if model.bias is not None:
            model.bias.zero_()
    return model


def batch(inputs: list[float], targets: list[float]) -> SupervisedBatch:
    return SupervisedBatch(
        torch.tensor(inputs, dtype=torch.float32).reshape(-1, 1),
        torch.tensor(targets, dtype=torch.float32).reshape(-1, 1),
    )


def test_final_partial_accumulation_matches_large_batch_reference() -> None:
    grouped = linear_model()
    reference = linear_model()
    reference.load_state_dict(grouped.state_dict())
    batches = (
        batch([1.0], [2.0]),
        batch([2.0, 3.0], [4.0, 6.0]),
        batch([4.0], [8.0]),
    )
    optimizer = build_optimizer(grouped.parameters(), SGDConfig(learning_rate=0.01))
    trainer = Trainer(
        grouped,
        SupervisedMSETask(),
        optimizer,
        config=TrainerConfig(accumulation_steps=2),
    )

    results = trainer.train(batches)

    reference_optimizer = torch.optim.SGD(reference.parameters(), lr=0.01, foreach=False)
    for group in (batches[:2], batches[2:]):
        reference_optimizer.zero_grad(set_to_none=True)
        inputs = torch.cat([item.inputs for item in group])
        targets = torch.cat([item.targets for item in group])
        F.mse_loss(reference(inputs), targets, reduction="mean").backward()
        reference_optimizer.step()
    for actual, expected in zip(grouped.parameters(), reference.parameters(), strict=True):
        torch.testing.assert_close(actual, expected, rtol=1e-6, atol=1e-7)

    assert len(results) == 2
    assert (results[0].microbatches, results[0].samples, results[0].denominator) == (2, 3, 3)
    assert (results[1].microbatches, results[1].samples, results[1].denominator) == (1, 1, 1)
    assert trainer.state == TrainingState(microbatches=3, optimizer_steps=2, samples=4)
    assert all(parameter.grad is None for parameter in grouped.parameters())


def test_batch_size_one_path_overfits_tiny_affine_problem() -> None:
    model = linear_model()
    sample = batch([-2.0, -1.0, 0.0, 1.0, 2.0], [-3.5, -1.5, 0.5, 2.5, 4.5])
    trainer = Trainer(
        model,
        SupervisedMSETask(),
        build_optimizer(model.parameters(), SGDConfig(learning_rate=0.1)),
    )

    trainer.train((sample,) * 40)
    evaluation = trainer.evaluate((batch([1.0], [2.5]),))

    assert evaluation.mean_loss < 1e-8
    assert trainer.state == TrainingState(microbatches=40, optimizer_steps=40, samples=200)


def test_evaluation_restores_mode_and_does_not_mutate_parameters_gradients_or_state() -> None:
    model = linear_model()
    trainer = Trainer(
        model,
        SupervisedMSETask(),
        build_optimizer(model.parameters(), SGDConfig(learning_rate=0.05)),
    )
    trainer.train((batch([1.0], [2.0]),))
    model.train(True)
    state_before = trainer.state
    parameters_before = tuple(parameter.detach().clone() for parameter in model.parameters())

    result = trainer.evaluate((batch([1.0], [2.0]), batch([2.0], [4.0])))

    assert model.training
    assert trainer.state is state_before
    assert not result.optimizer_step
    assert result.state is state_before
    assert isinstance(result.metrics["loss"], float)
    for parameter, expected in zip(model.parameters(), parameters_before, strict=True):
        torch.testing.assert_close(parameter, expected, rtol=0.0, atol=0.0)
        assert parameter.grad is None


def test_nonfinite_forward_fails_before_optimizer_mutation() -> None:
    class OverflowModel(nn.Module):
        def __init__(self) -> None:
            super().__init__()
            self.offset = nn.Parameter(torch.tensor(0.0))

        def forward(self, inputs: torch.Tensor) -> torch.Tensor:
            return torch.exp(inputs + self.offset)

    model = OverflowModel()
    trainer = Trainer(
        model,
        SupervisedMSETask(),
        build_optimizer(model.parameters(), SGDConfig(learning_rate=0.1)),
    )
    before = model.offset.detach().clone()

    with pytest.raises(FloatingPointError, match="output"):
        trainer.train((batch([100.0], [0.0]),))

    torch.testing.assert_close(model.offset, before, rtol=0.0, atol=0.0)
    assert model.offset.grad is None
    assert trainer.state == TrainingState()


def test_nonfinite_gradient_fails_before_optimizer_mutation() -> None:
    class InfiniteBackward(torch.autograd.Function):
        @staticmethod
        def forward(ctx, value: torch.Tensor) -> torch.Tensor:
            return value.clone()

        @staticmethod
        def backward(ctx, gradient: torch.Tensor) -> tuple[torch.Tensor]:
            return (torch.full_like(gradient, float("inf")),)

    class BadGradientModel(nn.Module):
        def __init__(self) -> None:
            super().__init__()
            self.weight = nn.Parameter(torch.tensor(1.0))

        def forward(self, inputs: torch.Tensor) -> torch.Tensor:
            return InfiniteBackward.apply(inputs * self.weight)

    model = BadGradientModel()
    trainer = Trainer(
        model,
        SupervisedMSETask(),
        build_optimizer(model.parameters(), SGDConfig(learning_rate=0.1)),
    )
    before = model.weight.detach().clone()

    with pytest.raises(FloatingPointError, match="gradient"):
        trainer.train((batch([1.0], [0.0]),))

    torch.testing.assert_close(model.weight, before, rtol=0.0, atol=0.0)
    assert model.weight.grad is None
    assert trainer.state == TrainingState()


def test_cancellation_after_forward_clears_gradients_without_advancing_state() -> None:
    cancellation = {"requested": False}
    delegate = SupervisedMSETask()

    class CancellingTask:
        def compute(self, model: nn.Module, item: SupervisedBatch) -> TaskResult:
            result = delegate.compute(model, item)
            cancellation["requested"] = True
            return result

    model = linear_model()
    trainer = Trainer(
        model,
        CancellingTask(),
        build_optimizer(model.parameters(), SGDConfig(learning_rate=0.1)),
    )
    before = tuple(parameter.detach().clone() for parameter in model.parameters())

    with pytest.raises(Canceled, match="canceled"):
        trainer.train(
            (batch([1.0], [2.0]),),
            cancellation_check=lambda: cancellation["requested"],
        )

    assert trainer.state == TrainingState()
    for parameter, expected in zip(model.parameters(), before, strict=True):
        torch.testing.assert_close(parameter, expected, rtol=0.0, atol=0.0)
        assert parameter.grad is None


def test_optimizer_and_scheduler_order_is_explicit() -> None:
    events: list[str] = []

    class RecordingLinear(nn.Linear):
        def forward(self, inputs: torch.Tensor) -> torch.Tensor:
            events.append("forward")
            return super().forward(inputs)

    class RecordingSGD(torch.optim.SGD):
        def zero_grad(self, *args, **kwargs) -> None:
            events.append("zero_grad")
            super().zero_grad(*args, **kwargs)

        def step(self, closure: Callable[[], float] | None = None):
            events.append("optimizer")
            return super().step(closure)

    class RecordingScheduler:
        def step(self) -> None:
            events.append("scheduler")

    model = RecordingLinear(1, 1, bias=False)
    optimizer = RecordingSGD(model.parameters(), lr=0.1, foreach=False)
    model.weight.register_hook(lambda gradient: events.append("backward") or gradient)
    trainer = Trainer(model, SupervisedMSETask(), optimizer, scheduler=RecordingScheduler())

    trainer.train((batch([1.0], [0.0]),))

    assert events == ["zero_grad", "forward", "backward", "optimizer", "scheduler", "zero_grad"]


def test_optional_gradient_clipping_reports_preclip_norm_and_bounds_update() -> None:
    model = linear_model(bias=False)
    with torch.no_grad():
        model.weight.fill_(1.0)
    trainer = Trainer(
        model,
        SupervisedMSETask(),
        build_optimizer(model.parameters(), SGDConfig(learning_rate=1.0)),
        config=TrainerConfig(gradient_clip_norm=1.0),
    )
    before = model.weight.detach().clone()

    result = trainer.train((batch([10.0], [0.0]),))[0]

    assert result.metrics["gradient_norm"] == pytest.approx(200.0)
    assert torch.linalg.vector_norm(model.weight.detach() - before).item() <= 1.000001


@pytest.mark.parametrize(
    "config",
    [
        lambda: TrainerConfig(accumulation_steps=0),
        lambda: TrainerConfig(accumulation_steps=True),
        lambda: TrainerConfig(maximum_microbatches_per_call=0),
        lambda: TrainerConfig(gradient_clip_norm=0.0),
        lambda: TrainerConfig(gradient_clip_norm=float("inf")),
    ],
)
def test_trainer_config_rejects_invalid_bounds(config) -> None:
    with pytest.raises(ValueError):
        config()
