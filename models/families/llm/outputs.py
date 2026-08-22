# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Stable, export-friendly output structures for the decoder-only LLM."""

from __future__ import annotations

from typing import NamedTuple

from torch import Tensor


class CausalLMOutput(NamedTuple):
    """Model outputs.

    ``logits`` has shape ``[B, T, vocab_size]``. ``hidden_states`` is the final
    normalized decoder representation with shape ``[B, T, hidden_size]``.
    """

    logits: Tensor
    hidden_states: Tensor
