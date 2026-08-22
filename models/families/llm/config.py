# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Immutable configuration and resource limits for the decoder-only LLM."""

from __future__ import annotations

import math
from dataclasses import dataclass
from typing import Final

MAXIMUM_VOCABULARY_SIZE: Final = 2_000_000
MAXIMUM_SEQUENCE_LENGTH: Final = 131_072
MAXIMUM_LAYER_COUNT: Final = 1_024
MAXIMUM_HIDDEN_SIZE: Final = 131_072
MAXIMUM_ATTENTION_HEADS: Final = 2_048
MAXIMUM_INTERMEDIATE_SIZE: Final = 524_288
MAXIMUM_BATCH_SIZE: Final = 65_536
MAXIMUM_TOKENS_PER_BATCH: Final = 1_048_576
MAXIMUM_ATTENTION_ELEMENTS: Final = 2_147_483_648
MAXIMUM_MODEL_PARAMETERS: Final = 100_000_000_000
MAXIMUM_INITIALIZATION_SEED: Final = (1 << 63) - 1


def _positive_integer(name: str, value: int, maximum: int) -> int:
    if isinstance(value, bool) or not isinstance(value, int):
        raise TypeError(f"{name} must be an integer")
    if value <= 0 or value > maximum:
        raise ValueError(f"{name} must be in [1, {maximum}]")
    return value


def _probability(name: str, value: float) -> float:
    if isinstance(value, bool) or not isinstance(value, int | float):
        raise TypeError(f"{name} must be a real number")
    resolved = float(value)
    if not math.isfinite(resolved) or not 0.0 <= resolved < 1.0:
        raise ValueError(f"{name} must be finite and in [0, 1)")
    return resolved


def _positive_float(name: str, value: float) -> float:
    if isinstance(value, bool) or not isinstance(value, int | float):
        raise TypeError(f"{name} must be a real number")
    resolved = float(value)
    if not math.isfinite(resolved) or resolved <= 0.0:
        raise ValueError(f"{name} must be finite and positive")
    return resolved


