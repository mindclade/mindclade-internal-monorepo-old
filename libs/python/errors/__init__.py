# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Transport-neutral structured faults for Mindclade Python packages.

Layer 0 of `libs/python`: standard library only, and importable from every other
package here. It owns classification and retry intent, and nothing else — no
transport, no logging, no telemetry, no persistence, no PyTorch.
"""

from .base import (
    MAXIMUM_FIELD_KEY_LENGTH,
    MAXIMUM_FIELD_VALUE_LENGTH,
    MAXIMUM_FIELDS,
    MAXIMUM_MESSAGE_LENGTH,
    MAXIMUM_OPERATION_LENGTH,
    MAXIMUM_REASON_LENGTH,
    Canceled,
    DeadlineExceeded,
    FailedPrecondition,
    InvalidArgument,
    MindcladeError,
    OutOfRange,
    ResourceExhausted,
    code_of,
    fields_of,
    is_retryable,
    operation_of,
    public_message_of,
    reason_of,
    retry_of,
    wrap,
)
from .codes import Code, default_message, is_canonical_code, normalize_code, parse_code
from .retry import (
    RetryKind,
    RetryPolicy,
    backoff_retry,
    delayed_retry,
    immediate_retry,
    no_retry,
)

__all__ = [
    "MAXIMUM_FIELDS",
    "MAXIMUM_FIELD_KEY_LENGTH",
    "MAXIMUM_FIELD_VALUE_LENGTH",
    "MAXIMUM_MESSAGE_LENGTH",
    "MAXIMUM_OPERATION_LENGTH",
    "MAXIMUM_REASON_LENGTH",
    "Canceled",
    "Code",
    "DeadlineExceeded",
    "FailedPrecondition",
    "InvalidArgument",
    "MindcladeError",
    "OutOfRange",
    "ResourceExhausted",
    "RetryKind",
    "RetryPolicy",
    "backoff_retry",
    "code_of",
    "default_message",
    "delayed_retry",
    "fields_of",
    "immediate_retry",
    "is_canonical_code",
    "is_retryable",
    "no_retry",
    "normalize_code",
    "operation_of",
    "parse_code",
    "public_message_of",
    "reason_of",
    "retry_of",
    "wrap",
]
