# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Small deterministic fixture for contract tests and CPU smoke execution."""

from __future__ import annotations

from models.families.llm.config import LLMConfig
from models.families.llm.model import DecoderOnlyLanguageModel


def tiny_llm_config() -> LLMConfig:
    """Return the canonical tiny configuration; it is not a trained model."""

    return LLMConfig(
        vocab_size=32,
        max_sequence_length=16,
        num_layers=2,
        hidden_size=16,
        num_heads=4,
        intermediate_size=32,
        dropout=0.1,
        attention_dropout=0.1,
        initialization_seed=17,
        padding_idx=0,
        maximum_batch_size=8,
        maximum_tokens_per_batch=128,
        maximum_attention_elements=8_192,
        maximum_model_parameters=1_000_000,
    )


def build_tiny_llm(config: LLMConfig | None = None) -> DecoderOnlyLanguageModel:
    """Build a fresh tiny decoder-only LLM with deterministic initial weights."""

    if config is not None and not isinstance(config, LLMConfig):
        raise TypeError("config must be an LLMConfig or None")
    return DecoderOnlyLanguageModel(config if config is not None else tiny_llm_config())
