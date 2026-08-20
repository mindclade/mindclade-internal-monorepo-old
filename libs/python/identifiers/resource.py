# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Canonical Mindclade resource identifiers.

A resource ID is ``<kind>_<32 lowercase hexadecimal UUIDv7 characters>`` — the
same shape ``libs/go/identifiers/id.go`` parses and the cross-language fixture
``tests/integration/cross_language/fixtures/primitives_v1.json`` carries. Because
the payload is a UUIDv7, IDs of one kind sort lexicographically by creation time,
which is what lets a database range-scan them and a log stay readable.

This module owns the *format*. Which kinds exist is a domain decision and stays
with the packages that mint them, exactly as Go splits ``Kind`` validation from
the domain's list of valid kinds.

The UUIDv7 generator is written out here because ``uuid.uuid7`` does not exist in
the Python this repository pins (3.12; it arrives in 3.14). It follows RFC 9562's
method 2 for intra-millisecond monotonicity: the 12-bit ``rand_a`` field is used
as a counter within a millisecond rather than as noise, so two IDs minted in the
same millisecond still order by creation.
"""

from __future__ import annotations

import re
import secrets
import threading
import time
from collections.abc import Callable
from dataclasses import dataclass
from typing import Final

from libs.python.errors import InvalidArgument, ResourceExhausted

MINIMUM_KIND_LENGTH: Final = 2
MAXIMUM_KIND_LENGTH: Final = 24
UUID_COMPACT_LENGTH: Final = 32
UUID_BINARY_SIZE: Final = 16
ID_SEPARATOR: Final = "_"

UUID_VERSION: Final = 7
_VARIANT_RFC4122: Final = 0x80

_KIND = re.compile(r"^[a-z][a-z0-9]{1,23}$")
_ID = re.compile(r"^(?P<kind>[a-z][a-z0-9]{1,23})_(?P<body>[0-9a-f]{32})$")

_MAX_COUNTER: Final = 0xFFF

# Identifiers available per millisecond. The seed is drawn from the lower half of
# the counter space, so at least half of this is always available in a burst.
COUNTER_SPACE: Final = _MAX_COUNTER + 1
GUARANTEED_PER_MILLISECOND: Final = COUNTER_SPACE >> 1


def is_canonical_kind(value: str) -> bool:
    """Report whether ``value`` is a canonical kind prefix."""
    return bool(_KIND.fullmatch(value))


def is_canonical_resource_id(value: str) -> bool:
    """Report whether ``value`` is a canonical resource ID.

    Checks the textual shape only. :func:`ResourceId.parse` additionally verifies
    that the payload is a version 7, RFC-variant UUID.
    """
    return bool(_ID.fullmatch(value))


def parse_kind(value: str) -> str:
    """Validate and return a kind prefix."""
    if not is_canonical_kind(value):
        raise InvalidArgument(
            f"kind must match [a-z][a-z0-9]{{{MINIMUM_KIND_LENGTH - 1},{MAXIMUM_KIND_LENGTH - 1}}}",
            reason="kind_format",
            fields={"value": value[:128]},
        )
    return value


@dataclass(frozen=True, slots=True)
class ResourceId:
    """A resource identifier: a kind prefix and a UUIDv7 payload."""

    kind: str
    raw: bytes

    def __post_init__(self) -> None:
        parse_kind(self.kind)
        if len(self.raw) != UUID_BINARY_SIZE:
            raise InvalidArgument(
                f"resource id payload must be {UUID_BINARY_SIZE} bytes",
                reason="id_length",
            )
        if self.raw[6] >> 4 != UUID_VERSION:
            raise InvalidArgument(
                "resource ids require a version 7 UUID payload", reason="id_uuid_version"
            )
        if self.raw[8] & 0xC0 != _VARIANT_RFC4122:
            raise InvalidArgument(
                "resource ids require an RFC variant UUID payload", reason="id_uuid_variant"
            )

    @classmethod
    def parse(cls, value: str) -> ResourceId:
        """Parse the canonical form, rejecting uppercase hexadecimal payloads.

        Rejected for the same reason digests reject it: one byte-for-byte
        representation, so an ID is a single key in a database and a signature.
        """
        match = _ID.fullmatch(value)
        if match is None:
            raise InvalidArgument(
                "resource id must be <kind>_<32 lowercase hex characters>",
                reason="id_format",
                fields={"value": value[:128]},
            )
        return cls(match.group("kind"), bytes.fromhex(match.group("body")))

    @property
    def body(self) -> str:
        """The 32-character hexadecimal payload, without the kind prefix."""
        return self.raw.hex()

    @property
    def unix_millis(self) -> int:
        """The UUIDv7 creation timestamp, in milliseconds since the epoch."""
        return int.from_bytes(self.raw[:6], "big")

    @property
    def text(self) -> str:
        """The canonical text form."""
        return f"{self.kind}{ID_SEPARATOR}{self.raw.hex()}"

    def __str__(self) -> str:
        return self.text


class IdGenerator:
    """An RFC 9562 method-2 UUIDv7 source.

    Two operations with deliberately different guarantees:

    ``raw_at`` honors the timestamp it is given. A caller passing an explicit
    millisecond is stamping a specific moment — backfilling a record, or building
    a deterministic fixture — and silently clamping that to "whatever the last
    call used" would embed a timestamp the caller never asked for and cannot
    detect. Ordering is guaranteed only within one requested millisecond.

    ``raw_now`` reads the clock and never goes backwards. A backward step, from
    NTP correction or a suspended host, would otherwise mint IDs sorting before
    ones already handed out; holding the last stamp and counting forward keeps
    the sort order that makes these IDs range-scannable.

    Locked because a stage worker mints from more than one thread, and two threads
    racing on the counter would produce a duplicate — the one failure a unique
    identifier may not have.
    """

    __slots__ = ("_clock", "_counter", "_last_millis", "_lock")

    def __init__(self, clock: Callable[[], int] | None = None) -> None:
        self._lock = threading.Lock()
        self._clock = clock if clock is not None else _system_clock_millis
        self._last_millis = -1
        self._counter = 0

    def raw_at(self, unix_millis: int) -> bytes:
        """Mint a payload stamped at exactly ``unix_millis``.

        At most :data:`COUNTER_SPACE` identifiers exist per millisecond, and this
        path may not borrow from the next one, so exhausting the counter raises
        rather than silently returning a different timestamp than was asked for.
        Callers minting at an explicit stamp are backfilling or building fixtures;
        thousands at one millisecond is a bug worth seeing.
        """
        if unix_millis < 0 or unix_millis >= 1 << 48:
            raise InvalidArgument(
                "UUIDv7 timestamps span 48 bits of milliseconds since the epoch",
                reason="timestamp_range",
            )
        return self._mint(unix_millis, allow_advance=False)

    def raw_now(self) -> bytes:
        """Mint a payload at the current time, never earlier than the last one."""
        return self._mint(max(self._clock(), self._last_millis), allow_advance=True)

    def _mint(self, unix_millis: int, *, allow_advance: bool) -> bytes:
        with self._lock:
            if unix_millis != self._last_millis:
                # Seed low so a burst has room to count up without spilling.
                self._counter = secrets.randbelow(_MAX_COUNTER >> 1)
            elif self._counter >= _MAX_COUNTER:
                if not allow_advance:
                    raise ResourceExhausted(
                        "UUIDv7 counter space for this millisecond is exhausted",
                        reason="id_counter_exhausted",
                        fields={"unix_millis": str(unix_millis)},
                    )
                # Wall-clock minting promises "now, in order" rather than an exact
                # stamp, so borrowing the next millisecond keeps uniqueness without
                # breaking any promise. The drift is bounded by the mint rate.
                unix_millis += 1
                self._counter = secrets.randbelow(_MAX_COUNTER >> 1)
            else:
                self._counter += 1

            self._last_millis = unix_millis
            counter = self._counter

        payload = bytearray(secrets.token_bytes(UUID_BINARY_SIZE))
        payload[0:6] = unix_millis.to_bytes(6, "big")
        payload[6] = (UUID_VERSION << 4) | (counter >> 8)
        payload[7] = counter & 0xFF
        payload[8] = (payload[8] & 0x3F) | _VARIANT_RFC4122
        return bytes(payload)


def _system_clock_millis() -> int:
    return time.time_ns() // 1_000_000


_DEFAULT_GENERATOR = IdGenerator()


def new_resource_id(kind: str) -> ResourceId:
    """Mint a resource ID of ``kind`` at the current time, monotonically."""
    return ResourceId(parse_kind(kind), _DEFAULT_GENERATOR.raw_now())


def new_resource_id_at(kind: str, unix_millis: int) -> ResourceId:
    """Mint a resource ID of ``kind`` stamped at exactly ``unix_millis``."""
    return ResourceId(parse_kind(kind), _DEFAULT_GENERATOR.raw_at(unix_millis))
