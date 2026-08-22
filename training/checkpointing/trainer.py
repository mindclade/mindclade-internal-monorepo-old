# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Scheduler-safe checkpoint entry points for the authoritative Trainer."""

from __future__ import annotations

from pathlib import Path

from libs.python.errors import FailedPrecondition, InvalidArgument
from training.core import Trainer

from .dcp import DCPManifest, save_distributed_checkpoint
from .manifest import CheckpointIdentity, CheckpointManifest
from .resume import save_local_checkpoint


def save_local_trainer_checkpoint(
    destination: Path,
    *,
    trainer: Trainer,
    identity: CheckpointIdentity,
    data_position: int,
) -> CheckpointManifest:
    """Save one scheduler-free Trainer through the bounded local-v1 format."""

    resolved = _checkpointable_trainer(trainer)
    return save_local_checkpoint(
        destination,
        model=resolved.model,
        optimizer=resolved.optimizer,
        training_state=resolved.state,
        identity=identity,
        data_position=data_position,
    )


def save_distributed_trainer_checkpoint(
    destination: Path,
    *,
    trainer: Trainer,
    identity: CheckpointIdentity,
    data_position: int,
) -> DCPManifest:
    """Save one scheduler-free Trainer through distributed-v1."""

    resolved = _checkpointable_trainer(trainer)
    return save_distributed_checkpoint(
        destination,
        model=resolved.model,
        optimizer=resolved.optimizer,
        training_state=resolved.state,
        identity=identity,
        data_position=data_position,
    )


def _checkpointable_trainer(value: object) -> Trainer:
    if not isinstance(value, Trainer):
        raise InvalidArgument(
            "Trainer checkpoint API requires a Trainer",
            reason="checkpoint_trainer",
        )
    if value.scheduler is not None:
        raise FailedPrecondition(
            "scheduler-backed Trainer checkpointing is unsupported because scheduler state is "
            "not part of local-v1 or distributed-v1",
            reason="checkpoint_scheduler_unsupported",
        )
    if not value.healthy:
        raise FailedPrecondition(
            "a poisoned Trainer cannot be checkpointed; restore into fresh objects",
            reason="checkpoint_trainer_poisoned",
        )
    return value


__all__ = ["save_distributed_trainer_checkpoint", "save_local_trainer_checkpoint"]
