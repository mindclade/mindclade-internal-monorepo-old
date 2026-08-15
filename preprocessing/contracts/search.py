# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Search policy contract."""

from dataclasses import dataclass


@dataclass(frozen=True)
class SearchPolicy:
    tool: str
    tool_version: str
    database_snapshot_digest: str
    parameters_digest: str
    maximum_hits: int
    maximum_sequences: int

    def __post_init__(self):
        if (
            not self.tool
            or not self.tool_version
            or self.maximum_hits <= 0
            or self.maximum_sequences <= 0
        ):
            raise ValueError("invalid search policy")
