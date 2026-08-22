# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Contract, causality, gradient, state, dropout, and export tests for the LLM."""

from __future__ import annotations

import copy
from dataclasses import FrozenInstanceError, replace
from pathlib import Path

import pytest
import torch
from torch import nn

from models.adapters.export import (
    DynamicDimension,
    TensorInputContract,
    export_bundle,
    load_export_bundle,
    validate_export_parity,
)
from models.components.attention import AttentionOperator
from models.families.llm import (
    CausalLMOutput,
    DecoderLayer,
    DecoderOnlyLanguageModel,
    LLMConfig,
)
from models.families.llm.reference.tiny import build_tiny_llm, tiny_llm_config

RTOL = 1e-5
ATOL = 1e-6


def _deterministic_config() -> LLMConfig:
    return replace(tiny_llm_config(), dropout=0.0, attention_dropout=0.0)


def test_config_is_immutable_and_rejects_invalid_or_unbounded_values() -> None:
    config = tiny_llm_config()
    assert config.head_dim == 4
    assert config.estimated_parameter_count < config.maximum_model_parameters
    with pytest.raises(FrozenInstanceError):
        config.hidden_size = 32  # type: ignore[misc]
    with pytest.raises(ValueError, match="divisible"):
        replace(config, hidden_size=18)
    with pytest.raises(ValueError, match="even"):
        replace(config, hidden_size=12, num_heads=4)
    with pytest.raises(ValueError, match=r"\[0, 1\)"):
        replace(config, dropout=1.0)
    with pytest.raises(ValueError, match="greater than one"):
        replace(config, rotary_base=1.0)
    with pytest.raises(TypeError, match="boolean"):
        replace(config, tie_word_embeddings=1)  # type: ignore[arg-type]
    with pytest.raises(ValueError, match="parameter estimate"):
        replace(config, maximum_model_parameters=1)

    token_limited = replace(config, maximum_tokens_per_batch=4)
    with pytest.raises(ValueError, match="maximum_tokens_per_batch"):
        token_limited.validate_input_shape(2, 3)
    attention_limited = replace(config, maximum_attention_elements=63)
    with pytest.raises(ValueError, match="maximum_attention_elements"):
        attention_limited.validate_input_shape(1, 4)


def test_forward_contract_masks_invalid_queries_and_rejects_bad_inputs() -> None:
    model = build_tiny_llm(_deterministic_config()).eval()
    input_ids = torch.tensor([[1, 2, 3, 4], [5, 6, 0, 0]], dtype=torch.int64)
    attention_mask = torch.tensor(
        [[True, True, True, True], [True, True, False, False]],
        dtype=torch.bool,
    )
    with torch.inference_mode():
        output = model(input_ids, attention_mask)
        batch_one = model(input_ids[:1, :1], attention_mask[:1, :1])

    assert isinstance(output, CausalLMOutput)
    assert output.logits.shape == (2, 4, model.config.vocab_size)
    assert output.hidden_states.shape == (2, 4, model.config.hidden_size)
    assert output.logits.dtype == torch.float32
    assert output.logits.device == input_ids.device
    assert batch_one.logits.shape == (1, 1, model.config.vocab_size)
    torch.testing.assert_close(output.logits[1, 2:], torch.zeros_like(output.logits[1, 2:]))
    torch.testing.assert_close(
        output.hidden_states[1, 2:],
        torch.zeros_like(output.hidden_states[1, 2:]),
    )

    with pytest.raises(TypeError, match=r"int32 or torch\.int64"):
        model(input_ids.to(torch.float32), attention_mask)
    with pytest.raises(ValueError, match=r"\[batch, sequence\]"):
        model(input_ids.unsqueeze(0), attention_mask)
    with pytest.raises(ValueError, match="values"):
        model(torch.tensor([[-1, 1]], dtype=torch.int64))
    with pytest.raises(ValueError, match="sequence_length"):
        model(torch.ones(1, 17, dtype=torch.int64))
    with pytest.raises(TypeError, match=r"torch\.bool"):
        model(input_ids, attention_mask.to(torch.int64))
    with pytest.raises(ValueError, match=r"\[batch, sequence\]"):
        model(input_ids, attention_mask[:, :-1])


