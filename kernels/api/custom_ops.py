# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Graph-safe registration contract for qualified accelerator operators."""

from __future__ import annotations

from collections.abc import Callable
from dataclasses import dataclass
from typing import Any

import torch


@dataclass(frozen=True, slots=True)
class CustomOpDefinition:
    """Explicit torch.library identity with fake and optional autograd behavior."""

    namespace: str
    name: str
    device_types: tuple[str, ...]
    mutates_args: tuple[str, ...] = ()

    def __post_init__(self) -> None:
        if not self.namespace.isidentifier() or self.namespace == "torch":
            raise ValueError("custom-op namespace must be an owned Python identifier")
        if not self.name.isidentifier():
            raise ValueError("custom-op name must be a Python identifier")
        if not self.device_types or len(set(self.device_types)) != len(self.device_types):
            raise ValueError("custom ops require unique explicit device types")
        if any(device not in {"cpu", "cuda"} for device in self.device_types):
            raise ValueError("custom ops support only explicitly reviewed CPU or CUDA devices")

    @property
    def qualified_name(self) -> str:
        return f"{self.namespace}::{self.name}"

    def register(
        self,
        implementation: Callable[..., Any],
        *,
        fake: Callable[..., Any],
        backward: Callable[..., Any] | None = None,
        setup_context: Callable[..., Any] | None = None,
    ) -> Any:
        if (backward is None) != (setup_context is None):
            raise ValueError("backward and setup_context must be registered together")
        decorator = torch.library.custom_op(
            self.qualified_name,
            mutates_args=self.mutates_args,
            device_types=self.device_types,
        )
        custom_op = decorator(implementation)
        custom_op.register_fake(fake)
        if backward is not None and setup_context is not None:
            custom_op.register_autograd(backward, setup_context=setup_context)
        return custom_op
