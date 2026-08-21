# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Composition factory invoked by the Rust-supervised worker process."""

from __future__ import annotations

from libs.python.worker_runtime import StageEngine, StageWorker, WorkerLimits

from .executor import build_executor


def build_worker(engine: StageEngine, limits: WorkerLimits | None = None) -> StageWorker:
    worker = StageWorker(build_executor(engine), limits)
    worker.ready()
    return worker
