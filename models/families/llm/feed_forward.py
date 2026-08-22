# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Family-level SwiGLU feed-forward block."""

from __future__ import annotations

import torch
from torch import nn

from models.components.nn import SwiGLUFeedForward

from .config import LLMConfig


class DecoderFeedForward(nn.Module):
    """Apply SwiGLU expansion/projection followed by residual-path dropout."""

    def __init__(self, config: LLMConfig) -> None:
        super().__init__()
        if not isinstance(config, LLMConfig):
            raise TypeError("config must be an LLMConfig")
        self.hidden_size = config.hidden_size
        self.feed_forward = SwiGLUFeedForward(
            config.hidden_size,
            config.intermediate_size,
            bias=config.bias,
        )
        self.dropout = nn.Dropout(config.dropout)

    def forward(self, hidden_states: torch.Tensor) -> torch.Tensor:
        if not isinstance(hidden_states, torch.Tensor):
            raise TypeError("feed-forward hidden_states must be a torch.Tensor")
        if hidden_states.ndim != 3 or hidden_states.shape[-1] != self.hidden_size:
            raise ValueError("feed-forward hidden_states must have shape [batch, sequence, hidden]")
        projected = self.feed_forward.forward(hidden_states)
        return self.dropout.forward(projected)
