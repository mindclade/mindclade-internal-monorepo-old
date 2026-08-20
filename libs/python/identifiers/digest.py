# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Content digests in the one canonical form the platform addresses bytes by.

ADR-0004 makes every dataset, reference database, checkpoint, model bundle and
evidence record addressable by digest. That guarantee is only worth anything if
every producer spells a digest the same way, which before this module they did
not: nine call sites built ``"sha256:" + hashlib.sha256(...).hexdigest()`` by
hand and three mutually incompatible predicates decided whether a string was a
digest at all — a strict regex, ``startswith`` plus ``len == 71``, and
``startswith`` alone. The length form accepts uppercase hex the regex rejects, so
two validators in the same tree disagreed about the same string.

Parsing rejects uppercase hex for the reason ``libs/go/identifiers/id.go`` gives
about IDs: databases, signatures and cache keys need one byte-for-byte
representation, and accepting two spellings of one digest means a content-address
that is not actually a single address.

Unlike Go's ``identifiers.Digest``, this type carries no presence bit. Go needs
one so a zero value can mean "absent" without making an all-zero digest
unrepresentable; Python spells absence ``Digest | None``, so the type is always a
real digest and the ambiguity does not arise.
"""

from __future__ import annotations

import hashlib
from dataclasses import dataclass
from hmac import compare_digest
from typing import IO, Final

from libs.python.errors import InvalidArgument

DIGEST_ALGORITHM: Final = "sha256"
DIGEST_BINARY_SIZE: Final = 32
DIGEST_HEX_LENGTH: Final = DIGEST_BINARY_SIZE * 2
DIGEST_PREFIX: Final = DIGEST_ALGORITHM + ":"
DIGEST_TEXT_LENGTH: Final = len(DIGEST_PREFIX) + DIGEST_HEX_LENGTH

# Streaming chunk for of_reader. Large enough that the syscall overhead disappears
# against a multi-gigabyte checkpoint, small enough not to matter for a manifest.
_READ_CHUNK: Final = 1 << 20

_LOWER_HEX: Final = frozenset("0123456789abcdef")


def is_canonical_digest(value: object) -> bool:
    """Report whether ``value`` is a canonical digest, without constructing one.

    The single predicate that replaces the three that disagreed. Checks the
    prefix, the exact length, *and* that every payload character is lowercase
    hexadecimal — the part the length-based checks omitted.
    """
    if not isinstance(value, str):
        return False
    if len(value) != DIGEST_TEXT_LENGTH or not value.startswith(DIGEST_PREFIX):
        return False
    return all(character in _LOWER_HEX for character in value[len(DIGEST_PREFIX) :])


@dataclass(frozen=True, slots=True)
class Digest:
    """A SHA-256 content digest. Canonical text form ``sha256:<64 lowercase hex>``."""

    raw: bytes

    def __post_init__(self) -> None:
        if not isinstance(self.raw, bytes):
            raise InvalidArgument(
                "digest payload must be bytes",
                reason="digest_type",
            )
        if len(self.raw) != DIGEST_BINARY_SIZE:
            raise InvalidArgument(
                f"digest must be {DIGEST_BINARY_SIZE} bytes, got {len(self.raw)}",
                reason="digest_length",
            )

    @classmethod
    def of(cls, data: bytes) -> Digest:
        """Digest ``data``."""
        if not isinstance(data, bytes):
            raise InvalidArgument("digest input must be bytes", reason="digest_input_type")
        return cls(hashlib.sha256(data).digest())

    @classmethod
    def of_text(cls, text: str) -> Digest:
        """Digest ``text``'s UTF-8 bytes.

        UTF-8 rather than the platform encoding, so the digest of a string is the
        same on every host and agrees with Go and Rust.
        """
        return cls.of(text.encode("utf-8"))

    @classmethod
    def of_reader(cls, reader: IO[bytes]) -> tuple[Digest, int]:
        """Stream ``reader`` and return its digest with the byte count consumed.

        Streamed rather than read whole: the callers that need this are hashing
        checkpoints and dataset shards, where reading the object into memory to
        hash it is the difference between working and an out-of-memory kill.
        """
        hasher = hashlib.sha256()
        consumed = 0
        while chunk := reader.read(_READ_CHUNK):
            if not isinstance(chunk, bytes):
                raise InvalidArgument(
                    "digest reader must return bytes",
                    reason="digest_reader_type",
                )
            hasher.update(chunk)
            consumed += len(chunk)
        return cls(hasher.digest()), consumed

    @classmethod
    def parse(cls, value: str) -> Digest:
        """Parse the canonical text form, rejecting anything else."""
        if not is_canonical_digest(value):
            raise InvalidArgument(
                f"digest must be {DIGEST_PREFIX}<{DIGEST_HEX_LENGTH} lowercase hex>",
                reason="digest_format",
                fields={"value": value[:128] if isinstance(value, str) else type(value).__name__},
            )
        return cls(bytes.fromhex(value[len(DIGEST_PREFIX) :]))

    @property
    def hex(self) -> str:
        """The payload without the algorithm prefix."""
        return self.raw.hex()

    @property
    def text(self) -> str:
        """The canonical text form."""
        return DIGEST_PREFIX + self.raw.hex()

    def equals(self, other: Digest) -> bool:
        """Compare in constant time.

        Digests gate artifact admission, so a comparison that returns early leaks
        how much of a forged digest was correct.
        """
        return isinstance(other, Digest) and compare_digest(self.raw, other.raw)

    def __str__(self) -> str:
        return self.text
