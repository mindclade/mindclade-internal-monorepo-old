# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Owned initialization and guaranteed teardown for torchrun process groups."""

from __future__ import annotations

import datetime
import os
from collections.abc import Iterator, Mapping
from contextlib import contextmanager

import torch

from libs.python.errors import FailedPrecondition, InvalidArgument

from .context import DistributedConfig, DistributedContext, TorchrunEnvironment


def initialize(
    config: DistributedConfig | None = None,
    *,
    environ: Mapping[str, str] | None = None,
) -> DistributedContext:
    """Initialize exactly one process group from launcher-owned rank variables."""

    resolved = config or DistributedConfig()
    if not isinstance(resolved, DistributedConfig):
        raise TypeError("config must be DistributedConfig")
    if not torch.distributed.is_available():
        raise FailedPrecondition(
            "this PyTorch build does not provide distributed support",
            reason="distributed_unavailable",
        )
    if torch.distributed.is_initialized():
        raise FailedPrecondition(
            "a process group is already initialized",
            reason="distributed_already_initialized",
        )
    environment = TorchrunEnvironment.from_environ(os.environ if environ is None else environ)
    expected_world_size = 2 if resolved.backend == "gloo" else 8
    if environment.world_size != expected_world_size:
        raise InvalidArgument(
            "distributed backend does not match the approved world size",
            reason="distributed_backend_world_size",
        )

    if resolved.backend == "gloo":
        if not torch.distributed.is_gloo_available():
            raise FailedPrecondition(
                "the gloo backend is unavailable",
                reason="distributed_backend_unavailable",
            )
        device = torch.device("cpu")
    else:
        if not torch.distributed.is_nccl_available() or not torch.cuda.is_available():
            raise FailedPrecondition(
                "the nccl backend or CUDA runtime is unavailable",
                reason="distributed_backend_unavailable",
            )
        if environment.local_rank >= torch.cuda.device_count():
            raise FailedPrecondition(
                "torchrun local rank has no corresponding CUDA device",
                reason="distributed_device_unavailable",
            )
        torch.cuda.set_device(environment.local_rank)
        device = torch.device("cuda", environment.local_rank)

    try:
        torch.distributed.init_process_group(
            backend=resolved.backend,
            init_method="env://",
            rank=environment.rank,
            world_size=environment.world_size,
            timeout=datetime.timedelta(seconds=resolved.timeout_seconds),
        )
    except (RuntimeError, ValueError) as error:
        raise FailedPrecondition(
            "distributed process-group initialization failed",
            reason="distributed_initialization",
            cause=error,
        ) from error
    context = DistributedContext(environment, resolved.backend, device)
    try:
        context.validate_active()
    except BaseException:
        torch.distributed.destroy_process_group()
        raise
    return context


def teardown(context: DistributedContext) -> None:
    """Destroy the owned process group without a failure-prone final barrier."""

    if not isinstance(context, DistributedContext):
        raise TypeError("context must be DistributedContext")
    context.validate_active()
    torch.distributed.destroy_process_group()


@contextmanager
def distributed_session(
    config: DistributedConfig | None = None,
    *,
    environ: Mapping[str, str] | None = None,
) -> Iterator[DistributedContext]:
    """Initialize and always tear down the process group owned by this context."""

    context = initialize(config, environ=environ)
    try:
        yield context
    finally:
        if torch.distributed.is_available() and torch.distributed.is_initialized():
            torch.distributed.destroy_process_group()


__all__ = ["distributed_session", "initialize", "teardown"]
