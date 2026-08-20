# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Shared execution seam for Python scientific stage workers."""

from __future__ import annotations

from collections.abc import Callable
from dataclasses import dataclass
from threading import Event
from time import time_ns
from typing import Protocol

from libs.python.errors import Canceled, DeadlineExceeded, FailedPrecondition, InvalidArgument

from .contracts import StageEnvelope, StageKind, StageResult


class StageEngine(Protocol):
    """Scientific/numerical engine implemented by an owning domain package."""

    def execute(self, stage: StageEnvelope, context: ExecutionContext) -> StageResult: ...


def _system_now_millis() -> int:
    return time_ns() // 1_000_000


class CancellationToken:
    """Thread-safe, process-local cooperative cancellation signal."""

    __slots__ = ("_event",)

    def __init__(self) -> None:
        self._event = Event()

    def cancel(self) -> None:
        self._event.set()

    @property
    def is_cancelled(self) -> bool:
        return self._event.is_set()


@dataclass(frozen=True, slots=True)
class ExecutionContext:
    """Deadline and cancellation state an engine checks at safe interruption points."""

    deadline_unix_millis: int
    now_millis: Callable[[], int]
    cancellation: CancellationToken

    def __post_init__(self) -> None:
        if (
            isinstance(self.deadline_unix_millis, bool)
            or not isinstance(self.deadline_unix_millis, int)
            or self.deadline_unix_millis < 0
        ):
            raise InvalidArgument("execution deadline is invalid", reason="stage_deadline")
        if not callable(self.now_millis):
            raise InvalidArgument("execution clock must be callable", reason="executor_clock")
        if not isinstance(self.cancellation, CancellationToken):
            raise InvalidArgument(
                "execution cancellation must be a CancellationToken",
                reason="executor_cancellation",
            )

    def current_millis(self) -> int:
        value = self.now_millis()
        if isinstance(value, bool) or not isinstance(value, int) or value < 0:
            raise FailedPrecondition(
                "stage executor clock returned invalid milliseconds",
                reason="stage_clock",
            )
        return value

    def remaining_millis(self) -> int:
        """Return remaining time, clamped to zero for an expired deadline."""
        return max(0, self.deadline_unix_millis - self.current_millis())

    def checkpoint(self, *, operation: str = "") -> None:
        """Raise when cancellation or the absolute deadline forbids more work."""
        if self.cancellation.is_cancelled:
            raise Canceled(
                "stage execution was canceled",
                reason="stage_canceled",
                operation=operation,
            )
        if self.current_millis() >= self.deadline_unix_millis:
            raise DeadlineExceeded(
                "stage deadline expired during numerical execution",
                reason="stage_deadline",
                operation=operation,
            )


@dataclass(frozen=True, slots=True)
class StageExecutor:
    kind: StageKind
    engine: StageEngine
    now_millis: Callable[[], int] = _system_now_millis

    def __post_init__(self) -> None:
        if not isinstance(self.kind, StageKind):
            raise InvalidArgument("executor kind is invalid", reason="executor_kind")
        if not callable(getattr(self.engine, "execute", None)):
            raise InvalidArgument(
                "executor engine must implement execute", reason="executor_engine"
            )
        if not callable(self.now_millis):
            raise InvalidArgument("executor clock must be callable", reason="executor_clock")

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
        # A non-cooperative engine cannot publish success after its deadline or
        # cancellation even if it neglected to checkpoint while it was running.
        context.checkpoint(operation=stage.operation)
        if not isinstance(result, StageResult):
            raise FailedPrecondition(
                "stage engine returned an invalid result type",
                reason="stage_result_type",
            )
        result.validate()
        return result
