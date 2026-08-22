# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Causal self-attention with rotary positions and an operator injection seam."""

from __future__ import annotations

import torch
from torch import nn

from models.components.attention import (
    AttentionOperator,
    DenseMultiheadAttention,
    PyTorchSDPAOperator,
    RotaryEmbedding,
)

from .config import LLMConfig


def validate_attention_mask(
    attention_mask: torch.Tensor | None,
    *,
    batch_size: int,
    sequence_length: int,
    device: torch.device,
) -> None:
    """Validate the family mask contract: boolean ``[B, T]``, ``True`` is valid."""

    if attention_mask is None:
        return
    if not isinstance(attention_mask, torch.Tensor):
        raise TypeError("attention_mask must be a torch.Tensor or None")
    if attention_mask.layout is not torch.strided:
        raise TypeError("attention_mask must use a strided layout")
    if attention_mask.dtype is not torch.bool:
        raise TypeError("attention_mask must have dtype torch.bool")
    if attention_mask.ndim != 2 or attention_mask.shape != (batch_size, sequence_length):
        raise ValueError("attention_mask must have shape [batch, sequence]")
    if attention_mask.device != device:
        raise ValueError("attention_mask and hidden_states must be on the same device")


class RotaryAttentionOperator(AttentionOperator):
    """Apply RoPE to projected queries and keys, then call an attention operator."""

    def __init__(
        self,
        head_dim: int,
        *,
        base: float = 10_000.0,
        delegate: AttentionOperator | None = None,
    ) -> None:
        super().__init__()
        if delegate is not None and not isinstance(delegate, AttentionOperator):
            raise TypeError("delegate must implement AttentionOperator")
        self.rotary = RotaryEmbedding(head_dim, base=base)
        self.delegate = delegate if delegate is not None else PyTorchSDPAOperator()

    def forward(
        self,
        query: torch.Tensor,
        key: torch.Tensor,
        value: torch.Tensor,
        *,
        mask: torch.Tensor | None,
        causal: bool,
        scale: float | None,
        dropout_p: float,
    ) -> torch.Tensor:
        return self.delegate.forward(
            self.rotary.forward(query),
            self.rotary.forward(key),
            value,
            mask=mask,
            causal=causal,
            scale=scale,
            dropout_p=dropout_p,
        )


class CausalSelfAttention(nn.Module):
    """Decoder self-attention over ``[B, T, D]`` hidden states."""

    def __init__(
        self,
        config: LLMConfig,
        *,
        operator: AttentionOperator | None = None,
    ) -> None:
        super().__init__()
        if not isinstance(config, LLMConfig):
            raise TypeError("config must be an LLMConfig")
        if operator is not None and not isinstance(operator, AttentionOperator):
            raise TypeError("operator must implement AttentionOperator")
        self.hidden_size = config.hidden_size
        rotary_operator = RotaryAttentionOperator(
            config.head_dim,
            base=config.rotary_base,
            delegate=operator,
        )
        self.attention = DenseMultiheadAttention(
            config.hidden_size,
            config.num_heads,
            dropout=config.attention_dropout,
            bias=config.bias,
            operator=rotary_operator,
        )
        self.output_dropout = nn.Dropout(config.dropout)

    def forward(
        self,
        hidden_states: torch.Tensor,
        attention_mask: torch.Tensor | None = None,
    ) -> torch.Tensor:
        if not isinstance(hidden_states, torch.Tensor):
            raise TypeError("attention hidden_states must be a torch.Tensor")
        if hidden_states.layout is not torch.strided:
            raise TypeError("attention hidden_states must use a strided layout")
        if hidden_states.ndim != 3 or hidden_states.shape[-1] != self.hidden_size:
            raise ValueError("attention hidden_states must have shape [batch, sequence, hidden]")
        if hidden_states.numel() == 0:
            raise ValueError("attention hidden_states must be nonempty")
        parameter = self.attention.query_projection.weight
        if hidden_states.device != parameter.device:
            raise ValueError("attention hidden_states and parameters must be on the same device")
        if hidden_states.dtype != parameter.dtype:
            raise TypeError("attention hidden_states and parameters must have the same dtype")
        validate_attention_mask(
            attention_mask,
            batch_size=hidden_states.shape[0],
            sequence_length=hidden_states.shape[1],
            device=hidden_states.device,
        )
        output = self.attention.forward(
            hidden_states,
            hidden_states,
            hidden_states,
            attention_mask,
            causal=True,
        )
        return self.output_dropout.forward(output)
