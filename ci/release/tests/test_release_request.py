# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#
from __future__ import annotations

import importlib.util
import json
import tempfile
import unittest
from pathlib import Path

MODULE_PATH = Path(__file__).resolve().parents[1] / "release_request.py"
SPEC = importlib.util.spec_from_file_location("release_request", MODULE_PATH)
assert SPEC and SPEC.loader
release_request = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(release_request)


class ReleaseRequestTest(unittest.TestCase):
    def setUp(self) -> None:
        self.temporary = tempfile.TemporaryDirectory()
        self.root = Path(self.temporary.name).resolve()
        self.requests = self.root / "ci/release/requests"
        self.requests.mkdir(parents=True)
        catalog = self.root / "ci/release/targets.yaml"
        catalog.write_text(
            """---
schemaVersion: 1
targets:
  go-vanity:
    imageRepository: containers/go-vanity
    imageTarget: //services/go_vanity:image
    pushTarget: //services/go_vanity:push
    qualification: [tools/dev/bazelw, test, //services/go_vanity/..., --config=ci]
""",
            encoding="utf-8",
        )
        self.originals = (
            release_request.ROOT,
            release_request.REQUEST_ROOT,
            release_request.CATALOG_PATH,
        )
        release_request.ROOT = self.root
        release_request.REQUEST_ROOT = self.requests.resolve()
        release_request.CATALOG_PATH = catalog

    def tearDown(self) -> None:
        (
            release_request.ROOT,
            release_request.REQUEST_ROOT,
            release_request.CATALOG_PATH,
        ) = self.originals
        self.temporary.cleanup()

    def write_request(self, *, rollback: str | None = None, extra: str = "") -> Path:
        path = self.requests / "v0.2.0.yaml"
        path.write_text(
            """---
apiVersion: release.mindclade.dev/v1alpha1
kind: ReleaseRequest
metadata:
  name: v0.2.0
  changeTicket: PLATFORM-1234
spec:
  targets:
    - name: go-vanity
      rollbackDigest: %s
%s"""
            % (rollback or "sha256:" + "1" * 64, extra),
            encoding="utf-8",
        )
        return path

    def test_valid_request_uses_closed_catalog(self) -> None:
        self.write_request()
        result = release_request.validate_request(
            "ci/release/requests/v0.2.0.yaml", "a" * 40
        )
        self.assertEqual(result["target"], "go-vanity")
        self.assertEqual(result["catalog"]["pushTarget"], "//services/go_vanity:push")

    def test_request_cannot_escape_authorized_directory(self) -> None:
        outside = self.root / "release.yaml"
        outside.write_text("---\n", encoding="utf-8")
        with self.assertRaisesRegex(release_request.ContractError, "below ci/release/requests"):
            release_request.validate_request("release.yaml", "a" * 40)

    def test_zero_rollback_digest_is_rejected(self) -> None:
        self.write_request(rollback="sha256:" + "0" * 64)
        with self.assertRaisesRegex(release_request.ContractError, "zero digest"):
            release_request.validate_request(
                "ci/release/requests/v0.2.0.yaml", "a" * 40
            )

    def test_request_cannot_inject_a_command(self) -> None:
        self.write_request(extra="      command: [sh, -c, whoami]\n")
        with self.assertRaisesRegex(release_request.ContractError, "keys must be exactly"):
            release_request.validate_request(
                "ci/release/requests/v0.2.0.yaml", "a" * 40
            )

    def test_candidate_rejects_tampered_evidence(self) -> None:
        evidence = self.root / "artifact"
        evidence.mkdir()
        sbom = evidence / "sbom.spdx.json"
        provenance = evidence / "provenance.json"
        sbom.write_text("{}\n", encoding="utf-8")
        provenance.write_text("{}\n", encoding="utf-8")
        candidate = evidence / "candidate.json"
        image_digest = "sha256:" + "2" * 64
        candidate.write_text(
            json.dumps(
                {
                    "schemaVersion": 1,
                    "releaseId": "v0.2.0",
                    "changeTicket": "PLATFORM-1234",
                    "sourceSha": "a" * 40,
                    "target": "go-vanity",
                    "rollbackDigest": "sha256:" + "1" * 64,
                    "createdAt": "2026-08-20T00:00:00Z",
                    "artifact": {
                        "imageRef": "us-docker.pkg.dev/example/containers/go@" + image_digest,
                        "digest": image_digest,
                    },
                    "evidence": {
                        "sbom": {"path": sbom.name, "sha256": "0" * 64},
                        "provenance": {
                            "path": provenance.name,
                            "sha256": release_request._sha256(provenance),
                        },
                        "buildAttestor": "projects/example/attestors/build",
                    },
                }
            )
            + "\n",
            encoding="utf-8",
        )
        with self.assertRaisesRegex(release_request.ContractError, "content hash"):
            release_request.validate_candidate(candidate)


if __name__ == "__main__":
    unittest.main()
