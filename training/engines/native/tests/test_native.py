# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""The native engine remains a thin adapter over Trainer."""

import pytest
import torch
from torch import nn

from libs.python.errors import Canceled
from training.contracts import SupervisedBatch, TrainingState
from training.core import Trainer
from training.engines.native import NativeEngine
from training.optim import SGDConfig, build_optimizer
from training.tasks import SupervisedMSETask


def build_engine() -> NativeEngine:
    model = nn.Linear(1, 1)
    trainer = Trainer(
        model,
        SupervisedMSETask(),
        build_optimizer(model.parameters(), SGDConfig(learning_rate=0.05)),
    )
    return NativeEngine(trainer)


def test_native_engine_delegates_train_evaluate_and_state() -> None:
    engine = build_engine()
    batch = SupervisedBatch(torch.tensor([[1.0]]), torch.tensor([[2.0]]))

    trained = engine.train((batch,))
    evaluated = engine.evaluate((batch,))

    assert len(trained) == 1
    assert trained[0].optimizer_step
    assert not evaluated.optimizer_step
    assert engine.state == TrainingState(microbatches=1, optimizer_steps=1, samples=1)
    assert engine.trainer.state is engine.state


def test_native_engine_forwards_cancellation() -> None:
    engine = build_engine()
    batch = SupervisedBatch(torch.tensor([[1.0]]), torch.tensor([[2.0]]))

    with pytest.raises(Canceled, match="canceled"):
        engine.train((batch,), cancellation_check=lambda: True)

    assert engine.state == TrainingState()


def test_native_engine_requires_authoritative_trainer() -> None:
    with pytest.raises(ValueError, match="Trainer"):
        NativeEngine(object())  # type: ignore[arg-type]


def test_reference_trainer_rejects_non_float32_model_state() -> None:
    model = nn.Linear(1, 1).double()
    optimizer = torch.optim.SGD(model.parameters(), lr=0.1)

    with pytest.raises(ValueError, match="float32"):
        Trainer(model, SupervisedMSETask(), optimizer)
