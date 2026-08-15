"""Shutdown helper preserving drain-before-stop semantics."""

from __future__ import annotations

from .model_runner import ModelWorker


def drain_and_stop(worker: ModelWorker) -> None:
    worker.drain()
    worker.stop()