def test_causal_prefix_is_invariant_to_future_tokens() -> None:
    model = build_tiny_llm(_deterministic_config()).eval()
    original = torch.tensor([[1, 2, 3, 4]], dtype=torch.int64)
    changed_future = torch.tensor([[1, 2, 12, 13]], dtype=torch.int64)
    with torch.inference_mode():
        first = model(original)
        second = model(changed_future)
    torch.testing.assert_close(first.logits[:, :2], second.logits[:, :2], rtol=RTOL, atol=ATOL)
    torch.testing.assert_close(
        first.hidden_states[:, :2],
        second.hidden_states[:, :2],
        rtol=RTOL,
        atol=ATOL,
    )


def test_train_dropout_is_stochastic_and_eval_is_deterministic() -> None:
    config = replace(tiny_llm_config(), dropout=0.5, attention_dropout=0.5)
    model = build_tiny_llm(config)
    input_ids = torch.tensor([[1, 2, 3, 4]], dtype=torch.int64)

    model.train()
    first_train = model(input_ids).logits
    second_train = model(input_ids).logits
    assert not torch.equal(first_train, second_train)

    model.eval()
    with torch.inference_mode():
        first_eval = model(input_ids).logits
        second_eval = model(input_ids).logits
    torch.testing.assert_close(first_eval, second_eval, rtol=0.0, atol=0.0)


def test_initialization_is_deterministic_and_embedding_tying_is_explicit() -> None:
    config = _deterministic_config()
    torch.manual_seed(1)
    first = build_tiny_llm(config)
    _ = torch.randn(100)
    torch.manual_seed(999)
    second = build_tiny_llm(config)
    for name, tensor in first.state_dict().items():
        torch.testing.assert_close(tensor, second.state_dict()[name], rtol=0.0, atol=0.0)

    assert first.lm_head.weight is first.token_embeddings.weight
    assert torch.count_nonzero(first.token_embeddings.weight[config.padding_idx]).item() == 0

    untied = build_tiny_llm(replace(config, tie_word_embeddings=False))
    assert untied.lm_head.weight is not untied.token_embeddings.weight
    assert untied.lm_head.weight.data_ptr() != untied.token_embeddings.weight.data_ptr()


def test_initialization_does_not_advance_the_process_global_rng() -> None:
    torch.manual_seed(313)
    expected = torch.randn(16)

    torch.manual_seed(313)
    _ = build_tiny_llm(_deterministic_config())
    actual = torch.randn(16)

    torch.testing.assert_close(actual, expected, rtol=0.0, atol=0.0)


def test_backward_reaches_every_trainable_parameter_with_finite_gradients() -> None:
    model = build_tiny_llm(_deterministic_config())
    input_ids = torch.tensor([[1, 2, 3], [4, 5, 6]], dtype=torch.int64)
    output = model(input_ids)
    (output.logits.square().mean() + output.hidden_states.square().mean()).backward()

    gradients = {name: parameter.grad for name, parameter in model.named_parameters()}
    assert gradients
    assert all(gradient is not None for gradient in gradients.values())
    assert all(
        torch.isfinite(gradient).all() for gradient in gradients.values() if gradient is not None
    )


def test_strict_state_dict_round_trip_preserves_eval_outputs_and_tying() -> None:
    config = _deterministic_config()
    original = build_tiny_llm(config).eval()
    input_ids = torch.tensor([[1, 2, 3, 4]], dtype=torch.int64)
    attention_mask = torch.ones_like(input_ids, dtype=torch.bool)
    with torch.inference_mode():
        expected = original(input_ids, attention_mask)

    restored = build_tiny_llm(config).eval()
    incompatible = restored.load_state_dict(copy.deepcopy(original.state_dict()), strict=True)
    assert incompatible.missing_keys == []
    assert incompatible.unexpected_keys == []
    assert restored.lm_head.weight is restored.token_embeddings.weight
    assert restored.lm_head.weight.data_ptr() != original.lm_head.weight.data_ptr()
    assert any("inverse_frequencies" in key for key in restored.state_dict())
    with torch.inference_mode():
        actual = restored(input_ids, attention_mask)
    torch.testing.assert_close(actual.logits, expected.logits, rtol=RTOL, atol=ATOL)
    torch.testing.assert_close(
        actual.hidden_states,
        expected.hidden_states,
        rtol=RTOL,
        atol=ATOL,
    )


