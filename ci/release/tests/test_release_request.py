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
schemaVersion: 2
targets:
  go-vanity:
    releaseKind: application
    application: platform-go-vanity
    rolloutClass: stateless
    images:
      primary:
        repository: releases/go-vanity
        buildTarget: //services/go_vanity:image
        pushTarget: //services/go_vanity:push
    artifacts: []
    qualificationMode: test
    qualificationTargets: [//services/go_vanity/...]
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

    def write_request(
        self,
        *,
        previous_id: str = "v0.1.0",
        previous_digest: str | None = None,
        extra: str = "",
    ) -> Path:
        path = self.requests / "v0.2.0.yaml"
        path.write_text(
            """---
apiVersion: release.mindclade.dev/v1beta1
kind: ReleaseRequest
metadata:
  name: v0.2.0
  changeTicket: PLATFORM-1234
spec:
  target: go-vanity
  previousRelease:
    id: %s
    subjectDigest: %s
%s"""
            % (
                previous_id,
                previous_digest or "sha256:" + "1" * 64,
                extra,
            ),
            encoding="utf-8",
        )
        return path

    def test_valid_request_uses_closed_catalog(self) -> None:
        self.write_request()
        result = release_request.validate_request(
            "ci/release/requests/v0.2.0.yaml", "a" * 40
        )
        self.assertEqual(result["target"], "go-vanity")
        self.assertEqual(result["previousReleaseId"], "v0.1.0")
        self.assertEqual(
            result["catalog"]["images"]["primary"]["pushTarget"],
            "//services/go_vanity:push",
        )

    def test_inspect_exports_exact_catalog_identity_and_lineage(self) -> None:
        self.write_request()
        output = self.root / "github-output"
        release_request.inspect_request(
            "ci/release/requests/v0.2.0.yaml", "a" * 40, output
        )
        values = dict(
            line.split("=", 1)
            for line in output.read_text(encoding="utf-8").splitlines()
        )
        self.assertEqual(values["application"], "platform-go-vanity")
        self.assertEqual(values["release-kind"], "application")
        self.assertEqual(values["rollout-class"], "stateless")
        self.assertEqual(values["previous-release-id"], "v0.1.0")
        self.assertEqual(values["previous-subject-digest"], "sha256:" + "1" * 64)

    def test_request_cannot_escape_authorized_directory(self) -> None:
        outside = self.root / "release.yaml"
        outside.write_text("---\n", encoding="utf-8")
        with self.assertRaisesRegex(release_request.ContractError, "below ci/release/requests"):
            release_request.validate_request("release.yaml", "a" * 40)

    def test_zero_previous_subject_digest_is_rejected(self) -> None:
        self.write_request(previous_digest="sha256:" + "0" * 64)
        with self.assertRaisesRegex(release_request.ContractError, "zero digest"):
            release_request.validate_request(
                "ci/release/requests/v0.2.0.yaml", "a" * 40
            )

    def test_request_cannot_inject_a_command(self) -> None:
        self.write_request(extra="  command: [sh, -c, whoami]\n")
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
                    "schemaVersion": 2,
                    "releaseKind": "application",
                    "application": "platform-go-vanity",
                    "rolloutClass": "stateless",
                    "releaseId": "v0.2.0",
                    "changeTicket": "PLATFORM-1234",
                    "sourceSha": "a" * 40,
                    "target": "go-vanity",
                    "previousReleaseId": "v0.1.0",
                    "previousSubjectDigest": "sha256:" + "1" * 64,
                    "createdAt": "2026-08-20T00:00:00Z",
                    "artifact": {
                        "imageRef": "us-docker.pkg.dev/example/releases/go-vanity@" + image_digest,
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

    def test_previous_release_id_must_be_older_than_candidate(self) -> None:
        self.write_request(previous_id="v0.2.0")
        with self.assertRaisesRegex(release_request.ContractError, "must be older"):
            release_request.validate_request(
                "ci/release/requests/v0.2.0.yaml", "a" * 40
            )
        self.write_request(previous_id="v0.3.0")
        with self.assertRaisesRegex(release_request.ContractError, "must be older"):
            release_request.validate_request(
                "ci/release/requests/v0.2.0.yaml", "a" * 40
            )

    def test_catalog_cannot_inject_a_qualification_command(self) -> None:
        catalog = self.root / "ci/release/targets.yaml"
        catalog.write_text(
            catalog.read_text(encoding="utf-8")
            + "    command: [sh, -c, whoami]\n",
            encoding="utf-8",
        )
        self.write_request()
        with self.assertRaisesRegex(release_request.ContractError, "keys must be exactly"):
            release_request.validate_request(
                "ci/release/requests/v0.2.0.yaml", "a" * 40
            )


if __name__ == "__main__":
    unittest.main()
