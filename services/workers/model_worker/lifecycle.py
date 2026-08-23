# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Shared lifecycle names exposed at the service boundary.

This adapter used to carry its own ``Lifecycle``/``State`` pair. That copy tracked only a
phase, never an in-flight execution count, so ``stop()`` succeeded while the injected engine
was still running and about to publish results — the supervisor saw a stopped worker and was
free to reissue the same work elsewhere. It also raised bare ``RuntimeError`` across the Rust
supervision boundary instead of the shared ``libs.python.errors`` contract. Every other worker
in ``services/workers`` re-exports the shared runtime lifecycle; this one now does too.
"""

from libs.python.worker_runtime import WorkerLifecycle as Lifecycle
from libs.python.worker_runtime import WorkerState as State

__all__ = ["Lifecycle", "State"]
