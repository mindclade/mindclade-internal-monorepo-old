# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Chemical Component Dictionary index candidate."""

from __future__ import annotations

from data.manifest import ArtifactRef

from ..index import ReferenceIndex


def chemical_components_index(
    artifacts: tuple[ArtifactRef, ...], *, tool_version: str, parameters_digest: str
) -> ReferenceIndex:
    return ReferenceIndex(
        "chemical-components",
        "v1",
        "mindclade-ccd-builder",
        tool_version,
        parameters_digest,
        artifacts,
    )
