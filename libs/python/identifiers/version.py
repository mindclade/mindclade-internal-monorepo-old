# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Optimistic-concurrency tokens shared with Go and Rust.

A resource version binds a monotonic generation to the digest of the durable
representation observed at that generation: ``rv1:<generation>:sha256:<hex>``.
Carrying the digest as well as the counter is what makes a compare-and-swap
meaningful — two writers that both read generation 41 and both compute a new
state can be told apart, and a generation that advanced without the content
changing is visible rather than silent.

The wire form is the one ``libs/go/resourceversion/version.go`` parses and
``tests/integration/cross_language/test_resource_versions.py`` pins.
"""

from __future__ import annotations

import re
from dataclasses import dataclass
from typing import Final

from libs.python.errors import InvalidArgument

from .digest import Digest

SCHEMA_PREFIX: Final = "rv1"

# Leading zeros are rejected rather than tolerated: "rv1:007:..." and "rv1:7:..."
# would otherwise be two spellings of one version, and this token is compared as a
# string in caches and conditional requests.
_VERSION = re.compile(r"^rv1:(?P<generation>[1-9][0-9]*):(?P<digest>sha256:[0-9a-f]{64})$")


@dataclass(frozen=True, slots=True)
class ResourceVersion:
    """A generation paired with the content digest observed at it.

    Generations start at 1. Unlike Go's ``Version``, there is no zero value
    meaning absence — absence is ``ResourceVersion | None``, matching how
    :class:`~libs.python.identifiers.digest.Digest` drops Go's presence bit.
    """

    generation: int
    digest: Digest

    def __post_init__(self) -> None:
        if self.generation < 1:
            raise InvalidArgument(
                "resource version generations start at 1",
                reason="invalid_resource_version_generation",
            )

    @classmethod
    def parse(cls, value: str) -> ResourceVersion:
        """Parse the canonical text form."""
        match = _VERSION.fullmatch(value)
        if match is None:
            raise InvalidArgument(
                "resource version must be rv1:<generation>:sha256:<64 lowercase hex>",
                reason="invalid_resource_version_schema",
                fields={"value": value[:128]},
            )
        return cls(int(match.group("generation")), Digest.parse(match.group("digest")))

    @property
    def text(self) -> str:
        """The canonical text form."""
        return f"{SCHEMA_PREFIX}:{self.generation}:{self.digest.text}"

    def next(self, digest: Digest) -> ResourceVersion:
        """The version that follows this one for ``digest``."""
        return ResourceVersion(self.generation + 1, digest)

    def __str__(self) -> str:
        return self.text


def is_canonical_resource_version(value: str) -> bool:
    """Report whether ``value`` is a canonical resource version."""
    return bool(_VERSION.fullmatch(value))
