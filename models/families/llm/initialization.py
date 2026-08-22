# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Deterministic, side-effect-free parameter initialization for the LLM family."""

from __future__ import annotations

import torch
from torch import nn

from .config import LLMConfig


def initialize_llm_parameters(module: nn.Module, config: LLMConfig) -> None:
    """Initialize registered parameters without changing the process-global RNG.

    Matrix parameters use ``N(0, initializer_std)``; biases use zero and other
    vector parameters (normalization scales) use one. Shared parameters are written
    exactly once. Construction currently occurs on CPU and placement remains the
    caller's responsibility.
    """

    if not isinstance(module, nn.Module):
        raise TypeError("module must be a torch.nn.Module")
    if not isinstance(config, LLMConfig):
        raise TypeError("config must be an LLMConfig")

    parameters = tuple(module.named_parameters(remove_duplicate=True))
    if any(parameter.device.type != "cpu" for _, parameter in parameters):
        raise ValueError("LLM initialization requires CPU parameters before caller placement")

    generator = torch.Generator(device="cpu")
    generator.manual_seed(config.initialization_seed)
    with torch.no_grad():
        for name, parameter in parameters:
            if name.endswith("bias"):
                parameter.zero_()
            elif parameter.ndim == 1:
                parameter.fill_(1.0)
            else:
                nn.init.normal_(
                    parameter, mean=0.0, std=config.initializer_std, generator=generator
                )

        if config.padding_idx is not None:
            embedding = getattr(module, "token_embeddings", None)
            if embedding is None or not hasattr(embedding, "weight"):
                raise RuntimeError("configured padding_idx requires token_embeddings.weight")
            embedding.weight[config.padding_idx].zero_()
