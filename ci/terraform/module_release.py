#!/usr/bin/env python3
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

"""Fail-closed source, signer, and release evidence for Terraform modules."""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import re
import subprocess
import tempfile
import tomllib
import urllib.request
from datetime import date
from pathlib import Path
from typing import Any

ROOT = Path(__file__).resolve().parents[2]
VERSION_POLICY = Path("infra/terraform/governance/version.toml")
INTERFACE_MANIFEST = Path("infra/terraform/governance/module-interfaces.json")
RELEASE_AUTHORITY = Path("infra/terraform/governance/module-release-authority.json")
TAG = re.compile(r"^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$")
SHA = re.compile(r"^[0-9a-f]{40}$")
SHA256 = re.compile(r"^sha256:[0-9a-f]{64}$")
SSH_FINGERPRINT = re.compile(r"^SHA256:[A-Za-z0-9+/]{43}=?$")
GITHUB_LOGIN = re.compile(r"^[A-Za-z0-9](?:[A-Za-z0-9-]{0,37}[A-Za-z0-9])?$")
CHANGE_REFERENCE = re.compile(
    r"^https://github[.]com/mindclade/[A-Za-z0-9_.-]+/(?:pull|issues)/[1-9][0-9]*$"
)


def exact_keys(value: dict[str, Any], expected: set[str], label: str) -> None:
    if set(value) != expected:
        raise ValueError(f"{label} keys must be exactly {sorted(expected)}")


def load_source_contract(root: Path = ROOT) -> tuple[dict[str, Any], dict[str, Any]]:
    policy = tomllib.loads((root / VERSION_POLICY).read_text(encoding="utf-8"))
    manifest = json.loads((root / INTERFACE_MANIFEST).read_text(encoding="utf-8"))
    if not isinstance(policy, dict) or not isinstance(manifest, dict):
        raise ValueError("Terraform release governance sources must be objects")
    return policy, manifest


def load_release_authority(root: Path = ROOT) -> dict[str, Any]:
    value = json.loads((root / RELEASE_AUTHORITY).read_text(encoding="utf-8"))
    if not isinstance(value, dict):
        raise ValueError("Terraform module release authority must be one JSON object")
    validate_release_authority(value)
    return value


def _validate_evidence(value: Any, label: str, *, require_fresh: bool) -> dict[str, Any]:
    if not isinstance(value, dict):
        raise ValueError(f"{label} must be an object")
    exact_keys(
        value,
        {"change_reference", "evidence_sha256", "observed_at", "expires_at"},
        label,
    )
    if not CHANGE_REFERENCE.fullmatch(str(value["change_reference"])):
        raise ValueError(
            f"{label} change reference must be an exact Mindclade pull request or issue"
        )
    if not SHA256.fullmatch(str(value["evidence_sha256"])) or value["evidence_sha256"] == (
        "sha256:" + "0" * 64
    ):
        raise ValueError(f"{label} digest must be a nonzero SHA-256 identity")
    try:
        observed = date.fromisoformat(str(value["observed_at"]))
        expires = date.fromisoformat(str(value["expires_at"]))
    except ValueError as error:
        raise ValueError(f"{label} dates must be ISO calendar dates") from error
    lifetime = (expires - observed).days
    if lifetime < 1 or lifetime > 90:
        raise ValueError(f"{label} evidence lifetime must be between 1 and 90 days")
    today = date.today()
    if observed > today:
        raise ValueError(f"{label} evidence is future-dated")
    if require_fresh and expires < today:
        raise ValueError(f"{label} evidence has expired")
    return value


