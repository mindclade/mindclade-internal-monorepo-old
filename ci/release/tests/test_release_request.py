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
SCHEMA_PATH = Path(__file__).resolve().parents[1] / "release-request.schema.json"
SPEC = importlib.util.spec_from_file_location("release_request", MODULE_PATH)
assert SPEC and SPEC.loader
release_request = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(release_request)


def _materialize_packages(root: Path, labels: list[str]) -> None:
    """Create the package directories a set of catalog labels names, under `root`."""
    for label in labels:
        package = root / release_request._label_package(label)
        package.mkdir(parents=True, exist_ok=True)
        (package / "BUILD.bazel").write_text("", encoding="utf-8")


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
        # The catalog names Bazel packages and `validate_request` now resolves them, so the
        # fixture root has to carry the package the fixture catalog points at. Without it every
        # request in this class would be rejected for a reason the test is not about.
        _materialize_packages(self.root, ["//services/go_vanity:image"])
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
        release_id: str = "v0.2.0",
        strategy: str = "previous-release",
        previous_id: str = "v0.1.0",
        previous_digest: str | None = None,
        extra: str = "",
    ) -> Path:
        path = self.requests / f"{release_id}.yaml"
        previous = ""
        if strategy == "previous-release":
            previous = f"""    previousRelease:
      id: {previous_id}
      subjectDigest: {previous_digest or "sha256:" + "1" * 64}
"""
        path.write_text(
            f"""---
apiVersion: release.mindclade.dev/v1beta2
kind: ReleaseRequest
metadata:
  name: {release_id}
  changeTicket: PLATFORM-1234
spec:
  target: go-vanity
  rollback:
    strategy: {strategy}
{previous}
{extra}""",
            encoding="utf-8",
        )
        return path

    def test_valid_request_uses_closed_catalog(self) -> None:
        self.write_request()
        result = release_request.validate_request("ci/release/requests/v0.2.0.yaml", "a" * 40)
        self.assertEqual(result["target"], "go-vanity")
        self.assertEqual(result["previousReleaseId"], "v0.1.0")
        self.assertEqual(
            result["catalog"]["images"]["primary"]["pushTarget"],
            "//services/go_vanity:push",
        )

    def test_json_schema_matches_runtime_contract(self) -> None:
        schema = json.loads(SCHEMA_PATH.read_text(encoding="utf-8"))
        self.assertEqual(
            schema["properties"]["apiVersion"]["const"],
            "release.mindclade.dev/v1beta2",
        )
        spec = schema["properties"]["spec"]
        self.assertEqual(set(spec["required"]), {"target", "rollback"})
        rollback = spec["properties"]["rollback"]
        self.assertEqual(set(rollback["required"]), {"strategy"})
        self.assertEqual(
            set(rollback["properties"]["strategy"]["enum"]),
            {"bootstrap", "previous-release"},
        )
        previous = rollback["properties"]["previousRelease"]
        self.assertEqual(set(previous["required"]), {"id", "subjectDigest"})

    def test_inspect_exports_exact_catalog_identity_and_lineage(self) -> None:
        self.write_request()
        output = self.root / "github-output"
        release_request.inspect_request("ci/release/requests/v0.2.0.yaml", "a" * 40, output)
        values = dict(
            line.split("=", 1) for line in output.read_text(encoding="utf-8").splitlines()
        )
        self.assertEqual(values["application"], "platform-go-vanity")
        self.assertEqual(values["release-kind"], "application")
        self.assertEqual(values["rollout-class"], "stateless")
        self.assertEqual(values["rollback-strategy"], "previous-release")
        self.assertEqual(values["previous-release-id"], "v0.1.0")
        self.assertEqual(values["previous-subject-digest"], "sha256:" + "1" * 64)
        self.assertNotIn("rollback-digest", values)

    def test_request_cannot_escape_authorized_directory(self) -> None:
        outside = self.root / "release.yaml"
        outside.write_text("---\n", encoding="utf-8")
        with self.assertRaisesRegex(release_request.ContractError, "below ci/release/requests"):
            release_request.validate_request("release.yaml", "a" * 40)

    def test_rehearsal_validates_a_request_outside_the_armed_directory(self) -> None:
        # The point of the mode: the bytes an author intends to commit are checkable while
        # they are still outside the directory whose merge starts the release.
        outside = self.root / "v0.2.0.yaml"
        outside.write_text((self.write_request()).read_text(encoding="utf-8"), encoding="utf-8")
        (self.requests / "v0.2.0.yaml").unlink()
        with self.assertRaisesRegex(release_request.ContractError, "below ci/release/requests"):
            release_request.validate_request("v0.2.0.yaml", "a" * 40)
        result = release_request.validate_request("v0.2.0.yaml", "a" * 40, rehearsal=True)
        self.assertEqual(result["target"], "go-vanity")

    def test_rehearsal_accepts_a_request_outside_the_repository(self) -> None:
        """The realistic case, and the one the in-tree fixtures hid.

        An author drafts the request somewhere scratch before committing it. `pathRelative`
        is built with `relative_to(ROOT)`, which raises rather than returning a fallback, so
        this path escaped every fixture that wrote inside the fake root.
        """
        with tempfile.TemporaryDirectory() as elsewhere:
            outside = Path(elsewhere).resolve() / "v0.2.0.yaml"
            outside.write_text((self.write_request()).read_text(encoding="utf-8"), encoding="utf-8")
            result = release_request.validate_request(str(outside), "a" * 40, rehearsal=True)
            self.assertEqual(result["target"], "go-vanity")
            self.assertEqual(result["pathRelative"], outside.as_posix())

    def test_rehearsal_relaxes_only_the_directory(self) -> None:
        """A rehearsal that passed for a reason a merged request would not is worthless."""
        outside = self.root / "v0.2.0.yaml"
        for body, expected in (
            ("go-vanity-staging", "not in the closed catalog"),
            ("//services/attacker:push", "not in the closed catalog"),
        ):
            with self.subTest(target=body):
                outside.write_text(
                    f"""---
apiVersion: release.mindclade.dev/v1beta2
kind: ReleaseRequest
metadata:
  name: v0.2.0
  changeTicket: PLATFORM-1234
spec:
  target: {body}
  rollback:
    strategy: previous-release
    previousRelease:
      id: v0.1.0
      subjectDigest: sha256:{"1" * 64}
""",
                    encoding="utf-8",
                )
                with self.assertRaisesRegex(release_request.ContractError, expected):
                    release_request.validate_request("v0.2.0.yaml", "a" * 40, rehearsal=True)

        # The filename/metadata.name agreement is a content rule, not a directory rule.
        misnamed = self.root / "v9.9.9.yaml"
        misnamed.write_text(
            f"""---
apiVersion: release.mindclade.dev/v1beta2
kind: ReleaseRequest
metadata:
  name: v0.2.0
  changeTicket: PLATFORM-1234
spec:
  target: go-vanity
  rollback:
    strategy: previous-release
    previousRelease:
      id: v0.1.0
      subjectDigest: sha256:{"1" * 64}
""",
            encoding="utf-8",
        )
        with self.assertRaisesRegex(release_request.ContractError, "filename must exactly match"):
            release_request.validate_request("v9.9.9.yaml", "a" * 40, rehearsal=True)

    def test_rehearsal_does_not_reach_the_authority_path(self) -> None:
        """`inspect` and `build` are what the workflow calls; neither takes the relaxation.

        A rehearsal grants no authority. If it could reach `inspect`, a release could act on
        a file that never passed through the reviewed directory.
        """
        outside = self.root / "v0.2.0.yaml"
        outside.write_text((self.write_request()).read_text(encoding="utf-8"), encoding="utf-8")
        (self.requests / "v0.2.0.yaml").unlink()
        with self.assertRaisesRegex(release_request.ContractError, "below ci/release/requests"):
            release_request.inspect_request("v0.2.0.yaml", "a" * 40, self.root / "out")

        # Only `validate` accepts the flag; the two commands the workflow invokes reject it
        # at the command line rather than quietly ignoring it.
        parser = release_request.parser()
        self.assertTrue(
            parser.parse_args(
                ["validate", "--request", "r.yaml", "--source-sha", "a" * 40, "--rehearsal"]
            ).rehearsal
        )
        for command in ("inspect", "build"):
            with self.subTest(command=command), self.assertRaises(SystemExit):
                parser.parse_args(
                    [command, "--request", "r.yaml", "--source-sha", "a" * 40, "--rehearsal"]
                )

    def test_oversized_request_is_refused_before_parsing(self) -> None:
        """An unbounded read is the whole file in memory before a single field is checked."""
        path = self.requests / "v0.2.0.yaml"
        path.write_text("#" + "x" * (release_request.MAX_YAML_BYTES + 16), encoding="utf-8")
        with self.assertRaisesRegex(release_request.ContractError, "exceeds the .* bound"):
            release_request.validate_request("ci/release/requests/v0.2.0.yaml", "a" * 40)

    def test_request_bound_counts_utf8_bytes_not_characters(self) -> None:
        path = self.requests / "v0.2.0.yaml"
        path.write_text("#" + "é" * release_request.MAX_YAML_BYTES, encoding="utf-8")
        with self.assertRaisesRegex(release_request.ContractError, "exceeds the .* bound"):
            release_request.validate_request("ci/release/requests/v0.2.0.yaml", "a" * 40)

    def test_zero_previous_subject_digest_is_rejected(self) -> None:
        self.write_request(previous_digest="sha256:" + "0" * 64)
        with self.assertRaisesRegex(release_request.ContractError, "zero digest"):
            release_request.validate_request("ci/release/requests/v0.2.0.yaml", "a" * 40)

    def test_first_release_uses_bootstrap_rollback(self) -> None:
        self.write_request(release_id="v1.0.0", strategy="bootstrap")
        result = release_request.validate_request("ci/release/requests/v1.0.0.yaml", "a" * 40)
        self.assertEqual(result["rollbackStrategy"], "bootstrap")
        self.assertIsNone(result["previousReleaseId"])
        output = self.root / "github-output"
        release_request.inspect_request("ci/release/requests/v1.0.0.yaml", "a" * 40, output)
        values = dict(
            line.split("=", 1) for line in output.read_text(encoding="utf-8").splitlines()
        )
        self.assertEqual(values["rollback-strategy"], "bootstrap")
        self.assertEqual(values["previous-release-id"], "")
        self.assertEqual(values["previous-subject-digest"], "")

    def test_bootstrap_rollback_is_rejected_after_first_release(self) -> None:
        self.write_request(strategy="bootstrap")
        with self.assertRaisesRegex(release_request.ContractError, "first v1.0.0"):
            release_request.validate_request("ci/release/requests/v0.2.0.yaml", "a" * 40)

    def test_request_cannot_inject_a_command(self) -> None:
        self.write_request(extra="  command: [sh, -c, whoami]\n")
        with self.assertRaisesRegex(release_request.ContractError, "keys must be exactly"):
            release_request.validate_request("ci/release/requests/v0.2.0.yaml", "a" * 40)

    def test_candidate_rejects_tampered_evidence(self) -> None:
        evidence = self.root / "artifact"
        evidence.mkdir()
        sbom = evidence / "sbom.spdx.json"
        provenance = evidence / "provenance.json"
        vulnerability = evidence / "vulnerability.json"
        rollback = evidence / "rollback.json"
        sbom.write_text("{}\n", encoding="utf-8")
        provenance.write_text("{}\n", encoding="utf-8")
        vulnerability.write_text("{}\n", encoding="utf-8")
        rollback.write_text("{}\n", encoding="utf-8")
        candidate = evidence / "candidate.json"
        image_digest = "sha256:" + "2" * 64
        candidate.write_text(
            json.dumps(
                {
                    "schemaVersion": 4,
                    "releaseKind": "application",
                    "application": "platform-go-vanity",
                    "rolloutClass": "stateless",
                    "releaseId": "v0.2.0",
                    "changeTicket": "PLATFORM-1234",
                    "sourceSha": "a" * 40,
                    "target": "go-vanity",
                    "rollback": {
                        "schemaVersion": 1,
                        "releaseId": "v0.2.0",
                        "strategy": "previous-release",
                        "previousReleaseId": "v0.1.0",
                        "previousSubjectDigest": "sha256:" + "1" * 64,
                        "bootstrapAction": None,
                    },
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
                        "vulnerability": {
                            "path": vulnerability.name,
                            "sha256": release_request._sha256(vulnerability),
                        },
                        "rollback": {
                            "path": rollback.name,
                            "sha256": release_request._sha256(rollback),
                        },
                        "thirdPartyNotices": {
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
            release_request.validate_request("ci/release/requests/v0.2.0.yaml", "a" * 40)
        self.write_request(previous_id="v0.3.0")
        with self.assertRaisesRegex(release_request.ContractError, "must be older"):
            release_request.validate_request("ci/release/requests/v0.2.0.yaml", "a" * 40)

    def test_catalog_label_naming_a_missing_package_is_rejected(self) -> None:
        catalog = self.root / "ci/release/targets.yaml"
        catalog.write_text(
            catalog.read_text(encoding="utf-8").replace(
                "//services/go_vanity:image", "//services/go_vanity_renamed:image"
            ),
            encoding="utf-8",
        )
        self.write_request()
        with self.assertRaisesRegex(release_request.ContractError, "missing package directory"):
            release_request.validate_request("ci/release/requests/v0.2.0.yaml", "a" * 40)

    def test_catalog_label_without_a_build_file_is_rejected(self) -> None:
        (self.root / "services/go_vanity/BUILD.bazel").unlink()
        self.write_request()
        with self.assertRaisesRegex(release_request.ContractError, "no BUILD.bazel"):
            release_request.validate_request("ci/release/requests/v0.2.0.yaml", "a" * 40)

    def test_catalog_label_cannot_escape_the_repository(self) -> None:
        catalog = self.root / "ci/release/targets.yaml"
        catalog.write_text(
            catalog.read_text(encoding="utf-8").replace(
                "//services/go_vanity:push", "//../../etc:push"
            ),
            encoding="utf-8",
        )
        self.write_request()
        with self.assertRaisesRegex(release_request.ContractError, "does not name a package"):
            release_request.validate_request("ci/release/requests/v0.2.0.yaml", "a" * 40)

    def test_catalog_cannot_inject_a_qualification_command(self) -> None:
        catalog = self.root / "ci/release/targets.yaml"
        catalog.write_text(
            catalog.read_text(encoding="utf-8") + "    command: [sh, -c, whoami]\n",
            encoding="utf-8",
        )
        self.write_request()
        with self.assertRaisesRegex(release_request.ContractError, "keys must be exactly"):
            release_request.validate_request("ci/release/requests/v0.2.0.yaml", "a" * 40)


class SharedWorkflowVersionContractTest(unittest.TestCase):
    def test_release_caller_uses_the_exact_phased_workflow_versions(self) -> None:
        root = Path(__file__).resolve().parents[3]
        workflow = (root / ".github/workflows/release.yml").read_text(encoding="utf-8")
        expected = {
            "reusable-arc-wif-canary.yml": "v5.0.0",
            "reusable-arc-oci-build.yml": "v5.0.0",
            "reusable-arc-oci-qualify.yml": "v5.0.0",
            "reusable-arc-qualification-attest.yml": "v5.0.0",
            "reusable-binauthz-sign.yml": "v5.0.0",
            "reusable-gitops-promote.yml": "v5.0.0",
        }
        for name, version in expected.items():
            self.assertEqual(workflow.count(f"{name}@{version}"), 1, name)
        self.assertIn(
            "producer-evidence-digest: ${{ needs.qualify.outputs.producer-evidence-digest }}",
            workflow,
        )


class ProductionCatalogContractTest(unittest.TestCase):
    def test_protobuf_contract_bundle_is_a_closed_release_target(self) -> None:
        target = release_request.load_catalog()["protobuf-contracts"]
        self.assertEqual(target["releaseKind"], "bundle")
        self.assertEqual(target["rolloutClass"], "platform")
        self.assertEqual(
            target["images"]["primary"],
            {
                "repository": "releases/protobuf-contracts",
                "buildTarget": "//protocols:protobuf_contract_image",
                "pushTarget": "//protocols:protobuf_contract_push",
            },
        )
        self.assertEqual(
            target["qualificationTargets"],
            [
                "//protocols:protobuf_governance_test",
                "//protocols:typescript_projection_test",
                "//protocols/consumers:generated_go_test",
            ],
        )

    def test_every_committed_catalog_label_resolves_to_a_real_package(self) -> None:
        """The committed catalog may not name a package that is not in this tree.

        A catalog entry is a promise that the release path can build the thing it names, and
        until this ran the promise was checked for the first time by `bazel build` inside the
        credentialed build job. Nothing in this repository has been released, so no entry has
        ever been exercised end to end; this is what stands in for that until one is.
        """
        release_request.resolve_catalog_packages(release_request.load_catalog())

    def test_json_schema_target_enum_matches_the_committed_catalog(self) -> None:
        """The schema's `target` enum is a hand-maintained mirror of the catalog keys.

        `release_request.py` never reads the schema — it validates against `targets.yaml`
        directly — so the two can disagree silently, and a request naming a real catalog entry
        would then be accepted by the workflow and rejected by any editor or reviewer running
        the published schema. Binding them here makes adding a target a two-file change that
        fails loudly rather than a one-file change that drifts.
        """
        schema = json.loads(SCHEMA_PATH.read_text(encoding="utf-8"))
        self.assertEqual(
            set(schema["properties"]["spec"]["properties"]["target"]["enum"]),
            set(release_request.load_catalog()),
        )


class WorkedRequestTest(unittest.TestCase):
    """Every committed catalog name is requestable, and nothing outside the catalog is.

    Deliberately exercised here rather than by committing a request file: adding one under
    `ci/release/requests/` is not an example, it is the trigger — `.github/workflows/release.yml`
    fires on a push to protected `main` that adds exactly that path, and would run the canary,
    build, push, sign, and promotion proposal for real.
    """

    def setUp(self) -> None:
        committed = Path(__file__).resolve().parents[1] / "targets.yaml"
        self.catalog_names = sorted(release_request.load_catalog())
        self.temporary = tempfile.TemporaryDirectory()
        self.root = Path(self.temporary.name).resolve()
        self.requests = self.root / "ci/release/requests"
        self.requests.mkdir(parents=True)
        catalog = self.root / "ci/release/targets.yaml"
        catalog.write_text(committed.read_text(encoding="utf-8"), encoding="utf-8")
        self.originals = (
            release_request.ROOT,
            release_request.REQUEST_ROOT,
            release_request.CATALOG_PATH,
        )
        release_request.ROOT = self.root
        release_request.REQUEST_ROOT = self.requests.resolve()
        release_request.CATALOG_PATH = catalog
        labels = []
        for target in release_request.load_catalog().values():
            image = target["images"]["primary"]
            labels += [image["buildTarget"], image["pushTarget"], *target["qualificationTargets"]]
        _materialize_packages(self.root, labels)

    def tearDown(self) -> None:
        (
            release_request.ROOT,
            release_request.REQUEST_ROOT,
            release_request.CATALOG_PATH,
        ) = self.originals
        self.temporary.cleanup()

    def _write(self, target: str) -> str:
        (self.requests / "v0.2.0.yaml").write_text(
            f"""---
apiVersion: release.mindclade.dev/v1beta2
kind: ReleaseRequest
metadata:
  name: v0.2.0
  changeTicket: PLATFORM-1234
spec:
  target: {target}
  rollback:
    strategy: previous-release
    previousRelease:
      id: v0.1.0
      subjectDigest: sha256:{"1" * 64}
""",
            encoding="utf-8",
        )
        return "ci/release/requests/v0.2.0.yaml"

    def test_every_committed_catalog_target_validates(self) -> None:
        self.assertTrue(self.catalog_names)
        catalog = release_request.load_catalog()
        for name in self.catalog_names:
            with self.subTest(target=name):
                result = release_request.validate_request(self._write(name), "a" * 40)
                self.assertEqual(result["target"], name)
                # The contract hands the workflow the catalog entry verbatim; a request that
                # validated but resolved to a different entry would push the wrong subject.
                self.assertEqual(result["catalog"], catalog[name])

    def test_a_target_outside_the_catalog_is_rejected(self) -> None:
        path = self._write("go-vanity-staging")
        with self.assertRaisesRegex(release_request.ContractError, "not in the closed catalog"):
            release_request.validate_request(path, "a" * 40)


if __name__ == "__main__":
    unittest.main()
