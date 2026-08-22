# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Bounded orchestration; the CLI owns process isolation and GPU assignment."""

from __future__ import annotations

import hashlib
from collections.abc import Callable, Sequence

from kernels.tilelang.autotune.budget import TuningBudget
from kernels.tilelang.autotune.candidate import Candidate
from kernels.tilelang.autotune.database import TuningResults
from kernels.tilelang.autotune.objective import LatencyDistribution
from kernels.tilelang.autotune.validation import CandidateResult, CandidateStatus

ExecuteCandidate = Callable[[Candidate, TuningBudget], tuple[bool, str, Sequence[float]]]


def run_candidates(
    candidates: Sequence[Candidate],
    *,
    budget: TuningBudget,
    execute: ExecuteCandidate,
) -> TuningResults:
    if not candidates:
        raise ValueError("at least one candidate is required")
    selected = tuple(candidates[: budget.max_candidates])
    database = TuningResults(selected[0].environment_digest, selected[0].source_digest)
    for candidate in selected:
        if (
            candidate.environment_digest != database.environment_digest
            or candidate.source_digest != database.source_digest
        ):
            raise ValueError("a tuning run cannot mix source or environment identities")
        try:
            parity_passed, source_digest, samples = execute(candidate, budget)
            if not parity_passed:
                database.add(CandidateResult(candidate.digest, CandidateStatus.PARITY_FAILED))
                continue
            latency = LatencyDistribution(tuple(float(value) for value in samples))
            database.add(
                CandidateResult(
                    candidate.digest,
                    CandidateStatus.PASSED,
                    latency=latency,
                    generated_source_digest=source_digest,
                )
            )
        except TimeoutError as exc:
            database.add(
                CandidateResult(
                    candidate.digest,
                    CandidateStatus.TIMED_OUT,
                    failure_digest=hashlib.sha256(str(exc).encode()).hexdigest(),
                )
            )
        except Exception as exc:
            database.add(
                CandidateResult(
                    candidate.digest,
                    CandidateStatus.BENCHMARK_FAILED,
                    failure_digest=hashlib.sha256(str(exc).encode()).hexdigest(),
                )
            )
    return database