def validate_release_authority(
    value: dict[str, Any], *, require_qualified: bool = False
) -> dict[str, Any]:
    exact_keys(
        value,
        {
            "schema_version",
            "release_tag",
            "qualification",
            "signer",
            "signer_qualification",
            "immutable_releases",
        },
        "Terraform module release authority",
    )
    if value["schema_version"] != 1 or value["release_tag"] != "v0.4.0":
        raise ValueError("Terraform module release authority must govern schema 1 v0.4.0")
    if value["qualification"] not in {"blocked", "qualified"}:
        raise ValueError("Terraform module release authority qualification is invalid")
    immutable = value["immutable_releases"]
    if not isinstance(immutable, dict):
        raise ValueError("immutable release authority must be an object")
    exact_keys(immutable, {"qualification", "evidence"}, "immutable release authority")
    if immutable["qualification"] not in {"blocked", "qualified"}:
        raise ValueError("immutable release qualification is invalid")

    if value["qualification"] == "blocked":
        if value["signer"] is not None or value["signer_qualification"] is not None:
            raise ValueError("blocked release authority must not name an unqualified signer")
        if immutable != {"qualification": "blocked", "evidence": None}:
            raise ValueError("blocked release authority must not claim immutable release evidence")
        if require_qualified:
            raise ValueError("Terraform module release authority remains blocked")
        return value

    signer = value["signer"]
    if not isinstance(signer, dict):
        raise ValueError("qualified release authority must name its signer")
    exact_keys(
        signer,
        {"github_login", "ssh_key_fingerprint", "tagger_email", "tagger_name"},
        "Terraform module release signer",
    )
    if not GITHUB_LOGIN.fullmatch(str(signer["github_login"])):
        raise ValueError("release signer GitHub login is invalid")
    if not SSH_FINGERPRINT.fullmatch(str(signer["ssh_key_fingerprint"])):
        raise ValueError("release signer SSH fingerprint is invalid")
    for field in ("tagger_name", "tagger_email"):
        if not isinstance(signer[field], str) or not signer[field].strip():
            raise ValueError(f"release signer {field} is empty")
    if "@" not in signer["tagger_email"]:
        raise ValueError("release signer tagger_email is invalid")
    _validate_evidence(
        value["signer_qualification"], "signer qualification", require_fresh=require_qualified
    )
    if immutable["qualification"] != "qualified":
        raise ValueError("qualified release authority requires immutable releases")
    _validate_evidence(
        immutable["evidence"], "immutable release qualification", require_fresh=require_qualified
    )
    if immutable["evidence"]["evidence_sha256"] == value["signer_qualification"]["evidence_sha256"]:
        raise ValueError("signer and immutable release qualifications require distinct evidence")
    return value


def validate_source(
    tag: str, source_sha: str, root: Path = ROOT
) -> tuple[dict[str, Any], dict[str, Any]]:
    if TAG.fullmatch(tag) is None:
        raise ValueError("Terraform module release tag must be strict vMAJOR.MINOR.PATCH")
    if SHA.fullmatch(source_sha) is None:
        raise ValueError("Terraform module release source must be a full commit SHA")
    version = tag.removeprefix("v")
    policy, manifest = load_source_contract(root)
    load_release_authority(root)
    if policy.get("contract_version") != version:
        raise ValueError("release tag disagrees with Terraform version policy")
    if manifest.get("contract_version") != version:
        raise ValueError("release tag disagrees with generated Terraform interfaces")
    if policy.get("status") != "released" or manifest.get("status") != "released":
        raise ValueError(
            "Terraform contract must be reviewed as released before a tag can produce artifacts"
        )
    return policy, manifest


def validate_tag_documents(
    *,
    ref_document: dict[str, Any],
    tag_document: dict[str, Any],
    tag: str,
    source_sha: str,
) -> None:
    ref_object = ref_document.get("object")
    if ref_document.get("ref") != f"refs/tags/{tag}":
        raise ValueError("release ref name disagrees with the requested tag")
    if not isinstance(ref_object, dict) or ref_object.get("type") != "tag":
        raise ValueError("release ref must point to an annotated tag object")
    if ref_object.get("sha") != tag_document.get("sha"):
        raise ValueError("release ref and annotated tag object disagree")
    if tag_document.get("tag") != tag:
        raise ValueError("annotated tag name disagrees with its release ref")
    if not isinstance(tag_document.get("message"), str) or not tag_document["message"].strip():
        raise ValueError("annotated release tag omits its release message")
    target = tag_document.get("object")
    if not isinstance(target, dict) or target.get("type") != "commit":
        raise ValueError("annotated release tag must target a commit")
    if target.get("sha") != source_sha:
        raise ValueError("annotated release tag does not target the checked source commit")
    verification = tag_document.get("verification")
    if not isinstance(verification, dict):
        raise ValueError("annotated release tag omits signature verification")
    if verification.get("verified") is not True or verification.get("reason") != "valid":
        raise ValueError("annotated release tag signature is not valid")
    for field in ("signature", "payload", "verified_at"):
        if not isinstance(verification.get(field), str) or not verification[field]:
            raise ValueError(f"annotated release tag verification omits {field}")
    tagger = tag_document.get("tagger")
    if not isinstance(tagger, dict) or not all(
        tagger.get(key) for key in ("name", "email", "date")
    ):
        raise ValueError("annotated release tag omits complete tagger identity")


