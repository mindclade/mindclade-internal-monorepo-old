# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

from __future__ import annotations

import copy
import hashlib
import json
from pathlib import Path

import pytest

from tools.release.sbom import (
    LICENSE_ID,
    PACKAGE_ID,
    SbomError,
    enrich_document,
    validate_document,
)

ROOT = Path(__file__).resolve().parents[3]
LICENSE_TEXT = (ROOT / "LICENSE").read_text(encoding="utf-8")
LICENSE_DIGEST = hashlib.sha256(LICENSE_TEXT.encode("utf-8")).hexdigest()
MANIFEST = ROOT / "contracts" / "policy-bundle" / "manifest.json"
MANIFEST_VALUE = json.loads(MANIFEST.read_text(encoding="utf-8"))
MANIFEST_DIGEST = hashlib.sha256(MANIFEST.read_bytes()).hexdigest()


def base_document() -> dict[str, object]:
    return {
        "spdxVersion": "SPDX-2.3",
        "dataLicense": "CC0-1.0",
        "SPDXID": "SPDXRef-DOCUMENT",
        "name": "generated",
        "documentNamespace": "https://example.invalid/spdx/generated",
        "creationInfo": {
            "created": "2026-08-21T00:00:00Z",
            "creators": ["Tool: test"],
        },
        "packages": [],
        "relationships": [],
    }


def contract() -> dict[str, str]:
    return {
        "license_text": LICENSE_TEXT,
        "license_digest": LICENSE_DIGEST,
        "source_sha": "a" * 40,
        "release_id": "v1.2.3",
        "bundle_version": MANIFEST_VALUE["version"],
        "manifest_digest": MANIFEST_DIGEST,
    }


def test_enrichment_is_idempotent_and_complete() -> None:
    document = enrich_document(base_document(), **contract())
    validate_document(document, **contract())
    second = enrich_document(copy.deepcopy(document), **contract())
    assert second == document
    extracted = document["hasExtractedLicensingInfos"]
    assert extracted[0]["licenseId"] == LICENSE_ID
    assert extracted[0]["extractedText"] == LICENSE_TEXT
    assert LICENSE_DIGEST in extracted[0]["comment"]
    package = next(item for item in document["packages"] if item["SPDXID"] == PACKAGE_ID)
    assert package["licenseDeclared"] == LICENSE_ID
    assert package["licenseConcluded"] == LICENSE_ID


def test_wrong_license_text_or_spdx_version_fails_closed() -> None:
    bad_version = base_document()
    bad_version["spdxVersion"] = "SPDX-2.2"
    with pytest.raises(SbomError, match=r"SPDX-2\.3"):
        enrich_document(bad_version, **contract())
    invalid = contract()
    invalid["license_text"] = "abbreviated\n"
    with pytest.raises(SbomError, match="complete"):
        enrich_document(base_document(), **invalid)


def test_tampered_extracted_license_fails_validation() -> None:
    document = enrich_document(base_document(), **contract())
    document["hasExtractedLicensingInfos"][0]["extractedText"] = "tampered"
    with pytest.raises(SbomError, match="absent or stale"):
        validate_document(document, **contract())
