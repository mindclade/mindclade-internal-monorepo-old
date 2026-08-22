# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Thin training worker adapter over the shared stage runtime."""

from __future__ import annotations

from collections.abc import Callable
from time import time_ns
from typing import Protocol, runtime_checkable

from libs.python.errors import FailedPrecondition, InvalidArgument
from libs.python.worker_runtime import (
    CancellationToken,
    ExecutionContext,
    StageEngine,
    StageEnvelope,
    StageExecutor,
    StageKind,
    StageResult,
)


def _system_now_millis() -> int:
    return time_ns() // 1_000_000


@runtime_checkable
class TerminalCommitEngine(Protocol):
    """Marker implemented only by an engine with the documented terminal transaction."""

    @property
    def owns_terminal_commit(self) -> bool: ...

    def execute(self, stage: StageEnvelope, context: ExecutionContext) -> StageResult: ...


class TrainingStageExecutor(StageExecutor):
    """Training executor whose engine owns the irreversible terminal-commit boundary.

    The generic runtime checks the clock again after an engine returns. That is correct for
    engines which have not committed externally visible state, but this worker returns only after
    its injected checkpoint authority atomically commits checkpoint, registry, outputs, fence,
    and terminal status. Reclassifying that accepted result as retryable because the clock
    advanced while the commit response was in flight would permit a duplicate terminal attempt.

    This adapter preserves every generic precondition and result validation. It deliberately has
    no post-return interruption check: the engine performs a collective interruption check just
    before the committer call, and the committer must enforce the deadline and fence in the same
    atomic transaction as the terminal commit.
    """

    def execute(
        self,
        stage: StageEnvelope,
        *,
        cancellation: CancellationToken | None = None,
    ) -> StageResult:
        if not isinstance(stage, StageEnvelope):
            raise InvalidArgument(
                "executor stage must be a StageEnvelope",
                reason="stage_envelope_type",
            )
        stage.validate()
        if stage.kind is not self.kind:
            raise FailedPrecondition(
                f"executor for {self.kind.value} cannot run {stage.kind.value}",
                reason="stage_kind_mismatch",
                fields={"executor_kind": self.kind.value, "stage_kind": stage.kind.value},
            )
        if cancellation is not None and not isinstance(cancellation, CancellationToken):
            raise InvalidArgument(
                "executor cancellation must be a CancellationToken",
                reason="executor_cancellation",
            )
        context = ExecutionContext(
            stage.deadline_unix_millis,
            self.now_millis,
            cancellation or CancellationToken(),
        )
        context.checkpoint(operation=stage.operation)
        result = self.engine.execute(stage, context)
        if not isinstance(result, StageResult):
            raise FailedPrecondition(
                "stage engine returned an invalid result type",
                reason="stage_result_type",
            )
        result.validate()
        return result


def build_executor(
    engine: StageEngine,
    *,
    now_millis: Callable[[], int] = _system_now_millis,
) -> StageExecutor:
    """Compose exact training terminal-commit semantics over shared runtime types."""

    if isinstance(engine, TerminalCommitEngine) and engine.owns_terminal_commit:
        return TrainingStageExecutor(StageKind.TRAINING, engine, now_millis)
    return StageExecutor(StageKind.TRAINING, engine, now_millis)


__all__ = ["TerminalCommitEngine", "TrainingStageExecutor", "build_executor"]
