#!/usr/bin/env python3
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

"""Fail-closed source and signed-tag validation for Terraform module releases."""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import re
import subprocess
import tomllib
import urllib.request
from pathlib import Path
from typing import Any

ROOT = Path(__file__).resolve().parents[2]
VERSION_POLICY = ROOT / "infra/terraform/governance/version.toml"
INTERFACE_MANIFEST = ROOT / "infra/terraform/governance/module-interfaces.json"
TAG = re.compile(r"^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$")
SHA = re.compile(r"^[0-9a-f]{40}$")


def load_source_contract() -> tuple[dict[str, Any], dict[str, Any]]:
    policy = tomllib.loads(VERSION_POLICY.read_text(encoding="utf-8"))
    manifest = json.loads(INTERFACE_MANIFEST.read_text(encoding="utf-8"))
    if not isinstance(policy, dict) or not isinstance(manifest, dict):
        raise ValueError("Terraform release governance sources must be objects")
    return policy, manifest


def validate_source(tag: str, source_sha: str) -> tuple[dict[str, Any], dict[str, Any]]:
    if TAG.fullmatch(tag) is None:
        raise ValueError("Terraform module release tag must be strict vMAJOR.MINOR.PATCH")
    if SHA.fullmatch(source_sha) is None:
        raise ValueError("Terraform module release source must be a full commit SHA")
    version = tag.removeprefix("v")
    policy, manifest = load_source_contract()
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


def verify_connected(repository: str, tag: str, source_sha: str, token: str) -> None:
    if re.fullmatch(r"[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+", repository) is None:
        raise ValueError("GitHub repository must be owner/name")
    if not token:
        raise ValueError("GitHub token is required for signed tag verification")
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


def build_manifest(tag: str, source_sha: str) -> dict[str, Any]:
    policy, interfaces = validate_source(tag, source_sha)
    interface_bytes = INTERFACE_MANIFEST.read_bytes()
    module_tree = subprocess.run(
        ["git", "rev-parse", f"{source_sha}:infra/terraform/modules"],
        cwd=ROOT,
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
        "schema_version": 1,
        "source_revision": source_sha,
        "status": "released",
    }


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    subparsers = parser.add_subparsers(dest="command", required=True)
    for command in ("validate-source", "verify-connected", "manifest"):
        subparser = subparsers.add_parser(command)
        subparser.add_argument("--tag", required=True)
        subparser.add_argument("--source-sha", required=True)
        if command == "verify-connected":
            subparser.add_argument("--repository", required=True)
        if command == "manifest":
            subparser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    validate_source(args.tag, args.source_sha)
    if args.command == "verify-connected":
        verify_connected(args.repository, args.tag, args.source_sha, os.environ.get("GH_TOKEN", ""))
    elif args.command == "manifest":
        document = build_manifest(args.tag, args.source_sha)
        args.output.write_text(
            json.dumps(document, indent=2, sort_keys=True) + "\n", encoding="utf-8"
        )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
