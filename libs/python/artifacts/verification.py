# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Bounded verification of artifact bytes against canonical references."""

from __future__ import annotations

import hashlib
from collections.abc import Callable, Iterable
from typing import Final

from libs.python.errors import (
    Code,
    FailedPrecondition,
    InvalidArgument,
    MindcladeError,
    ResourceExhausted,
)
from libs.python.identifiers import ArtifactRef, Digest

MAXIMUM_CHUNKS: Final = 1 << 20


def verify_chunks(
    reference: ArtifactRef,
    chunks: Iterable[bytes],
    *,
    cancelled: Callable[[], bool] | None = None,
    maximum_chunks: int = MAXIMUM_CHUNKS,
) -> int:
    """Consume and verify a bounded byte stream, returning bytes consumed."""
    if not isinstance(reference, ArtifactRef):
        raise InvalidArgument("verification requires an ArtifactRef", reason="artifact_verify_ref")
    try:
        iterator = iter(chunks)
    except TypeError as error:
        raise InvalidArgument(
            "artifact chunks must be iterable bytes",
            reason="artifact_verify_chunks",
            cause=error,
        ) from error
    if cancelled is not None and not callable(cancelled):
        raise InvalidArgument(
            "artifact cancellation predicate must be callable",
            reason="artifact_verify_cancellation",
        )
    if (
        isinstance(maximum_chunks, bool)
        or not isinstance(maximum_chunks, int)
        or maximum_chunks <= 0
        or maximum_chunks > MAXIMUM_CHUNKS
    ):
        raise InvalidArgument(
            f"maximum_chunks must be in [1, {MAXIMUM_CHUNKS}]",
            reason="artifact_verify_chunk_bound",
        )

    hasher = hashlib.sha256()
    consumed = 0
    index = 0
    while True:
        if cancelled is not None:
            cancellation_state = cancelled()
            if not isinstance(cancellation_state, bool):
                raise InvalidArgument(
                    "artifact cancellation predicate must return a boolean",
                    reason="artifact_verify_cancellation",
                )
            if cancellation_state:
                raise MindcladeError(
                    Code.CANCELED,
                    "artifact verification canceled",
                    reason="artifact_verify_canceled",
                )
        try:
            chunk = next(iterator)
        except StopIteration:
            break
        index += 1
        if index > maximum_chunks:
            raise ResourceExhausted(
                "artifact stream exceeded the chunk bound",
                reason="artifact_verify_chunk_count",
            )
        if not isinstance(chunk, bytes):
            raise InvalidArgument(
                "artifact chunks must be bytes",
                reason="artifact_verify_chunk_type",
            )
        consumed += len(chunk)
        if consumed > reference.size_bytes:
            raise FailedPrecondition(
                "artifact stream exceeds its declared size",
                reason="artifact_verify_size",
            )
        hasher.update(chunk)

    if consumed != reference.size_bytes:
        raise FailedPrecondition(
            "artifact stream does not match its declared size",
            reason="artifact_verify_size",
        )
    actual = Digest(hasher.digest())
    if not actual.equals(reference.digest):
        raise FailedPrecondition(
            "artifact stream does not match its declared digest",
            reason="artifact_verify_digest",
        )
    return consumed


def verify_bytes(reference: ArtifactRef, content: bytes) -> None:
    """Verify one in-memory byte string."""
    if not isinstance(content, bytes):
        raise InvalidArgument(
            "artifact content must be bytes",
            reason="artifact_verify_chunk_type",
        )
    verify_chunks(reference, (content,))
