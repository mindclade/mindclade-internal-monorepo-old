"""Health projection for the Python model worker."""

from __future__ import annotations

from dataclasses import dataclass

from .protocol import WorkerPhase


@dataclass(frozen=True, slots=True)
class Health:
    phase: WorkerPhase
    loaded_models: int
    active_requests: int

    @property
    def live(self) -> bool:
        return self.phase is not WorkerPhase.STOPPED

    @property
    def ready(self) -> bool:
        return self.phase is WorkerPhase.READY
