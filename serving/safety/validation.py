# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Startup validation for policy/screener composition."""

from __future__ import annotations

from .policy import SafetyPolicy
from .screening import Screener


def validate_composition(policy: SafetyPolicy, screeners: tuple[Screener, ...]) -> None:
    policy.validate()
    names = tuple(screener.name for screener in screeners)
    if any(not name or len(name) > 128 for name in names):
        raise ValueError("screener name is invalid")
    if len(names) != len(set(names)):
        raise ValueError("screener names must be unique")
    missing = sorted(set(policy.required_screeners) - set(names))
    if missing and not policy.fail_closed:
        raise ValueError(f"non-fail-closed policy is missing required screeners: {missing}")
