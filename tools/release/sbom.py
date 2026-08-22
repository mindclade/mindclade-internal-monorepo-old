#!/usr/bin/env python3
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

"""Enrich and validate release SPDX 2.3 SBOMs with the proprietary LicenseRef."""

from __future__ import annotations

import argparse
import hashlib
import json
import re
import sys
import tempfile
from pathlib import Path
from typing import Any

LICENSE_ID = "LicenseRef-Mindclade-Proprietary"
LICENSE_NAME = "Mindclade Proprietary License (mindclade-license@2)"
PACKAGE_ID = "SPDXRef-Mindclade-Release"
DOCUMENT_ID = "SPDXRef-DOCUMENT"
SHA_RE = re.compile(r"^[a-f0-9]{40}$")
RELEASE_RE = re.compile(r"^v[0-9]+\.[0-9]+\.[0-9]+$")


class SbomError(ValueError):
    """An SPDX document or proprietary-license projection violated its contract."""


def _sha256(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def _mapping(value: Any, label: str) -> dict[str, Any]:
    if not isinstance(value, dict) or not all(isinstance(key, str) for key in value):
        raise SbomError(f"{label} must be a string-keyed object")
    return value


def _load_json(path: Path, label: str) -> dict[str, Any]:
    try:
        value = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        raise SbomError(f"cannot load {label} {path}: {exc}") from exc
    return _mapping(value, label)


def _policy_identity(path: Path) -> tuple[str, str]:
    manifest = _load_json(path, "policy manifest")
    if manifest.get("bundleId") != "mindclade-policy-bundle":
        raise SbomError("policy manifest bundleId is not canonical")
    if manifest.get("licenseExpression") != LICENSE_ID:
        raise SbomError("policy manifest does not declare the proprietary SPDX LicenseRef")
    version = manifest.get("version")
    if not isinstance(version, str) or not re.fullmatch(
        r"[0-9]{4}\.[0-9]{2}\.[0-9]{2}\.[1-9][0-9]*", version
    ):
        raise SbomError("policy manifest version is malformed")
    return version, _sha256(path)


def _license_info(
    license_text: str,
    license_digest: str,
    source_sha: str,
    bundle_version: str,
    manifest_digest: str,
) -> dict[str, Any]:
    source_url = (
        "https://github.com/mindclade/mindclade-internal-monorepo/blob/"
        f"{source_sha}/LICENSE"
    )
    policy_url = (
        "https://github.com/mindclade/.github/blob/main/"
        "contracts/policy-bundle/manifest.json"
    )
    return {
        "licenseId": LICENSE_ID,
        "extractedText": license_text,
        "name": LICENSE_NAME,
        "comment": (
            f"Complete canonical license text; SHA-256: {license_digest}; "
            f"policy bundle: {bundle_version}; manifest SHA-256: {manifest_digest}."
        ),
        "seeAlsos": [source_url, policy_url],
    }


def enrich_document(
    document: dict[str, Any],
    *,
    license_text: str,
    license_digest: str,
    source_sha: str,
    release_id: str,
    bundle_version: str,
    manifest_digest: str,
) -> dict[str, Any]:
    """Return an enriched SPDX 2.3 document without weakening generator evidence."""
    if document.get("spdxVersion") != "SPDX-2.3":
        raise SbomError("release SBOM must use SPDX-2.3")
    if document.get("dataLicense") != "CC0-1.0":
        raise SbomError("SPDX document dataLicense must be CC0-1.0")
    if document.get("SPDXID") != DOCUMENT_ID:
        raise SbomError(f"SPDX document id must be {DOCUMENT_ID}")
    if not SHA_RE.fullmatch(source_sha):
        raise SbomError("source SHA must be a lowercase 40-character Git commit")
    if not RELEASE_RE.fullmatch(release_id):
        raise SbomError("release id must be a full vX.Y.Z identifier")
    if not license_text.endswith("\n") or LICENSE_ID not in license_text:
        raise SbomError(
            "canonical LICENSE must be complete, newline-terminated, and identify its LicenseRef"
        )
    actual_license_digest = hashlib.sha256(license_text.encode("utf-8")).hexdigest()
    if actual_license_digest != license_digest:
        raise SbomError("canonical LICENSE digest does not match its bytes")

    extracted = document.get("hasExtractedLicensingInfos", [])
    if not isinstance(extracted, list):
        raise SbomError("hasExtractedLicensingInfos must be a list")
    extracted = [
        item
        for item in extracted
        if not isinstance(item, dict) or item.get("licenseId") != LICENSE_ID
    ]
    extracted.append(
        _license_info(
            license_text,
            license_digest,
            source_sha,
            bundle_version,
            manifest_digest,
        )
    )
    document["hasExtractedLicensingInfos"] = sorted(
        extracted,
        key=lambda item: str(item.get("licenseId", "")) if isinstance(item, dict) else "",
    )

    packages = document.get("packages", [])
    if not isinstance(packages, list):
        raise SbomError("packages must be a list")
    packages = [
        package
        for package in packages
        if not isinstance(package, dict) or package.get("SPDXID") != PACKAGE_ID
    ]
    packages.append(
        {
            "name": "mindclade-release",
            "SPDXID": PACKAGE_ID,
            "versionInfo": release_id,
            "supplier": "Organization: Mindclade, LLC.",
            "downloadLocation": "NOASSERTION",
            "filesAnalyzed": False,
            "licenseConcluded": LICENSE_ID,
            "licenseDeclared": LICENSE_ID,
            "copyrightText": "Copyright © 2026 Mindclade, LLC. All Rights Reserved.",
            "comment": f"First-party release source commit {source_sha}.",
        }
    )
    document["packages"] = packages

    relationships = document.get("relationships", [])
    if not isinstance(relationships, list):
        raise SbomError("relationships must be a list")
    relationship = {
        "spdxElementId": DOCUMENT_ID,
        "relationshipType": "DESCRIBES",
        "relatedSpdxElement": PACKAGE_ID,
    }
    relationships = [
        item
        for item in relationships
        if not (
            isinstance(item, dict)
            and item.get("spdxElementId") == DOCUMENT_ID
            and item.get("relationshipType") == "DESCRIBES"
            and item.get("relatedSpdxElement") == PACKAGE_ID
        )
    ]
    relationships.append(relationship)
    document["relationships"] = relationships
    return document


def validate_document(
    document: dict[str, Any],
    *,
    license_text: str,
    license_digest: str,
    source_sha: str,
    release_id: str,
    bundle_version: str,
    manifest_digest: str,
) -> None:
    expected = enrich_document(
        json.loads(json.dumps(document)),
        license_text=license_text,
        license_digest=license_digest,
        source_sha=source_sha,
        release_id=release_id,
        bundle_version=bundle_version,
        manifest_digest=manifest_digest,
    )
    expected_license = next(
        item
        for item in expected["hasExtractedLicensingInfos"]
        if item.get("licenseId") == LICENSE_ID
    )
    actual_licenses = [
        item
        for item in document.get("hasExtractedLicensingInfos", [])
        if isinstance(item, dict) and item.get("licenseId") == LICENSE_ID
    ]
    if actual_licenses != [expected_license]:
        raise SbomError("SPDX proprietary extracted licensing information is absent or stale")
    actual_packages = [
        item
        for item in document.get("packages", [])
        if isinstance(item, dict) and item.get("SPDXID") == PACKAGE_ID
    ]
    expected_package = next(
        item for item in expected["packages"] if item.get("SPDXID") == PACKAGE_ID
    )
    if actual_packages != [expected_package]:
        raise SbomError("SPDX first-party release package is absent or stale")
    relationship = {
        "spdxElementId": DOCUMENT_ID,
        "relationshipType": "DESCRIBES",
        "relatedSpdxElement": PACKAGE_ID,
    }
    if document.get("relationships", []).count(relationship) != 1:
        raise SbomError(
            "SPDX document must describe the first-party release package exactly once"
        )


def _atomic_json(path: Path, value: dict[str, Any]) -> None:
    payload = json.dumps(value, indent=2, sort_keys=True, ensure_ascii=False) + "\n"
    path.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.NamedTemporaryFile(
        mode="w",
        encoding="utf-8",
        dir=path.parent,
        prefix=f".{path.name}.",
        delete=False,
    ) as stream:
        temporary = Path(stream.name)
        stream.write(payload)
        stream.flush()
    temporary.replace(path)


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("command", choices=("enrich", "validate"))
    parser.add_argument("--input", type=Path, required=True)
    parser.add_argument("--output", type=Path)
    parser.add_argument("--license", type=Path, required=True)
    parser.add_argument("--policy-manifest", type=Path, required=True)
    parser.add_argument("--source-sha", required=True)
    parser.add_argument("--release-id", required=True)
    args = parser.parse_args()
    try:
        document = _load_json(args.input, "SPDX document")
        license_text = args.license.read_text(encoding="utf-8")
        license_digest = _sha256(args.license)
        bundle_version, manifest_digest = _policy_identity(args.policy_manifest)
        common = {
            "license_text": license_text,
            "license_digest": license_digest,
            "source_sha": args.source_sha,
            "release_id": args.release_id,
            "bundle_version": bundle_version,
            "manifest_digest": manifest_digest,
        }
        if args.command == "enrich":
            if args.output is None:
                raise SbomError("enrich requires --output")
            enriched = enrich_document(document, **common)
            validate_document(enriched, **common)
            _atomic_json(args.output, enriched)
            print(f"SPDX SBOM enriched: {args.output}")
        else:
            if args.output is not None:
                raise SbomError("validate does not accept --output")
            validate_document(document, **common)
            print(f"SPDX SBOM validated: {args.input}")
    except (OSError, UnicodeDecodeError, SbomError) as exc:
        print(f"SPDX SBOM validation failed: {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
