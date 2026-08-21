# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Python-owned scientific stage contract."""

from __future__ import annotations

import re
from dataclasses import dataclass
from enum import StrEnum

_DIGEST = re.compile(r"sha256:[0-9a-f]{64}")
_VERSION = re.compile(r"[A-Za-z0-9][A-Za-z0-9._+-]{0,127}")


class StageKind(StrEnum):
    CANONICALIZE = "canonicalize"
    CURATE = "curate"
    QUALITY = "quality"
    SHARD = "shard"


@dataclass(frozen=True, slots=True)
class StageSpec:
    kind: StageKind
    implementation_version: str
    input_manifest_digest: str
    output_schema_digest: str
    replay_safe: bool

    def __post_init__(self) -> None:
        if not isinstance(self.kind, StageKind):
            raise ValueError("ingestion stage kind is invalid")
        if not isinstance(self.implementation_version, str) or not _VERSION.fullmatch(
            self.implementation_version
        ):
            raise ValueError("ingestion implementation version is invalid")
        for value, name in (
            (self.input_manifest_digest, "input manifest"),
            (self.output_schema_digest, "output schema"),
        ):
            if not isinstance(value, str) or not _DIGEST.fullmatch(value):
                raise ValueError(f"ingestion {name} digest is invalid")
        if not isinstance(self.replay_safe, bool):
            raise ValueError("ingestion replay_safe must be boolean")

    @property
    def idempotency_key(self) -> str:
        return ":".join(
            (
                self.kind.value,
                self.implementation_version,
                self.input_manifest_digest,
                self.output_schema_digest,
            )
        )
