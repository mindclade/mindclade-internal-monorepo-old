# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Validation and construction tests for the reference optimizer factory."""

import pytest
import torch
from torch import nn

from training.optim import AdamWConfig, SGDConfig, build_optimizer


def parameter() -> nn.Parameter:
    return nn.Parameter(torch.tensor([1.0], dtype=torch.float32))


def test_builds_explicit_non_foreach_sgd() -> None:
    value = parameter()

    optimizer = build_optimizer(
        [value],
        SGDConfig(learning_rate=0.25, momentum=0.5, weight_decay=0.1),
    )

    assert isinstance(optimizer, torch.optim.SGD)
    group = optimizer.param_groups[0]
    assert group["lr"] == 0.25
    assert group["momentum"] == 0.5
    assert group["weight_decay"] == 0.1
    assert group["foreach"] is False


def test_builds_explicit_non_fused_adamw() -> None:
    value = parameter()

    optimizer = build_optimizer(
        (item for item in [value]),
        AdamWConfig(
            learning_rate=0.01,
            betas=(0.8, 0.9),
            epsilon=1e-6,
            weight_decay=0.2,
        ),
    )

    assert isinstance(optimizer, torch.optim.AdamW)
    group = optimizer.param_groups[0]
    assert group["lr"] == 0.01
    assert group["betas"] == (0.8, 0.9)
    assert group["eps"] == 1e-6
    assert group["weight_decay"] == 0.2
    assert group["foreach"] is False
    assert group["fused"] is False


@pytest.mark.parametrize(
    "config",
    [
        lambda: SGDConfig(learning_rate=0.0),
        lambda: SGDConfig(learning_rate=float("nan")),
        lambda: SGDConfig(learning_rate=0.1, momentum=1.0),
        lambda: SGDConfig(learning_rate=0.1, weight_decay=-0.1),
        lambda: AdamWConfig(learning_rate=0.1, betas=(0.9, 1.0)),
        lambda: AdamWConfig(learning_rate=0.1, epsilon=0.0),
        lambda: AdamWConfig(learning_rate=True),
    ],
)
def test_optimizer_config_rejects_invalid_values(config) -> None:
    with pytest.raises(ValueError):
        config()


@pytest.mark.parametrize(
    ("parameters", "message"),
    [
        ([], "at least one"),
        ([parameter(), torch.tensor([1.0])], "nn.Parameter"),
        ([nn.Parameter(torch.ones(1, dtype=torch.float64))], "CPU or CUDA float32"),
    ],
)
def test_factory_rejects_invalid_parameter_collections(parameters, message: str) -> None:
    with pytest.raises(ValueError, match=message):
        build_optimizer(parameters, SGDConfig(learning_rate=0.1))


def test_factory_rejects_duplicate_and_frozen_parameters() -> None:
    duplicate = parameter()
    frozen = parameter()
    frozen.requires_grad_(False)

    with pytest.raises(ValueError, match="duplicates"):
        build_optimizer([duplicate, duplicate], SGDConfig(learning_rate=0.1))
    with pytest.raises(ValueError, match="frozen"):
        build_optimizer([frozen], SGDConfig(learning_rate=0.1))


def test_built_optimizer_performs_expected_sgd_update() -> None:
    value = parameter()
    optimizer = build_optimizer([value], SGDConfig(learning_rate=0.25))
    value.grad = torch.tensor([2.0])

    optimizer.step()

    torch.testing.assert_close(value.detach(), torch.tensor([0.5]), rtol=0.0, atol=0.0)


@pytest.mark.skipif(not torch.cuda.is_available(), reason="CUDA is unavailable")
@pytest.mark.parametrize(
    "config",
    [SGDConfig(learning_rate=0.1), AdamWConfig(learning_rate=0.01)],
)
def test_factory_builds_explicit_cuda_float32_optimizer(config) -> None:
    value = nn.Parameter(torch.tensor([1.0], dtype=torch.float32, device="cuda"))

    optimizer = build_optimizer([value], config)
    value.grad = torch.tensor([2.0], dtype=torch.float32, device="cuda")
    optimizer.step()

    assert value.device.type == "cuda"
    assert torch.isfinite(value).all()
