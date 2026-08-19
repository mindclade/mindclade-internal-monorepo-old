# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Stable, transport-neutral failure classification.

This is the Python seat at a three-language table. The same seventeen strings are
declared by ``libs/go/faults/code.go`` and ``libs/rust/faults/src/code.rs``, and a
fault crossing a process boundary has to mean the same thing on both sides of it.
``tests/integration/cross_language/test_error_codes.py`` asserts the three sets are
equal, so adding a member here without adding it there is a failing build rather
than a silent divergence.

Codes are deliberately broad. Machine-readable detail belongs in a fault's
``reason``, not in an ever-growing taxonomy: a code is something a caller
switches on, and a switch with two hundred arms is not a classification.
"""

from __future__ import annotations

from enum import StrEnum


class Code(StrEnum):
    """A canonical failure classification.

    ``StrEnum`` rather than a bare ``str`` alias so ``Code.NOT_FOUND == "not_found"``
    holds for serialization while the member set stays closed and enumerable.
    """

    UNKNOWN = "unknown"
    CANCELED = "canceled"
    INVALID_ARGUMENT = "invalid_argument"
    DEADLINE_EXCEEDED = "deadline_exceeded"
    NOT_FOUND = "not_found"
    ALREADY_EXISTS = "already_exists"
    PERMISSION_DENIED = "permission_denied"
    UNAUTHENTICATED = "unauthenticated"
    RESOURCE_EXHAUSTED = "resource_exhausted"
    FAILED_PRECONDITION = "failed_precondition"
    CONFLICT = "conflict"
    ABORTED = "aborted"
    OUT_OF_RANGE = "out_of_range"
    NOT_IMPLEMENTED = "not_implemented"
    INTERNAL = "internal"
    UNAVAILABLE = "unavailable"
    DATA_LOSS = "data_loss"


_DEFAULT_MESSAGES: dict[Code, str] = {
    Code.CANCELED: "request canceled",
    Code.INVALID_ARGUMENT: "invalid argument",
    Code.DEADLINE_EXCEEDED: "deadline exceeded",
    Code.NOT_FOUND: "resource not found",
    Code.ALREADY_EXISTS: "resource already exists",
    Code.PERMISSION_DENIED: "permission denied",
    Code.UNAUTHENTICATED: "authentication required",
    Code.RESOURCE_EXHAUSTED: "resource exhausted",
    Code.FAILED_PRECONDITION: "failed precondition",
    Code.CONFLICT: "resource conflict",
    Code.ABORTED: "operation aborted",
    Code.OUT_OF_RANGE: "value out of range",
    Code.NOT_IMPLEMENTED: "operation not implemented",
    Code.INTERNAL: "internal error",
    Code.UNAVAILABLE: "service unavailable",
    Code.DATA_LOSS: "data loss",
}


def is_canonical_code(value: str) -> bool:
    """Report whether ``value`` is already one of the canonical wire strings."""
    return value in Code.__members__.values()


def parse_code(value: str) -> Code:
    """Parse a canonical code, normalizing surrounding space and ASCII case.

    Raises ``ValueError`` on an unrecognized value rather than silently returning
    ``UNKNOWN``. A code arriving off the wire that this build does not know about
    is a version skew worth surfacing; callers that would rather degrade than fail
    have ``normalize_code`` for exactly that.
    """
    candidate = value.strip().lower()
    try:
        return Code(candidate)
    except ValueError:
        raise ValueError(f"errors: invalid code {value!r}") from None


def normalize_code(value: str | Code) -> Code:
    """Return ``value`` when it is canonical and ``Code.UNKNOWN`` otherwise."""
    try:
        return parse_code(str(value))
    except ValueError:
        return Code.UNKNOWN


def default_message(code: str | Code) -> str:
    """Return the client-safe message used when a fault carries none."""
    return _DEFAULT_MESSAGES.get(normalize_code(code), "operation failed")
