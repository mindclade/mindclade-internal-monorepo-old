# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Process-local model-worker lifecycle."""

from __future__ import annotations

from enum import StrEnum
from threading import Lock


class State(StrEnum):
    STARTING = "starting"
    READY = "ready"
    DRAINING = "draining"
    STOPPED = "stopped"


class Lifecycle:
    def __init__(self) -> None:
        self._state = State.STARTING
        self._lock = Lock()

    @property
    def state(self) -> State:
        with self._lock:
            return self._state

    def ready(self) -> None:
        self._transition(State.STARTING, State.READY)

    def drain(self) -> None:
        with self._lock:
            if self._state is State.READY:
                self._state = State.DRAINING

    def stop(self) -> None:
        with self._lock:
            if self._state not in {State.DRAINING, State.STARTING}:
                raise RuntimeError(f"cannot stop model worker from {self._state}")
            self._state = State.STOPPED

    def accepting(self) -> bool:
        return self.state is State.READY

    def _transition(self, expected: State, target: State) -> None:
        with self._lock:
            if self._state is not expected:
                raise RuntimeError(f"expected {expected}, found {self._state}")
            self._state = target
