# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Token embeddings and request validation for the decoder-only LLM."""

from __future__ import annotations

import torch
from torch import nn

from .config import LLMConfig

TOKEN_DTYPES = frozenset((torch.int32, torch.int64))


def validate_input_ids(
    input_ids: torch.Tensor,
    config: LLMConfig,
    *,
    parameter: torch.Tensor,
) -> None:
    """Validate a nonempty dense ``[B, T]`` token tensor at the model boundary."""

    if not isinstance(input_ids, torch.Tensor):
        raise TypeError("input_ids must be a torch.Tensor")
    if input_ids.layout is not torch.strided:
        raise TypeError("input_ids must use a strided layout")
    if input_ids.dtype not in TOKEN_DTYPES:
        raise TypeError("input_ids must have dtype torch.int32 or torch.int64")
    if input_ids.ndim != 2:
        raise ValueError("input_ids must have shape [batch, sequence]")
    if input_ids.numel() == 0:
        raise ValueError("input_ids must be nonempty")
    config.validate_input_shape(input_ids.shape[0], input_ids.shape[1])
    if input_ids.device != parameter.device:
        raise ValueError("input_ids and model parameters must be on the same device")

    # Eager admission rejects unsafe indices before the embedding operator. Export
    # capture cannot soundly specialize a graph on tensor data, so the exported graph
    # delegates this range check to aten.embedding at runtime.
    if not torch.compiler.is_compiling():
        minimum, maximum = torch.aminmax(input_ids)
        if minimum.item() < 0 or maximum.item() >= config.vocab_size:
            raise ValueError(f"input_ids values must be in [0, {config.vocab_size})")


class TokenEmbedding(nn.Module):
    """Map integer token IDs ``[B, T]`` to hidden states ``[B, T, D]``."""

    def __init__(self, config: LLMConfig) -> None:
        super().__init__()
        if not isinstance(config, LLMConfig):
            raise TypeError("config must be an LLMConfig")
        self.config = config
        self.embedding = nn.Embedding(
            config.vocab_size,
            config.hidden_size,
            padding_idx=config.padding_idx,
        )
        self.dropout = nn.Dropout(config.dropout)

    @property
    def weight(self) -> torch.Tensor:
        """Expose the registered embedding weight for deliberate LM-head tying."""

        return self.embedding.weight

    def forward(self, input_ids: torch.Tensor) -> torch.Tensor:
        validate_input_ids(input_ids, self.config, parameter=self.weight)
        embedded = self.embedding.forward(input_ids)
        return self.dropout.forward(embedded)