@dataclass(frozen=True, slots=True)
class LLMConfig:
    """Construction and runtime resource contract for ``DecoderOnlyLanguageModel``.

    ``maximum_*`` fields are admission limits checked on every eager forward. They
    describe supported requests; they are not hints and the model never truncates an
    input to make it fit. The attention-element budget is ``B * H * T * T``.
    """

    vocab_size: int
    max_sequence_length: int
    num_layers: int
    hidden_size: int
    num_heads: int
    intermediate_size: int
    dropout: float = 0.0
    attention_dropout: float = 0.0
    rms_norm_eps: float = 1e-6
    rotary_base: float = 10_000.0
    initializer_std: float = 0.02
    initialization_seed: int = 0
    bias: bool = False
    tie_word_embeddings: bool = True
    padding_idx: int | None = None
    maximum_batch_size: int = 256
    maximum_tokens_per_batch: int = 65_536
    maximum_attention_elements: int = 268_435_456
    maximum_model_parameters: int = 100_000_000_000

    def __post_init__(self) -> None:
        integer_fields = (
            ("vocab_size", self.vocab_size, MAXIMUM_VOCABULARY_SIZE),
            ("max_sequence_length", self.max_sequence_length, MAXIMUM_SEQUENCE_LENGTH),
            ("num_layers", self.num_layers, MAXIMUM_LAYER_COUNT),
            ("hidden_size", self.hidden_size, MAXIMUM_HIDDEN_SIZE),
            ("num_heads", self.num_heads, MAXIMUM_ATTENTION_HEADS),
            ("intermediate_size", self.intermediate_size, MAXIMUM_INTERMEDIATE_SIZE),
            ("maximum_batch_size", self.maximum_batch_size, MAXIMUM_BATCH_SIZE),
            (
                "maximum_tokens_per_batch",
                self.maximum_tokens_per_batch,
                MAXIMUM_TOKENS_PER_BATCH,
            ),
            (
                "maximum_attention_elements",
                self.maximum_attention_elements,
                MAXIMUM_ATTENTION_ELEMENTS,
            ),
            (
                "maximum_model_parameters",
                self.maximum_model_parameters,
                MAXIMUM_MODEL_PARAMETERS,
            ),
        )
        for name, value, maximum in integer_fields:
            _positive_integer(name, value, maximum)

        if self.hidden_size % self.num_heads != 0:
            raise ValueError("hidden_size must be divisible by num_heads")
        if self.head_dim < 2 or self.head_dim % 2 != 0:
            raise ValueError("attention head_dim must be even and at least 2 for rotary embedding")

        object.__setattr__(self, "dropout", _probability("dropout", self.dropout))
        object.__setattr__(
            self,
            "attention_dropout",
            _probability("attention_dropout", self.attention_dropout),
        )
        object.__setattr__(
            self,
            "rms_norm_eps",
            _positive_float("rms_norm_eps", self.rms_norm_eps),
        )
        object.__setattr__(
            self,
            "rotary_base",
            _positive_float("rotary_base", self.rotary_base),
        )
        if self.rotary_base <= 1.0:
            raise ValueError("rotary_base must be greater than one")
        object.__setattr__(
            self,
            "initializer_std",
            _positive_float("initializer_std", self.initializer_std),
        )

        if isinstance(self.initialization_seed, bool) or not isinstance(
            self.initialization_seed, int
        ):
            raise TypeError("initialization_seed must be an integer")
        if not 0 <= self.initialization_seed <= MAXIMUM_INITIALIZATION_SEED:
            raise ValueError(f"initialization_seed must be in [0, {MAXIMUM_INITIALIZATION_SEED}]")
        for name, value in (
            ("bias", self.bias),
            ("tie_word_embeddings", self.tie_word_embeddings),
        ):
            if not isinstance(value, bool):
                raise TypeError(f"{name} must be a boolean")

        if self.padding_idx is not None:
            if isinstance(self.padding_idx, bool) or not isinstance(self.padding_idx, int):
                raise TypeError("padding_idx must be an integer or None")
            if not 0 <= self.padding_idx < self.vocab_size:
                raise ValueError("padding_idx must be in the vocabulary range")

        if self.estimated_parameter_count > self.maximum_model_parameters:
            raise ValueError(
                "model parameter estimate exceeds maximum_model_parameters: "
                f"{self.estimated_parameter_count} > {self.maximum_model_parameters}"
            )

    @property
    def head_dim(self) -> int:
        """Return the per-head channel width."""

        return self.hidden_size // self.num_heads

    @property
    def estimated_parameter_count(self) -> int:
        """Return a conservative parameter count for admission control."""

        embedding = self.vocab_size * self.hidden_size
        attention = 4 * self.hidden_size * self.hidden_size
        feed_forward = 3 * self.hidden_size * self.intermediate_size
        normalization = 2 * self.hidden_size
        layer_bias = 0
        if self.bias:
            layer_bias = (4 * self.hidden_size) + (2 * self.intermediate_size) + self.hidden_size
        decoder = self.num_layers * (attention + feed_forward + normalization + layer_bias)
        final_norm = self.hidden_size
        untied_head = 0 if self.tie_word_embeddings else embedding
        return embedding + decoder + final_norm + untied_head

    def validate_input_shape(self, batch_size: int, sequence_length: int) -> None:
        """Validate a concrete ``[B, T]`` request against declared limits."""

        _positive_integer("batch_size", batch_size, self.maximum_batch_size)
        _positive_integer("sequence_length", sequence_length, self.max_sequence_length)
        tokens = batch_size * sequence_length
        if tokens > self.maximum_tokens_per_batch:
            raise ValueError(
                "input exceeds maximum_tokens_per_batch: "
                f"{tokens} > {self.maximum_tokens_per_batch}"
            )
        attention_elements = batch_size * self.num_heads * sequence_length * sequence_length
        if attention_elements > self.maximum_attention_elements:
            raise ValueError(
                "input exceeds maximum_attention_elements: "
                f"{attention_elements} > {self.maximum_attention_elements}"
            )
