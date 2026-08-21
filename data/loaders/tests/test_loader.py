# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

from __future__ import annotations

import pytest
import torch

from data.loaders import LoaderConfig, SampleDataset, build_loader
from data.loaders.diagnostics import audit_coverage
from data.sample import Sample

DIGEST = "sha256:" + "a" * 64


def samples(count: int = 5) -> tuple[Sample, ...]:
    return tuple(
        Sample(
            f"sample-{index}",
            {"tokens": torch.tensor([index, index + 1], dtype=torch.int64)},
            DIGEST,
            group_id=f"group-{index}",
            split="train",
            label=index % 2,
        )
        for index in range(count)
    )


def test_zero_worker_loader_has_explicit_batch_contract_and_complete_coverage() -> None:
    values = samples()
    dataset = SampleDataset(values)
    wrapped = build_loader(dataset, LoaderConfig(2, 17, shuffle=False, num_workers=0))
    batches = tuple(wrapped.loader)
    assert [batch.size for batch in batches] == [2, 2, 1]
    assert batches[0].features["tokens"].shape == (2, 2)
    assert batches[0].features["tokens"].device.type == "cpu"
    assert audit_coverage((item.sample_id for item in values), batches).complete


def test_shuffle_is_seeded_and_invalid_indices_fail_clearly() -> None:
    dataset = SampleDataset(samples())
    config = LoaderConfig(2, 29, shuffle=True)
    first = [
        sample_id
        for batch in build_loader(dataset, config).loader
        for sample_id in batch.sample_ids
    ]
    second = [
        sample_id
        for batch in build_loader(dataset, config).loader
        for sample_id in batch.sample_ids
    ]
    assert first == second
    with pytest.raises(IndexError, match="addressable"):
        dataset[-1]


def test_loader_configuration_rejects_accidental_worker_options() -> None:
    with pytest.raises(ValueError, match="persistent"):
        LoaderConfig(2, 1, persistent_workers=True)
    with pytest.raises(ValueError, match="prefetch"):
        LoaderConfig(2, 1, prefetch_factor=2)
