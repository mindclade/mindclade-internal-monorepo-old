# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

"""Keep the connected evidence aggregate aligned with the canonical release wire."""

from mindclade.registry.v1 import release_pb2

from tools.qualification.training_gke.evidence import REQUIRED_ARTIFACT_KINDS


def test_required_connected_artifacts_are_canonical_release_evidence_kinds() -> None:
    declared = set(release_pb2.EvidenceKind.keys())
    required = {f"EVIDENCE_KIND_{kind.upper()}" for kind in REQUIRED_ARTIFACT_KINDS}
    assert required <= declared
    assert all(release_pb2.EvidenceKind.Value(name) > 0 for name in required)