def verify_ssh_signature(payload: str, signature: str, expected_fingerprint: str) -> None:
    if not signature.startswith("-----BEGIN SSH SIGNATURE-----"):
        raise ValueError("release signer contract requires an SSH-signed annotated tag")
    with tempfile.TemporaryDirectory(prefix="terraform-module-signature-") as directory:
        signature_path = Path(directory) / "tag.sshsig"
        signature_path.write_text(signature, encoding="utf-8")
        result = subprocess.run(
            [
                "ssh-keygen",
                "-Y",
                "check-novalidate",
                "-n",
                "git",
                "-s",
                str(signature_path),
            ],
            input=payload,
            text=True,
            capture_output=True,
            check=False,
        )
    evidence = f"{result.stdout}\n{result.stderr}"
    if result.returncode != 0:
        raise ValueError("annotated tag SSH signature is cryptographically invalid")
    fingerprints = set(re.findall(r"SHA256:[A-Za-z0-9+/]{43}=?", evidence))
    if fingerprints != {expected_fingerprint}:
        raise ValueError("annotated tag signer fingerprint disagrees with release authority")


def bind_signer_evidence(
    *, tag_document: dict[str, Any], authority: dict[str, Any]
) -> dict[str, Any]:
    validate_release_authority(authority, require_qualified=True)
    signer = authority["signer"]
    tagger = tag_document["tagger"]
    verification = tag_document["verification"]
    if tagger["name"] != signer["tagger_name"] or tagger["email"] != signer["tagger_email"]:
        raise ValueError("annotated tagger identity disagrees with release authority")
    verify_ssh_signature(
        verification["payload"],
        verification["signature"],
        signer["ssh_key_fingerprint"],
    )
    return {
        "github_login": signer["github_login"],
        "immutable_releases_evidence": authority["immutable_releases"]["evidence"],
        "signature_sha256": "sha256:"
        + hashlib.sha256(verification["signature"].encode("utf-8")).hexdigest(),
        "signer_qualification": authority["signer_qualification"],
        "ssh_key_fingerprint": signer["ssh_key_fingerprint"],
        "tag_object_sha": tag_document["sha"],
        "tagger": tagger,
        "verified_at": verification["verified_at"],
    }


def github_json(url: str, token: str) -> dict[str, Any]:
    request = urllib.request.Request(
        url,
        headers={
            "Accept": "application/vnd.github+json",
            "Authorization": f"Bearer {token}",
            "X-GitHub-Api-Version": "2026-03-10",
        },
    )
    with urllib.request.urlopen(request, timeout=20) as response:
        document = json.load(response)
    if not isinstance(document, dict):
        raise ValueError("GitHub tag API response must be an object")
    return document


def verify_connected(
    repository: str,
    tag: str,
    source_sha: str,
    token: str,
    authority: dict[str, Any],
    signer_evidence_digest: str,
    immutable_releases_evidence_digest: str,
) -> dict[str, Any]:
    if re.fullmatch(r"[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+", repository) is None:
        raise ValueError("GitHub repository must be owner/name")
    if not token:
        raise ValueError("GitHub token is required for signed tag verification")
    validate_release_authority(authority, require_qualified=True)
    if authority["release_tag"] != tag:
        raise ValueError("release authority does not govern the requested tag")
    if authority["signer_qualification"]["evidence_sha256"] != signer_evidence_digest:
        raise ValueError("signer evidence input disagrees with release authority")
    if (
        authority["immutable_releases"]["evidence"]["evidence_sha256"]
        != immutable_releases_evidence_digest
    ):
        raise ValueError("immutable release evidence input disagrees with release authority")
    base = f"https://api.github.com/repos/{repository}"
    ref_document = github_json(f"{base}/git/ref/tags/{tag}", token)
    ref_object = ref_document.get("object")
    if not isinstance(ref_object, dict) or not isinstance(ref_object.get("sha"), str):
        raise ValueError("GitHub release ref omits its object SHA")
    tag_document = github_json(f"{base}/git/tags/{ref_object['sha']}", token)
    validate_tag_documents(
        ref_document=ref_document,
        tag_document=tag_document,
        tag=tag,
        source_sha=source_sha,
    )
    return {
        "release_tag": tag,
        "source_revision": source_sha,
        "signer": bind_signer_evidence(tag_document=tag_document, authority=authority),
    }


