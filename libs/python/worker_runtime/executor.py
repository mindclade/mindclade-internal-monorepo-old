"""Shared execution seam for Python scientific stage workers."""

from __future__ import annotations

from collections.abc import Callable
from dataclasses import dataclass
from time import time
from typing import Protocol

from .contracts import StageEnvelope, StageKind, StageResult


class StageEngine(Protocol):
    """Scientific/numerical engine implemented by an owning domain package."""

    def execute(self, stage: StageEnvelope) -> StageResult: ...


@dataclass(slots=True)
class StageExecutor:
    kind: StageKind
    engine: StageEngine
    now_millis: Callable[[], int] = lambda: int(time() * 1000)

    def execute(self, stage: StageEnvelope) -> StageResult:
        stage.validate()
        if stage.kind is not self.kind:
            raise ValueError(f"executor for {self.kind.value} cannot run {stage.kind.value}")
        if self.now_millis() >= stage.deadline_unix_millis:
            raise TimeoutError("stage deadline has expired before numerical execution")
        result = self.engine.execute(stage)
        result.validate()
        return result
