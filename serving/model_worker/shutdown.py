# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Shutdown helper preserving drain-before-stop semantics."""

from __future__ import annotations

from .model_runner import ModelWorker


def drain_and_stop(worker: ModelWorker) -> None:
    worker.drain()
    worker.stop()
