# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Deterministic retry decisions; durable scheduling remains in Go."""

from __future__ import annotations

from dataclasses import dataclass

from libs.python.errors import Code, code_of, is_retryable

from .config import BatchLimits


@dataclass(frozen=True, slots=True)
class RetryDecision:
    retry: bool
    delay_millis: int
    code: Code


def classify(error: BaseException, attempt: int, limits: BatchLimits) -> RetryDecision:
    if isinstance(attempt, bool) or attempt < 1:
        raise ValueError("retry attempt must be a positive integer")
    code = code_of(error)
    if attempt >= limits.maximum_attempts or not is_retryable(error):
        return RetryDecision(False, 0, code)
    exponent = min(attempt - 1, 30)
    delay = min(limits.maximum_retry_delay_millis, limits.base_retry_delay_millis * (2**exponent))
    return RetryDecision(True, delay, code)