def load_tag_evidence(path: Path, tag: str, source_sha: str) -> dict[str, Any]:
    value = json.loads(path.read_text(encoding="utf-8"))
    if not isinstance(value, dict):
        raise ValueError("tag evidence must be one JSON object")
    exact_keys(value, {"release_tag", "source_revision", "signer"}, "tag evidence")
    if value["release_tag"] != tag or value["source_revision"] != source_sha:
        raise ValueError("tag evidence disagrees with the module release identity")
    if not isinstance(value["signer"], dict):
        raise ValueError("tag evidence omits its signer binding")
    return value


def build_manifest(
    tag: str, source_sha: str, tag_evidence: dict[str, Any], root: Path = ROOT
) -> dict[str, Any]:
    policy, interfaces = validate_source(tag, source_sha, root)
    if tag_evidence["release_tag"] != tag or tag_evidence["source_revision"] != source_sha:
        raise ValueError("tag evidence disagrees with the module release identity")
    interface_bytes = (root / INTERFACE_MANIFEST).read_bytes()
    module_tree = subprocess.run(
        ["git", "rev-parse", f"{source_sha}:infra/terraform/modules"],
        cwd=root,
        check=True,
        capture_output=True,
        text=True,
    ).stdout.strip()
    if SHA.fullmatch(module_tree) is None:
        raise ValueError("Terraform module tree must resolve to a full Git object SHA")
    return {
        "contract_version": policy["contract_version"],
        "interface_manifest_sha256": hashlib.sha256(interface_bytes).hexdigest(),
        "module_count": len(interfaces.get("modules", {})),
        "module_tree_sha": module_tree,
        "release_tag": tag,
        "schema_version": 2,
        "source_revision": source_sha,
        "status": "released",
        "tag_identity": tag_evidence["signer"],
    }


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    subparsers = parser.add_subparsers(dest="command", required=True)
    for command in ("validate-source", "verify-connected", "manifest"):
        subparser = subparsers.add_parser(command)
        subparser.add_argument("--tag", required=True)
        subparser.add_argument("--source-sha", required=True)
        subparser.add_argument("--source-root", type=Path, default=ROOT)
        if command == "verify-connected":
            subparser.add_argument("--repository", required=True)
            subparser.add_argument("--signer-evidence-digest", required=True)
            subparser.add_argument("--immutable-releases-evidence-digest", required=True)
            subparser.add_argument("--output", type=Path, required=True)
        if command == "manifest":
            subparser.add_argument("--tag-evidence", type=Path, required=True)
            subparser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    root = args.source_root.resolve()
    validate_source(args.tag, args.source_sha, root)
    if args.command == "verify-connected":
        document = verify_connected(
            args.repository,
            args.tag,
            args.source_sha,
            os.environ.get("GH_TOKEN", ""),
            load_release_authority(root),
            args.signer_evidence_digest,
            args.immutable_releases_evidence_digest,
        )
        args.output.write_text(
            json.dumps(document, indent=2, sort_keys=True) + "\n", encoding="utf-8"
        )
    elif args.command == "manifest":
        tag_evidence = load_tag_evidence(args.tag_evidence, args.tag, args.source_sha)
        document = build_manifest(args.tag, args.source_sha, tag_evidence, root)
        args.output.write_text(
            json.dumps(document, indent=2, sort_keys=True) + "\n", encoding="utf-8"
        )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
