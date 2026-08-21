# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""MSA search index candidate."""

from __future__ import annotations

from data.manifest import ArtifactRef

from ..index import ReferenceIndex


def msa_index(
    artifacts: tuple[ArtifactRef, ...],
    *,
    format_version: str,
    tool: str,
    tool_version: str,
    parameters_digest: str,
) -> ReferenceIndex:
    return ReferenceIndex("msa", format_version, tool, tool_version, parameters_digest, artifacts)
