# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

from __future__ import annotations

from pathlib import Path

import pytest

from ci.nightly.pipeline import NightlyContract, load_contract


def test_committed_contract_is_full_graph() -> None:
    contract = load_contract(Path(__file__).with_name("targets.yaml"))
    assert contract.mode == "full"
    assert contract.analysis_targets == ("//...",)
    assert contract.test_targets == ("//...",)


def test_contract_rejects_unknown_fields() -> None:
    with pytest.raises(ValueError, match="unknown"):
        NightlyContract.from_dict(
            {
                "schema_version": 1,
                "mode": "full",
                "analysis_targets": ["//..."],
                "test_targets": ["//..."],
                "unexpected": True,
            }
        )


@pytest.mark.parametrize("mode", ["affected", "", None, True])
def test_contract_rejects_non_full_mode(mode: object) -> None:
    with pytest.raises(ValueError, match="mode"):
        NightlyContract.from_dict(
            {
                "schema_version": 1,
                "mode": mode,
                "analysis_targets": ["//..."],
                "test_targets": ["//..."],
            }
        )
