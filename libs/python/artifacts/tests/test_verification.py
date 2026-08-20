# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

from __future__ import annotations

from collections.abc import Iterable

import pytest

from libs.python.artifacts import (
    VerifiedArtifactClient,
    reference_bytes,
    verify_bytes,
    verify_chunks,
)
from libs.python.errors import Code, MindcladeError, code_of
from libs.python.identifiers import ArtifactRef


def reference(content: bytes = b"content") -> ArtifactRef:
    return reference_bytes(
        content,
        media_type="application/octet-stream",
        logical_kind="dataset",
    )


def test_verification_accepts_chunking_without_changing_identity() -> None:
    ref = reference()
    assert verify_chunks(ref, (b"con", b"tent")) == len(b"content")
    verify_bytes(ref, b"content")


@pytest.mark.parametrize("content", [b"short", b"content-too-long", b"contXnt"])
def test_verification_rejects_size_or_digest_mismatches(content: bytes) -> None:
    with pytest.raises(MindcladeError):
        verify_bytes(reference(), content)


def test_verification_honors_cancellation_without_consuming_more_chunks() -> None:
    consumed = 0

    def chunks() -> Iterable[bytes]:
        nonlocal consumed
        consumed += 1
        yield b"content"

    with pytest.raises(MindcladeError) as caught:
        verify_chunks(reference(), chunks(), cancelled=lambda: True)
    assert code_of(caught.value) is Code.CANCELED
    assert consumed == 1


def test_verified_client_uses_an_injected_reader_and_enforces_memory_bound() -> None:
    class Reader:
        def read(self, _reference: ArtifactRef) -> Iterable[bytes]:
            return (b"con", b"tent")

    assert VerifiedArtifactClient(Reader()).read(reference()) == b"content"
    with pytest.raises(MindcladeError, match="client bound"):
        VerifiedArtifactClient(Reader(), maximum_bytes=2).read(reference())
