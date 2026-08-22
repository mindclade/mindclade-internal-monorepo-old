# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

import hashlib
import json
from collections.abc import Mapping

from kernels.tilelang.autotune import Candidate, TuningBudget, bounded_candidates, run_candidates
from kernels.tilelang.autotune.objective import LatencyDistribution, stable_winner
from kernels.tilelang.autotune.validation import CandidateStatus

_SOURCE = hashlib.sha256(b"source").hexdigest()
_ENVIRONMENT = hashlib.sha256(b"environment").hexdigest()
_GENERATED = hashlib.sha256(b"generated").hexdigest()


def test_search_space_is_bounded_and_filters_illegal_resource_combinations() -> None:
    def legality(config: Mapping[str, int | float | str | bool]) -> str | None:
        block = config["block"]
        stages = config["stages"]
        assert isinstance(block, int)
        assert isinstance(stages, int)
        return "resource" if block * stages > 192 else None

    candidates = bounded_candidates(
        {"block": [32, 64, 128], "stages": [1, 2, 3]},
        legality=legality,
        source_digest=_SOURCE,
        environment_digest=_ENVIRONMENT,
        maximum=4,
    )
    assert len(candidates) == 4
    for candidate in candidates:
        block = candidate.config["block"]
        stages = candidate.config["stages"]
        assert isinstance(block, int)
        assert isinstance(stages, int)
        assert block * stages <= 192
    assert len({candidate.digest for candidate in candidates}) == 4


def test_runner_gates_latency_on_parity_and_serializes_results() -> None:
    candidates = bounded_candidates(
        {"block": [32, 64]},
        legality=lambda _config: None,
        source_digest=_SOURCE,
        environment_digest=_ENVIRONMENT,
        maximum=2,
    )

    def execute(candidate: Candidate, _budget: TuningBudget) -> tuple[bool, str, list[float]]:
        block = candidate.config["block"]
        assert isinstance(block, int)
        passed = block == 64
        return passed, _GENERATED, [1.0, 0.9, 1.1, 1.0, 1.0]

    results = run_candidates(candidates, budget=TuningBudget(max_candidates=2), execute=execute)
    statuses = {result.status for result in results.results.values()}
    assert statuses == {CandidateStatus.PARITY_FAILED, CandidateStatus.PASSED}
    payload = json.loads(results.to_json())
    assert payload["schema_version"] == 1
    assert len(payload["results"]) == 2


def test_winner_uses_median_and_rejects_unstable_samples() -> None:
    winner = stable_winner(
        {
            "stable": LatencyDistribution((1.0, 1.0, 1.01, 0.99, 1.0)),
            "noisy": LatencyDistribution((0.1, 2.0, 1.0, 3.0, 0.2)),
        }
    )
    assert winner == "stable"
