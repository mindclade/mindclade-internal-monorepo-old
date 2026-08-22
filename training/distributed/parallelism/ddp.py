# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""One-process-per-device DistributedDataParallel wrapper."""

from __future__ import annotations

import torch
from torch import nn
from torch.nn.parallel import DistributedDataParallel

from libs.python.errors import FailedPrecondition, InvalidArgument
from training.distributed.context import DistributedContext


def wrap_ddp(model: nn.Module, context: DistributedContext) -> DistributedDataParallel:
    """Wrap one explicitly placed float32 model in the active default process group."""

    if not isinstance(model, nn.Module):
        raise InvalidArgument(
            "DDP model must be an nn.Module",
            reason="distributed_model",
        )
    if isinstance(model, DistributedDataParallel):
        raise InvalidArgument(
            "model is already wrapped in DistributedDataParallel",
            reason="distributed_model_wrapped",
        )
    if not isinstance(context, DistributedContext):
        raise InvalidArgument(
            "DDP wrapper requires a DistributedContext",
            reason="distributed_context",
        )
    context.validate_active()
    trainable = 0
    state = (*model.named_parameters(), *model.named_buffers())
    if not state:
        raise FailedPrecondition(
            "DDP model must contain parameter or buffer state",
            reason="distributed_model_empty",
        )
    for name, tensor in state:
        if tensor.device != context.device:
            raise FailedPrecondition(
                "DDP model state must be explicitly placed on the context device",
                reason="distributed_model_device",
                fields={"tensor": name},
            )
        if tensor.is_floating_point() and tensor.dtype is not torch.float32:
            raise FailedPrecondition(
                "DDP reference model state must be float32",
                reason="distributed_model_dtype",
                fields={"tensor": name},
            )
        if isinstance(tensor, nn.Parameter) and tensor.requires_grad:
            trainable += 1
    if trainable == 0:
        raise FailedPrecondition(
            "DDP model must contain a trainable parameter",
            reason="distributed_model_trainable",
        )

    if context.device.type == "cuda":
        return DistributedDataParallel(
            model,
            device_ids=[context.local_rank],
            output_device=context.local_rank,
            forward_sync_buffers=True,
            find_unused_parameters=False,
        )
    return DistributedDataParallel(
        model,
        device_ids=None,
        forward_sync_buffers=True,
        find_unused_parameters=False,
    )


__all__ = ["wrap_ddp"]
