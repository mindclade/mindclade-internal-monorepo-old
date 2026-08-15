# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Immutable scientific reference database snapshot provenance."""

from dataclasses import dataclass


@dataclass(frozen=True)
class DatabaseSnapshot:
    release_id: str
    name: str
    version: str
    snapshot_digest: str
    source_cutoff: str
    index_format: str
    index_tool: str
    index_tool_version: str
    shard_digests: tuple[str, ...]

    def __post_init__(self):
        if (
            not self.release_id
            or not self.snapshot_digest.startswith("sha256:")
            or not self.shard_digests
        ):
            raise ValueError("invalid database snapshot")
