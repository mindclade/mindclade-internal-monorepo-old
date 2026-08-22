# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Bounded decoder-only causal language model."""

from __future__ import annotations

from collections.abc import Callable

import torch
from torch import nn

from models.components.attention import AttentionOperator
from models.components.normalization import RMSNorm

from .attention import validate_attention_mask
from .config import LLMConfig
from .embeddings import TokenEmbedding
from .initialization import initialize_llm_parameters
from .layer import DecoderLayer, mask_invalid_queries
from .outputs import CausalLMOutput

AttentionOperatorFactory = Callable[[], AttentionOperator]


class DecoderOnlyLanguageModel(nn.Module):
    """A pre-RMSNorm, RoPE, SwiGLU causal language model.

    Inputs are integer ``input_ids[B, T]`` and an optional boolean
    ``attention_mask[B, T]`` where ``True`` marks a valid token. The output
    structure is stable in train and evaluation modes. This bounded vertical
    slice deliberately excludes cache, generation, loss, and checkpoint I/O.
    """

    def __init__(
        self,
        config: LLMConfig,
        *,
        attention_operator_factory: AttentionOperatorFactory | None = None,
    ) -> None:
        super().__init__()
        if not isinstance(config, LLMConfig):
            raise TypeError("config must be an LLMConfig")
        if attention_operator_factory is not None and not callable(attention_operator_factory):
            raise TypeError("attention_operator_factory must be callable or None")
        self._config = config
        # PyTorch modules initialize parameters from the process-global CPU RNG in
        # their constructors. Every parameter is replaced below using the config's
        # private generator, so preserve the caller's RNG state around construction.
        with torch.random.fork_rng(devices=[]):
            self.token_embeddings = TokenEmbedding(config)

            operators: list[AttentionOperator | None] = []
            operator_identities: set[int] = set()
            for _ in range(config.num_layers):
                operator = (
                    None if attention_operator_factory is None else attention_operator_factory()
                )
                if operator is not None and not isinstance(operator, AttentionOperator):
                    raise TypeError(
                        "attention_operator_factory must return AttentionOperator instances"
                    )
                if operator is not None:
                    identity = id(operator)
                    if identity in operator_identities:
                        raise ValueError(
                            "attention_operator_factory must return a fresh module per layer"
                        )
                    operator_identities.add(identity)
                operators.append(operator)

            self.layers = nn.ModuleList(
                DecoderLayer(config, attention_operator=operator) for operator in operators
            )
            self.final_norm = RMSNorm(config.hidden_size, eps=config.rms_norm_eps)
            self.lm_head = nn.Linear(config.hidden_size, config.vocab_size, bias=False)
            if config.tie_word_embeddings:
                self.lm_head.weight = self.token_embeddings.weight
            initialize_llm_parameters(self, config)

    @property
    def config(self) -> LLMConfig:
        """Return the immutable construction and admission configuration."""

        return self._config

    def forward(
        self,
        input_ids: torch.Tensor,
        attention_mask: torch.Tensor | None = None,
    ) -> CausalLMOutput:
        hidden_states = self.token_embeddings(input_ids)
        validate_attention_mask(
            attention_mask,
            batch_size=input_ids.shape[0],
            sequence_length=input_ids.shape[1],
            device=input_ids.device,
        )
        hidden_states = mask_invalid_queries(hidden_states, attention_mask)
        for layer in self.layers:
            hidden_states = layer(hidden_states, attention_mask)
        hidden_states = self.final_norm(hidden_states)
        hidden_states = mask_invalid_queries(hidden_states, attention_mask)
        logits = self.lm_head(hidden_states)
        logits = mask_invalid_queries(logits, attention_mask)
        return CausalLMOutput(logits=logits, hidden_states=hidden_states)
