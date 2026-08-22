# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Pre-normalized decoder transformer layer."""

from __future__ import annotations

import torch
from torch import nn

from models.components.attention import AttentionOperator
from models.components.nn import ResidualAdd
from models.components.normalization import RMSNorm

from .attention import CausalSelfAttention, validate_attention_mask
from .config import LLMConfig
from .feed_forward import DecoderFeedForward


def mask_invalid_queries(
    hidden_states: torch.Tensor,
    attention_mask: torch.Tensor | None,
) -> torch.Tensor:
    """Make the family contract explicit: invalid query positions are exact zero."""

    if attention_mask is None:
        return hidden_states
    return torch.where(
        attention_mask.unsqueeze(-1),
        hidden_states,
        torch.zeros_like(hidden_states),
    )


class DecoderLayer(nn.Module):
    """A causal pre-RMSNorm attention and SwiGLU layer."""

    def __init__(
        self,
        config: LLMConfig,
        *,
        attention_operator: AttentionOperator | None = None,
    ) -> None:
        super().__init__()
        if not isinstance(config, LLMConfig):
            raise TypeError("config must be an LLMConfig")
        self.hidden_size = config.hidden_size
        self.attention_norm = RMSNorm(config.hidden_size, eps=config.rms_norm_eps)
        self.attention = CausalSelfAttention(config, operator=attention_operator)
        self.feed_forward_norm = RMSNorm(config.hidden_size, eps=config.rms_norm_eps)
        self.feed_forward = DecoderFeedForward(config)
        self.residual_add = ResidualAdd()

    def forward(
        self,
        hidden_states: torch.Tensor,
        attention_mask: torch.Tensor | None = None,
    ) -> torch.Tensor:
        if not isinstance(hidden_states, torch.Tensor):
            raise TypeError("decoder hidden_states must be a torch.Tensor")
        if hidden_states.ndim != 3 or hidden_states.shape[-1] != self.hidden_size:
            raise ValueError("decoder hidden_states must have shape [batch, sequence, hidden]")
        validate_attention_mask(
            attention_mask,
            batch_size=hidden_states.shape[0],
            sequence_length=hidden_states.shape[1],
            device=hidden_states.device,
        )
        hidden_states = mask_invalid_queries(hidden_states, attention_mask)
        attention_output = self.attention(self.attention_norm(hidden_states), attention_mask)
        hidden_states = self.residual_add(hidden_states, attention_output)
        hidden_states = mask_invalid_queries(hidden_states, attention_mask)
        feed_forward_output = self.feed_forward(self.feed_forward_norm(hidden_states))
        hidden_states = self.residual_add(hidden_states, feed_forward_output)
        return mask_invalid_queries(hidden_states, attention_mask)
