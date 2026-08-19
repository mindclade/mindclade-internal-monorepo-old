# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Retry *intent*, carried alongside a fault.

This module communicates whether and how a caller may try again. It does not
retry anything, does not sleep, and does not prescribe a backoff curve — the same
split ``libs/go/faults`` and ``libs/go/retry`` keep, and for the same reason: the
side that knows why an operation failed is rarely the side that should own the
schedule for trying it again.

Durations are integer milliseconds rather than ``timedelta``. The value crosses a
process boundary into Go's ``time.Duration`` and Rust's ``Duration``, and a
float-backed ``timedelta`` round-trips through JSON with a precision the other two
languages do not share.
"""

from __future__ import annotations

from dataclasses import dataclass, replace
from enum import StrEnum


class RetryKind(StrEnum):
    """How a caller may retry.

    ``UNSPECIFIED`` is the empty string, matching Go's zero value, so a fault that
    never expressed retry intent and one that expressed "no opinion" serialize
    identically instead of becoming two states a consumer has to tell apart.
    """

    UNSPECIFIED = ""
    NEVER = "never"
    IMMEDIATE = "immediate"
    BACKOFF = "with_backoff"
    AFTER = "after"


@dataclass(frozen=True, slots=True)
class RetryPolicy:
    """Retry intent communicated to a caller.

    ``max_attempts`` counts the initial attempt, so ``1`` means "do not try again"
    and ``0`` delegates the limit to the caller's own policy.
    """

    kind: RetryKind = RetryKind.UNSPECIFIED
    after_millis: int = 0
    max_attempts: int = 0

    def normalized(self) -> RetryPolicy:
        """Return the canonical form of this policy.

        Every field combination that cannot mean anything is collapsed onto one
        that can: a delay on a kind that has no delay is dropped, and ``AFTER``
        with a non-positive delay becomes ``IMMEDIATE`` — it already behaves that
        way, and saying so makes two equivalent policies compare equal.
        """
        max_attempts = max(self.max_attempts, 0)

        match self.kind:
            case RetryKind.UNSPECIFIED | RetryKind.NEVER:
                return replace(self, after_millis=0, max_attempts=0)
            case RetryKind.IMMEDIATE | RetryKind.BACKOFF:
                return replace(self, after_millis=0, max_attempts=max_attempts)
            case RetryKind.AFTER:
                if self.after_millis <= 0:
                    return replace(
                        self,
                        kind=RetryKind.IMMEDIATE,
                        after_millis=0,
                        max_attempts=max_attempts,
                    )
                return replace(self, max_attempts=max_attempts)
            case _:
                return RetryPolicy()

    def valid(self) -> bool:
        """Report whether this policy is already canonical."""
        return self == self.normalized()

    def specified(self) -> bool:
        """Report whether the policy expresses retry intent at all."""
        return self.normalized().kind is not RetryKind.UNSPECIFIED

    def retryable(self) -> bool:
        """Report whether the normalized policy permits another attempt."""
        normalized = self.normalized()
        if normalized.max_attempts == 1:
            return False
        return normalized.kind in {
            RetryKind.IMMEDIATE,
            RetryKind.BACKOFF,
            RetryKind.AFTER,
        }


def no_retry() -> RetryPolicy:
    """An explicit policy forbidding another attempt."""
    return RetryPolicy(kind=RetryKind.NEVER)


def immediate_retry(max_attempts: int = 0) -> RetryPolicy:
    """Permit an immediate retry."""
    return RetryPolicy(kind=RetryKind.IMMEDIATE, max_attempts=max_attempts).normalized()


def backoff_retry(max_attempts: int = 0) -> RetryPolicy:
    """Permit retrying under a caller-selected backoff algorithm."""
    return RetryPolicy(kind=RetryKind.BACKOFF, max_attempts=max_attempts).normalized()


def delayed_retry(after_millis: int, max_attempts: int = 0) -> RetryPolicy:
    """Permit retrying after at least ``after_millis`` has elapsed."""
    return RetryPolicy(
        kind=RetryKind.AFTER, after_millis=after_millis, max_attempts=max_attempts
    ).normalized()
