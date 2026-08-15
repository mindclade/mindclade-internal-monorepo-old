# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Thin preprocessing worker adapter over the shared stage runtime."""

from __future__ import annotations

from libs.python.worker_runtime import StageEngine, StageExecutor, StageKind


def build_executor(engine: StageEngine) -> StageExecutor:
    """Compose the owning domain engine without duplicating orchestration policy."""
    return StageExecutor(StageKind.PREPROCESS, engine)
