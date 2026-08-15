# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

from dataclasses import dataclass


@dataclass(frozen=True)
class ToolchainRecord:
    tool: str
    version: str
    binary_digest: str
    arguments_digest: str
