# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

import pytest

from libs.python.errors import (
    RetryKind,
    RetryPolicy,
    backoff_retry,
    delayed_retry,
    immediate_retry,
    no_retry,
)


def test_unspecified_is_the_empty_string_so_it_matches_go_zero_value() -> None:
    assert RetryKind.UNSPECIFIED.value == ""
    assert RetryPolicy().kind is RetryKind.UNSPECIFIED


@pytest.mark.parametrize("kind", [RetryKind.UNSPECIFIED, RetryKind.NEVER])
def test_non_retrying_kinds_drop_delay_and_attempt_count(kind: RetryKind) -> None:
    normalized = RetryPolicy(kind=kind, after_millis=900, max_attempts=5).normalized()
    assert normalized == RetryPolicy(kind=kind)


def test_after_with_a_non_positive_delay_collapses_to_immediate() -> None:
    # It already behaves as an immediate retry; saying so makes two policies that mean
    # the same thing compare equal instead of only behaving alike.
    assert delayed_retry(0, 3) == RetryPolicy(kind=RetryKind.IMMEDIATE, max_attempts=3)
    assert delayed_retry(-1) == RetryPolicy(kind=RetryKind.IMMEDIATE)


def test_backoff_and_immediate_carry_no_delay() -> None:
    assert backoff_retry(4).after_millis == 0
    assert RetryPolicy(kind=RetryKind.IMMEDIATE, after_millis=50).normalized().after_millis == 0


def test_negative_attempts_are_clamped_not_preserved() -> None:
    assert immediate_retry(-3).max_attempts == 0


def test_max_attempts_of_one_means_the_initial_attempt_only() -> None:
    assert not immediate_retry(1).retryable()
    assert backoff_retry(2).retryable()


def test_retryable_and_specified_agree_with_the_kind() -> None:
    assert not no_retry().retryable()
    assert no_retry().specified()
    assert not RetryPolicy().specified()
    assert delayed_retry(250).retryable()


def test_normalized_is_idempotent_and_valid_reports_canonical_form() -> None:
    raw = RetryPolicy(kind=RetryKind.AFTER, after_millis=-5, max_attempts=-1)
    once = raw.normalized()
    assert not raw.valid()
    assert once.valid()
    assert once.normalized() == once


def test_policy_is_immutable() -> None:
    with pytest.raises(AttributeError):
        no_retry().kind = RetryKind.IMMEDIATE  # type: ignore[misc]
