# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

from __future__ import annotations

import importlib.util
from pathlib import Path

import pytest
import yaml

ROOT = Path(__file__).resolve().parents[3]
SPEC = importlib.util.spec_from_file_location(
    "module_release", ROOT / "ci/terraform/module_release.py"
)
assert SPEC is not None and SPEC.loader is not None
MODULE_RELEASE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MODULE_RELEASE)


def test_current_planned_contract_cannot_be_released() -> None:
    with pytest.raises(ValueError, match="reviewed as released"):
        MODULE_RELEASE.validate_source("v0.4.0", "a" * 40)


def test_signed_annotated_tag_must_target_exact_commit() -> None:
    ref = {"ref": "refs/tags/v0.4.0", "object": {"type": "tag", "sha": "b" * 40}}
    tag = {
        "sha": "b" * 40,
        "tag": "v0.4.0",
        "message": "Terraform modules v0.4.0",
        "object": {"type": "commit", "sha": "a" * 40},
        "tagger": {"name": "release", "email": "release@example.com", "date": "2026-08-23"},
        "verification": {
            "verified": True,
            "reason": "valid",
            "signature": "signature",
            "payload": "payload",
            "verified_at": "2026-08-23T00:00:00Z",
        },
    }
    MODULE_RELEASE.validate_tag_documents(
        ref_document=ref, tag_document=tag, tag="v0.4.0", source_sha="a" * 40
    )
    tag["object"]["sha"] = "c" * 40
    with pytest.raises(ValueError, match="does not target"):
        MODULE_RELEASE.validate_tag_documents(
            ref_document=ref, tag_document=tag, tag="v0.4.0", source_sha="a" * 40
        )


def test_release_workflow_is_protected_fail_closed_and_non_publishing() -> None:
    source = (ROOT / ".github/workflows/terraform-module-release.yml").read_text(encoding="utf-8")
    workflow = yaml.safe_load(source)
    job = workflow["jobs"]["release-source"]
    assert job["environment"] == "terraform-module-release"
    assert workflow["permissions"] == {"contents": "read"}
    assert job["permissions"] == {
        "artifact-metadata": "write",
        "attestations": "write",
        "contents": "read",
        "id-token": "write",
    }
    assert "persist-credentials: false" in source
    assert "fetch-depth: 0" in source
    assert "verify-connected" in source and "validate-source" in source
    assert "ci/terraform/check.sh all" in source
    for publishing_command in ("git tag", "git push", "gh release"):
        assert publishing_command not in source
