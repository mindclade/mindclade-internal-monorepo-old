# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

from __future__ import annotations

import importlib.util
import subprocess
from datetime import date, timedelta
from pathlib import Path
from unittest import mock

import pytest
import yaml

ROOT = Path(__file__).resolve().parents[3]
SPEC = importlib.util.spec_from_file_location(
    "module_release", ROOT / "ci/terraform/module_release.py"
)
assert SPEC is not None and SPEC.loader is not None
MODULE_RELEASE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MODULE_RELEASE)


def qualified_authority() -> dict[str, object]:
    observed = date.today()
    evidence = {
        "change_reference": "https://github.com/mindclade/mindclade-internal-monorepo/issues/1",
        "evidence_sha256": "sha256:" + "1" * 64,
        "observed_at": observed.isoformat(),
        "expires_at": (observed + timedelta(days=30)).isoformat(),
    }
    return {
        "schema_version": 1,
        "release_tag": "v0.4.0",
        "qualification": "qualified",
        "signer": {
            "github_login": "release-operator",
            "ssh_key_fingerprint": "SHA256:" + "A" * 43,
            "tagger_email": "release@example.com",
            "tagger_name": "release",
        },
        "signer_qualification": evidence,
        "immutable_releases": {
            "qualification": "qualified",
            "enforced_by_owner": True,
            "evidence": {
                **evidence,
                "evidence_sha256": "sha256:" + "2" * 64,
                "expires_at": (observed + timedelta(days=7)).isoformat(),
            },
        },
    }


def test_current_planned_contract_cannot_be_released() -> None:
    with pytest.raises(ValueError, match="reviewed as released"):
        MODULE_RELEASE.validate_source("v0.4.0", "a" * 40)


def test_release_authority_remains_explicitly_blocked() -> None:
    authority = MODULE_RELEASE.load_release_authority(ROOT)
    assert authority["qualification"] == "blocked"
    with pytest.raises(ValueError, match="remains blocked"):
        MODULE_RELEASE.validate_release_authority(authority, require_qualified=True)


def test_qualified_authority_binds_fresh_signer_and_immutability_evidence() -> None:
    authority = qualified_authority()
    MODULE_RELEASE.validate_release_authority(authority, require_qualified=True)
    authority["immutable_releases"]["evidence"]["evidence_sha256"] = "sha256:" + "0" * 64
    with pytest.raises(ValueError, match="nonzero"):
        MODULE_RELEASE.validate_release_authority(authority, require_qualified=True)


def test_release_authority_rejects_future_or_reused_evidence() -> None:
    authority = qualified_authority()
    authority["signer_qualification"]["observed_at"] = (
        date.today() + timedelta(days=1)
    ).isoformat()
    authority["signer_qualification"]["expires_at"] = (date.today() + timedelta(days=2)).isoformat()
    with pytest.raises(ValueError, match="future-dated"):
        MODULE_RELEASE.validate_release_authority(authority, require_qualified=True)

    authority = qualified_authority()
    authority["immutable_releases"]["evidence"]["evidence_sha256"] = authority[
        "signer_qualification"
    ]["evidence_sha256"]
    with pytest.raises(ValueError, match="distinct evidence"):
        MODULE_RELEASE.validate_release_authority(authority, require_qualified=True)

    authority = qualified_authority()
    authority["immutable_releases"]["enforced_by_owner"] = False
    with pytest.raises(ValueError, match="enforced by the repository owner"):
        MODULE_RELEASE.validate_release_authority(authority, require_qualified=True)

    authority = qualified_authority()
    authority["immutable_releases"]["evidence"]["expires_at"] = (
        date.today() + timedelta(days=8)
    ).isoformat()
    with pytest.raises(ValueError, match="between 1 and 7 days"):
        MODULE_RELEASE.validate_release_authority(authority, require_qualified=True)


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


def test_signer_binding_rejects_wrong_cryptographic_fingerprint() -> None:
    authority = qualified_authority()
    tag_document = {
        "sha": "b" * 40,
        "tagger": {"name": "release", "email": "release@example.com", "date": "2026-08-23"},
        "verification": {
            "signature": "-----BEGIN SSH SIGNATURE-----\nsignature\n-----END SSH SIGNATURE-----\n",
            "payload": "payload",
            "verified_at": "2026-08-23T00:00:00Z",
        },
    }
    result = subprocess.CompletedProcess(
        args=["ssh-keygen"],
        returncode=0,
        stdout='Good "git" signature with ED25519 key SHA256:' + "B" * 43,
        stderr="",
    )
    with (
        mock.patch.object(MODULE_RELEASE.subprocess, "run", return_value=result),
        pytest.raises(ValueError, match="fingerprint disagrees"),
    ):
        MODULE_RELEASE.bind_signer_evidence(
            tag_document=tag_document,
            authority=authority,
        )


def test_release_workflow_is_protected_fail_closed_and_never_manages_tags() -> None:
    source = (ROOT / ".github/workflows/terraform-module-release.yml").read_text(encoding="utf-8")
    workflow = yaml.safe_load(source)
    authorize = workflow["jobs"]["authorize"]
    job = workflow["jobs"]["publish"]
    assert authorize["permissions"] == {"contents": "read"}
    assert job["needs"] == "authorize"
    assert job["environment"] == "terraform-module-release"
    assert workflow["permissions"] == {"contents": "read"}
    assert job["permissions"] == {
        "artifact-metadata": "write",
        "attestations": "write",
        "contents": "write",
        "id-token": "write",
    }
    assert source.count("persist-credentials: false") == 3
    assert source.count("fetch-depth: 0") == 3
    assert "workflow_dispatch:" in source
    assert "github.ref_protected" in source
    assert "refs/heads/main" in source
    assert "verify-connected" in source and "validate-source" in source
    assert '--change-reference "${CHANGE_REFERENCE}"' in source
    assert source.count("assert_current_authorities") == 3
    assert "immutable-releases-evidence-digest" in source
    assert ".assets[] | {name: .name, digest: .digest}" in source
    assert "git fetch" not in source
    assert 'git/ref/heads/main" --jq .object.sha' in source
    assert "releases/${release_id}" in source
    assert "gh release edit" not in source
    assert "cachix/install-nix-action@630ae543ea3a38a9a4166f03376c02c50f408342" in source
    assert "ci/terraform/check.sh all" in source
    assert "gh release create" in source
    assert "--verify-tag" in source
    assert "--draft" in source
    assert ".immutable == true" in source
    assert "steps.attest.outputs.bundle-path" in source
    for publishing_command in ("git tag", "git push", "gh release delete"):
        assert publishing_command not in source
