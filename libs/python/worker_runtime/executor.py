# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Shared execution seam for Python scientific stage workers."""

from __future__ import annotations

from collections.abc import Callable
from dataclasses import dataclass
from time import time_ns
from typing import Protocol

from libs.python.errors import DeadlineExceeded, FailedPrecondition, InvalidArgument

from .contracts import StageEnvelope, StageKind, StageResult


class StageEngine(Protocol):
    """Scientific/numerical engine implemented by an owning domain package."""

    def execute(self, stage: StageEnvelope) -> StageResult: ...


def _system_now_millis() -> int:
    return time_ns() // 1_000_000


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

    def execute(self, stage: StageEnvelope) -> StageResult:
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
        now_millis = self.now_millis()
        if isinstance(now_millis, bool) or not isinstance(now_millis, int) or now_millis < 0:
            raise FailedPrecondition(
                "stage executor clock returned invalid milliseconds",
                reason="stage_clock",
            )
        if now_millis >= stage.deadline_unix_millis:
            raise DeadlineExceeded(
                "stage deadline expired before numerical execution",
                reason="stage_deadline",
                operation=stage.operation,
            )
        result = self.engine.execute(stage)
        if not isinstance(result, StageResult):
            raise FailedPrecondition(
                "stage engine returned an invalid result type",
                reason="stage_result_type",
            )
        result.validate()
        return result
