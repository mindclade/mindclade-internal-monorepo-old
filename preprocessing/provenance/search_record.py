# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

from dataclasses import dataclass


@dataclass(frozen=True)
class SearchRecord:
    entity_digest: str
    database_snapshot_digest: str
    tool: str
    tool_version: str
    policy_digest: str
    raw_result_digest: str
    parsed_result_digest: str