class _IdentityAttentionOperator(AttentionOperator):
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
        del key, value, mask, causal, scale, dropout_p
        return query


def test_attention_operator_factory_requires_fresh_contract_instances() -> None:
    config = _deterministic_config()
    model = DecoderOnlyLanguageModel(config, attention_operator_factory=_IdentityAttentionOperator)
    for layer in model.layers.children():
        assert isinstance(layer, DecoderLayer)
        assert isinstance(layer.attention.attention.operator.delegate, _IdentityAttentionOperator)

    shared = _IdentityAttentionOperator()
    with pytest.raises(ValueError, match="fresh module"):
        DecoderOnlyLanguageModel(config, attention_operator_factory=lambda: shared)
    with pytest.raises(TypeError, match="AttentionOperator"):
        DecoderOnlyLanguageModel(config, attention_operator_factory=nn.Identity)  # type: ignore[arg-type]


class _LogitsForExport(nn.Module):
    def __init__(self, model: DecoderOnlyLanguageModel) -> None:
        super().__init__()
        self.model = model

    def forward(self, input_ids: torch.Tensor, attention_mask: torch.Tensor) -> torch.Tensor:
        return self.model.forward(input_ids, attention_mask).logits


def test_bounded_dynamic_export_adapter_round_trip_and_fresh_model_parity(
    tmp_path: Path,
) -> None:
    config = _deterministic_config()
    model = _LogitsForExport(build_tiny_llm(config)).eval()
    input_ids = torch.tensor([[1, 2, 3], [4, 5, 0]], dtype=torch.int64)
    attention_mask = torch.tensor(
        [[True, True, True], [True, True, False]],
        dtype=torch.bool,
    )
    batch = DynamicDimension(axis=0, name="batch", minimum=1, maximum=4)
    sequence = DynamicDimension(axis=1, name="sequence", minimum=1, maximum=8)
    contracts = (
        TensorInputContract.from_tensor(
            "input_ids",
            input_ids,
            dynamic_dimensions=(batch, sequence),
        ),
        TensorInputContract.from_tensor(
            "attention_mask",
            attention_mask,
            dynamic_dimensions=(batch, sequence),
        ),
    )
    configuration_sha256 = "sha256:" + "1" * 64
    source_sha256 = "sha256:" + "2" * 64
    runtime_sha256 = "sha256:" + "3" * 64
    kernel_manifest_sha256 = "sha256:" + "4" * 64
    manifest = export_bundle(
        model,
        (input_ids, attention_mask),
        contracts,
        tmp_path / "tiny-llm-export",
        configuration_sha256=configuration_sha256,
        source_sha256=source_sha256,
        runtime_sha256=runtime_sha256,
        kernel_manifest_sha256=kernel_manifest_sha256,
    )
    loaded = load_export_bundle(
        tmp_path / "tiny-llm-export",
        expected_manifest_sha256=manifest.sha256,
        expected_runtime_sha256=runtime_sha256,
        expected_kernel_manifest_sha256=kernel_manifest_sha256,
    )
    minimum_ids = torch.tensor([[7]], dtype=torch.int64)
    minimum_mask = torch.ones_like(minimum_ids, dtype=torch.bool)
    maximum_ids = torch.arange(32, dtype=torch.int64).reshape(4, 8)
    maximum_mask = torch.ones_like(maximum_ids, dtype=torch.bool)
    report = validate_export_parity(
        loaded,
        lambda: _LogitsForExport(build_tiny_llm(config)),
        (
            (minimum_ids, minimum_mask),
            (maximum_ids, maximum_mask),
        ),
        rtol=RTOL,
        atol=ATOL,
    )
    assert report.case_shapes == (
        ((1, 1), (1, 1)),
        ((4, 8), (4, 8)),
    )
