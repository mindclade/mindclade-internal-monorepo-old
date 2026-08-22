# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Dense batch-major multi-head attention with an injected operator seam."""

from __future__ import annotations

import math

import torch
from torch import nn

from models.components.attention.api import AttentionOperator, PyTorchSDPAOperator


class DenseMultiheadAttention(nn.Module):
    """Project and attend dense ``[batch, sequence, channel]`` tensors.

    ``mask`` must be boolean and uses ``True`` for allowed pairs. Accepted mask
    layouts are ``[B, Tk]``, ``[B, Tq, Tk]``, and ``[B, 1|H, Tq, Tk]``. A query
    row with no allowed keys produces an exact zero, including when the output
    projection has a bias.
    """

    def __init__(
        self,
        embed_dim: int,
        num_heads: int,
        *,
        dropout: float = 0.0,
        bias: bool = True,
        scale: float | None = None,
        operator: AttentionOperator | None = None,
    ) -> None:
        super().__init__()
        if isinstance(embed_dim, bool) or not isinstance(embed_dim, int):
            raise TypeError("embed_dim must be an integer")
        if embed_dim <= 0:
            raise ValueError("embed_dim must be a positive integer")
        if isinstance(num_heads, bool) or not isinstance(num_heads, int):
            raise TypeError("num_heads must be an integer")
        if num_heads <= 0:
            raise ValueError("num_heads must be a positive integer")
        if embed_dim % num_heads != 0:
            raise ValueError("embed_dim must be divisible by num_heads")
        if isinstance(dropout, bool) or not isinstance(dropout, int | float):
            raise TypeError("dropout must be a real number")
        resolved_dropout = float(dropout)
        if not 0.0 <= resolved_dropout < 1.0 or not math.isfinite(resolved_dropout):
            raise ValueError("dropout must be finite and in [0, 1)")
        resolved_scale: float | None = None
        if scale is not None:
            if isinstance(scale, bool) or not isinstance(scale, int | float):
                raise TypeError("scale must be a real number or None")
            resolved_scale = float(scale)
            if resolved_scale <= 0.0 or not math.isfinite(resolved_scale):
                raise ValueError("scale must be finite and positive")
        if operator is not None and not isinstance(operator, AttentionOperator):
            raise TypeError("operator must implement AttentionOperator")

        self.embed_dim = embed_dim
        self.num_heads = num_heads
        self.head_dim = embed_dim // num_heads
        self.dropout = resolved_dropout
        self.scale = resolved_scale
        self.query_projection = nn.Linear(embed_dim, embed_dim, bias=bias)
        self.key_projection = nn.Linear(embed_dim, embed_dim, bias=bias)
        self.value_projection = nn.Linear(embed_dim, embed_dim, bias=bias)
        self.output_projection = nn.Linear(embed_dim, embed_dim, bias=bias)
        self.operator = operator if operator is not None else PyTorchSDPAOperator()

    def forward(
        self,
        query: torch.Tensor,
        key: torch.Tensor,
        value: torch.Tensor,
        mask: torch.Tensor | None = None,
        *,
        causal: bool = False,
    ) -> torch.Tensor:
        self._validate_inputs(query, key, value)
        if not isinstance(causal, bool):
            raise TypeError("causal must be boolean")

        batch_size = query.shape[0]
        query_length = query.shape[1]
        key_length = key.shape[1]
        projected_query = self._split_heads(self.query_projection(query))
        projected_key = self._split_heads(self.key_projection(key))
        projected_value = self._split_heads(self.value_projection(value))

        canonical_mask = self._canonical_mask(
            mask,
            batch_size=batch_size,
            query_length=query_length,
            key_length=key_length,
            device=query.device,
        )
        operator_causal = causal
        if causal and canonical_mask is not None:
            causal_mask = torch.ones(
                (query_length, key_length),
                dtype=torch.bool,
                device=query.device,
            ).tril()
            canonical_mask = canonical_mask & causal_mask.view(1, 1, query_length, key_length)
            operator_causal = False

        attended = self.operator(
            projected_query,
            projected_key,
            projected_value,
            mask=canonical_mask,
            causal=operator_causal,
            scale=self.scale,
            dropout_p=self.dropout if self.training else 0.0,
        )
        self._validate_operator_output(attended, projected_query)

        output_row_is_valid: torch.Tensor | None = None
        if canonical_mask is not None:
            head_row_is_valid = canonical_mask.any(dim=-1, keepdim=True)
            attended = torch.where(head_row_is_valid, attended, torch.zeros_like(attended))
            output_row_is_valid = canonical_mask.any(dim=-1).any(dim=1)

        merged = attended.transpose(1, 2).reshape(batch_size, query_length, self.embed_dim)
        output: torch.Tensor = self.output_projection(merged)
        if output_row_is_valid is not None:
            output = torch.where(
                output_row_is_valid.unsqueeze(-1),
                output,
                torch.zeros_like(output),
            )
        return output

    def _split_heads(self, value: torch.Tensor) -> torch.Tensor:
        return value.reshape(
            value.shape[0], value.shape[1], self.num_heads, self.head_dim
        ).transpose(1, 2)

    def _validate_inputs(
        self,
        query: torch.Tensor,
        key: torch.Tensor,
        value: torch.Tensor,
    ) -> None:
        if not all(isinstance(item, torch.Tensor) for item in (query, key, value)):
            raise TypeError("query, key, and value must be tensors")
        if query.ndim != 3 or key.ndim != 3 or value.ndim != 3:
            raise ValueError("query, key, and value must have shape [B, T, C]")
        if query.shape[0] != key.shape[0] or key.shape[0] != value.shape[0]:
            raise ValueError("query, key, and value batch dimensions must match")
        if key.shape[1] != value.shape[1]:
            raise ValueError("key and value sequence lengths must match")
        if query.shape[1] == 0 or key.shape[1] == 0:
            raise ValueError("attention sequence lengths must be non-zero")
        if any(item.shape[-1] != self.embed_dim for item in (query, key, value)):
            raise ValueError(f"attention channel dimension must equal {self.embed_dim}")
        if not all(item.is_floating_point() for item in (query, key, value)):
            raise TypeError("query, key, and value must use floating-point dtypes")
        if query.dtype != key.dtype or key.dtype != value.dtype:
            raise TypeError("query, key, and value dtypes must match")
        if query.device != key.device or key.device != value.device:
            raise ValueError("query, key, and value devices must match")

    def _canonical_mask(
        self,
        mask: torch.Tensor | None,
        *,
        batch_size: int,
        query_length: int,
        key_length: int,
        device: torch.device,
    ) -> torch.Tensor | None:
        if mask is None:
            return None
        if not isinstance(mask, torch.Tensor):
            raise TypeError("attention mask must be a tensor or None")
        if mask.dtype != torch.bool:
            raise TypeError("attention mask must have boolean dtype")
        if mask.device != device:
            raise ValueError("attention mask must be on the query device")
        if mask.ndim == 2:
            if mask.shape[0] != batch_size or mask.shape[1] != key_length:
                raise ValueError("rank-2 mask must have shape [B, Tk]")
            return mask[:, None, None, :]
        if mask.ndim == 3:
            if (
                mask.shape[0] != batch_size
                or mask.shape[1] != query_length
                or mask.shape[2] != key_length
            ):
                raise ValueError("rank-3 mask must have shape [B, Tq, Tk]")
            return mask[:, None, :, :]
        if mask.ndim == 4:
            if (
                mask.shape[0] != batch_size
                or mask.shape[1] not in (1, self.num_heads)
                or mask.shape[2] != query_length
                or mask.shape[3] != key_length
            ):
                raise ValueError("rank-4 mask must have shape [B, 1|H, Tq, Tk]")
            return mask
        raise ValueError("attention mask must have rank 2, 3, or 4")

    @staticmethod
    def _validate_operator_output(output: torch.Tensor, query: torch.Tensor) -> None:
        if not isinstance(output, torch.Tensor):
            raise TypeError("attention operator must return a tensor")
        if output.shape != query.shape:
            raise ValueError("attention operator returned an invalid shape")
        if output.dtype != query.dtype:
            raise TypeError("attention operator changed the projected dtype")
        if output.device != query.device:
            raise ValueError("attention operator changed the projected device")
