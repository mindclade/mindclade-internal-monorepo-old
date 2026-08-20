#!/usr/bin/env python3
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#
"""Fail-closed implementation of the reviewed release-request contract."""

from __future__ import annotations

import argparse
import datetime as dt
import hashlib
import json
import os
import re
import shutil
import subprocess
import sys
from pathlib import Path
from typing import Any

import yaml

ROOT = Path(__file__).resolve().parents[2]
CATALOG_PATH = ROOT / "ci/release/targets.yaml"
REQUEST_ROOT = (ROOT / "ci/release/requests").resolve()
RELEASE_RE = re.compile(r"^v[0-9]+\.[0-9]+\.[0-9]+$")
SHA_RE = re.compile(r"^[0-9a-f]{40}$")
DIGEST_RE = re.compile(r"^sha256:[0-9a-f]{64}$")
TICKET_RE = re.compile(r"^[A-Z][A-Z0-9]+-[1-9][0-9]*$")
HOST_RE = re.compile(r"^[a-z0-9][a-z0-9.-]*-docker\.pkg\.dev$")
PROJECT_RE = re.compile(r"^[a-z][a-z0-9-]{4,28}[a-z0-9]$")
ATTESTOR_RE = re.compile(r"^[a-z][a-z0-9-]{0,61}[a-z0-9]$")
KEY_VERSION_RE = re.compile(
    r"^projects/[a-z][a-z0-9-]{4,28}[a-z0-9]/locations/[a-z0-9-]+/"
    r"keyRings/[A-Za-z0-9_-]+/cryptoKeys/[A-Za-z0-9_-]+/cryptoKeyVersions/[1-9][0-9]*$"
)


class ContractError(ValueError):
    """A reviewed request or generated evidence object violated its contract."""


def _mapping(value: Any, label: str) -> dict[str, Any]:
    if not isinstance(value, dict) or not all(isinstance(key, str) for key in value):
        raise ContractError(f"{label} must be a string-keyed mapping")
    return value


def _exact_keys(value: dict[str, Any], expected: set[str], label: str) -> None:
    if set(value) != expected:
        raise ContractError(
            f"{label} keys must be exactly {sorted(expected)}; got {sorted(value)}"
        )


def _load_yaml(path: Path) -> dict[str, Any]:
    try:
        parsed = yaml.safe_load(path.read_text(encoding="utf-8"))
    except (OSError, yaml.YAMLError) as exc:
        raise ContractError(f"cannot load {path}: {exc}") from exc
    return _mapping(parsed, str(path))


def _load_json(path: Path) -> dict[str, Any]:
    try:
        parsed = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        raise ContractError(f"cannot load {path}: {exc}") from exc
    return _mapping(parsed, str(path))


def _resolve_request(raw_path: str) -> Path:
    path = (ROOT / raw_path).resolve()
    try:
        path.relative_to(REQUEST_ROOT)
    except ValueError as exc:
        raise ContractError("request must be below ci/release/requests") from exc
    if path.parent != REQUEST_ROOT or path.suffix != ".yaml":
        raise ContractError("request must be a direct vX.Y.Z.yaml child")
    return path


def load_catalog() -> dict[str, dict[str, Any]]:
    raw = _load_yaml(CATALOG_PATH)
    _exact_keys(raw, {"schemaVersion", "targets"}, "target catalog")
    if raw["schemaVersion"] != 1:
        raise ContractError("unsupported target catalog schemaVersion")
    targets = _mapping(raw["targets"], "target catalog targets")
    if not targets:
        raise ContractError("target catalog is empty")
    for name, target_raw in targets.items():
        if not re.fullmatch(r"[a-z][a-z0-9-]{1,62}", name):
            raise ContractError(f"invalid catalog target name: {name}")
        target = _mapping(target_raw, f"target {name}")
        _exact_keys(
            target,
            {"imageRepository", "imageTarget", "pushTarget", "qualification"},
            f"target {name}",
        )
        if not re.fullmatch(r"[a-z0-9._/-]+", target["imageRepository"]):
            raise ContractError(f"target {name} has an invalid imageRepository")
        for label in ("imageTarget", "pushTarget"):
            if not isinstance(target[label], str) or not target[label].startswith("//"):
                raise ContractError(f"target {name} has an invalid {label}")
        qualification = target["qualification"]
        if not isinstance(qualification, list) or not qualification:
            raise ContractError(f"target {name} qualification must be a nonempty argv list")
        if not all(isinstance(item, str) and item for item in qualification):
            raise ContractError(f"target {name} qualification argv contains a non-string")
    return targets


def validate_request(raw_path: str, source_sha: str) -> dict[str, Any]:
    if not SHA_RE.fullmatch(source_sha):
        raise ContractError("source-sha must be a lowercase 40-character commit SHA")
    path = _resolve_request(raw_path)
    request = _load_yaml(path)
    _exact_keys(request, {"apiVersion", "kind", "metadata", "spec"}, "request")
    if request["apiVersion"] != "release.mindclade.dev/v1alpha1":
        raise ContractError("unsupported release request apiVersion")
    if request["kind"] != "ReleaseRequest":
        raise ContractError("kind must be ReleaseRequest")

    metadata = _mapping(request["metadata"], "metadata")
    _exact_keys(metadata, {"name", "changeTicket"}, "metadata")
    release_id = metadata["name"]
    if not isinstance(release_id, str) or not RELEASE_RE.fullmatch(release_id):
        raise ContractError("metadata.name must be a full vX.Y.Z release identifier")
    if path.name != f"{release_id}.yaml":
        raise ContractError("request filename must exactly match metadata.name")
    ticket = metadata["changeTicket"]
    if not isinstance(ticket, str) or not TICKET_RE.fullmatch(ticket):
        raise ContractError("metadata.changeTicket must be an immutable ticket identifier")

    spec = _mapping(request["spec"], "spec")
    _exact_keys(spec, {"targets"}, "spec")
    requested_targets = spec["targets"]
    if not isinstance(requested_targets, list) or len(requested_targets) != 1:
        raise ContractError("version 1 requires exactly one release target")
    target = _mapping(requested_targets[0], "spec.targets[0]")
    _exact_keys(target, {"name", "rollbackDigest"}, "spec.targets[0]")
    catalog = load_catalog()
    if target["name"] not in catalog:
        raise ContractError(f"target is not in the closed catalog: {target['name']}")
    rollback = target["rollbackDigest"]
    if not isinstance(rollback, str) or not DIGEST_RE.fullmatch(rollback):
        raise ContractError("rollbackDigest must be a canonical lowercase sha256 digest")
    if rollback == "sha256:" + "0" * 64:
        raise ContractError("rollbackDigest cannot be the zero digest")

    return {
        "path": path,
        "pathRelative": path.relative_to(ROOT).as_posix(),
        "releaseId": release_id,
        "changeTicket": ticket,
        "sourceSha": source_sha,
        "target": target["name"],
        "rollbackDigest": rollback,
        "catalog": catalog[target["name"]],
    }


def _require_env(name: str, pattern: re.Pattern[str]) -> str:
    value = os.environ.get(name, "")
    if not pattern.fullmatch(value):
        raise ContractError(f"{name} is absent or malformed")
    return value


def _require_tool(name: str) -> str:
    path = shutil.which(name)
    if path is None:
        raise ContractError(f"required release tool is unavailable: {name}")
    return path


def _run(argv: list[str], *, capture: bool = False) -> str:
    result = subprocess.run(
        argv,
        cwd=ROOT,
        check=False,
        text=True,
        stdout=subprocess.PIPE if capture else None,
        stderr=subprocess.PIPE if capture else None,
    )
    if result.returncode != 0:
        detail = (result.stderr or result.stdout or "").strip()
        raise ContractError(f"command failed ({result.returncode}): {argv[0]}: {detail}")
    return (result.stdout or "").strip()


def _sha256(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def _write_json(path: Path, value: dict[str, Any]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(
        json.dumps(value, indent=2, sort_keys=True, ensure_ascii=False) + "\n",
        encoding="utf-8",
    )


def _now() -> str:
    return dt.datetime.now(dt.timezone.utc).replace(microsecond=0).isoformat().replace("+00:00", "Z")


def build(request_path: str, source_sha: str, output: Path) -> None:
    contract = validate_request(request_path, source_sha)
    host = _require_env("ARTIFACT_REGISTRY_HOST", HOST_RE)
    project = _require_env("CI_PROJECT_ID", PROJECT_RE)
    attestor_project = _require_env("BINAUTHZ_BUILD_ATTESTOR_PROJECT", PROJECT_RE)
    attestor = _require_env("BINAUTHZ_BUILD_ATTESTOR", ATTESTOR_RE)
    key_version = _require_env("BINAUTHZ_BUILD_ATTESTOR_KEY_VERSION", KEY_VERSION_RE)
    _require_tool("gcloud")
    _require_tool("syft")

    target = contract["catalog"]
    repository = f"{host}/{project}/{target['imageRepository']}"
    tag = f"{contract['releaseId'][1:]}-{source_sha[:12]}"
    _run(["tools/dev/bazelw", "build", target["imageTarget"], "--config=ci"])
    _run(
        [
            "tools/dev/bazelw",
            "run",
            target["pushTarget"],
            "--config=ci",
            "--",
            f"--repository={repository}",
            f"--tag={tag}",
        ]
    )
    digest = _run(
        [
            "gcloud",
            "artifacts",
            "docker",
            "images",
            "describe",
            f"{repository}:{tag}",
            "--format=value(image_summary.digest)",
        ],
        capture=True,
    )
    if not DIGEST_RE.fullmatch(digest):
        raise ContractError("Artifact Registry did not return a canonical image digest")
    if digest == contract["rollbackDigest"]:
        raise ContractError("candidate digest must differ from the reviewed rollback digest")
    image_ref = f"{repository}@{digest}"

    sbom_path = output.parent / "sbom.spdx.json"
    provenance_path = output.parent / "provenance.json"
    _run(["syft", "scan", image_ref, "-o", f"spdx-json={sbom_path}"])
    provenance = {
        "_type": "https://in-toto.io/Statement/v1",
        "predicateType": "https://slsa.dev/provenance/v1",
        "subject": [{"name": repository, "digest": {"sha256": digest.removeprefix("sha256:")}}],
        "predicate": {
            "buildDefinition": {
                "buildType": "https://mindclade.dev/arc/bazel-oci/v1",
                "externalParameters": {
                    "releaseId": contract["releaseId"],
                    "target": contract["target"],
                },
                "resolvedDependencies": [
                    {
                        "uri": "git+https://github.com/mindclade/mindclade-internal-monorepo",
                        "digest": {"gitCommit": source_sha},
                    }
                ],
            },
            "runDetails": {
                "builder": {
                    "id": "https://github.com/mindclade/.github/.github/workflows/"
                    "reusable-arc-oci-build.yml@v4.0.0"
                },
                "metadata": {"invocationId": os.environ.get("GITHUB_RUN_ID", "connected-run")},
            },
        },
    }
    _write_json(provenance_path, provenance)
    if not sbom_path.is_file() or sbom_path.stat().st_size == 0:
        raise ContractError("syft did not create a nonempty SBOM")

    _run(
        [
            "gcloud",
            "beta",
            "container",
            "binauthz",
            "attestations",
            "sign-and-create",
            f"--project={attestor_project}",
            f"--artifact-url={image_ref}",
            f"--attestor={attestor}",
            f"--attestor-project={attestor_project}",
            f"--keyversion={key_version}",
            "--validate",
            "--quiet",
        ]
    )
    candidate = {
        "schemaVersion": 1,
        "releaseId": contract["releaseId"],
        "changeTicket": contract["changeTicket"],
        "sourceSha": source_sha,
        "target": contract["target"],
        "rollbackDigest": contract["rollbackDigest"],
        "createdAt": _now(),
        "artifact": {"imageRef": image_ref, "digest": digest},
        "evidence": {
            "sbom": {"path": sbom_path.name, "sha256": _sha256(sbom_path)},
            "provenance": {
                "path": provenance_path.name,
                "sha256": _sha256(provenance_path),
            },
            "buildAttestor": f"projects/{attestor_project}/attestors/{attestor}",
        },
    }
    _write_json(output, candidate)
    validate_candidate(output)


def validate_candidate(path: Path) -> dict[str, Any]:
    candidate = _load_json(path)
    _exact_keys(
        candidate,
        {
            "schemaVersion",
            "releaseId",
            "changeTicket",
            "sourceSha",
            "target",
            "rollbackDigest",
            "createdAt",
            "artifact",
            "evidence",
        },
        "candidate",
    )
    if candidate["schemaVersion"] != 1:
        raise ContractError("unsupported candidate schemaVersion")
    if not RELEASE_RE.fullmatch(str(candidate["releaseId"])):
        raise ContractError("candidate releaseId is malformed")
    if not SHA_RE.fullmatch(str(candidate["sourceSha"])):
        raise ContractError("candidate sourceSha is malformed")
    if candidate["target"] not in load_catalog():
        raise ContractError("candidate target is not in the closed catalog")
    if not DIGEST_RE.fullmatch(str(candidate["rollbackDigest"])):
        raise ContractError("candidate rollbackDigest is malformed")
    artifact = _mapping(candidate["artifact"], "candidate artifact")
    _exact_keys(artifact, {"imageRef", "digest"}, "candidate artifact")
    if not DIGEST_RE.fullmatch(str(artifact["digest"])):
        raise ContractError("candidate artifact digest is malformed")
    if not str(artifact["imageRef"]).endswith("@" + artifact["digest"]):
        raise ContractError("candidate imageRef and digest disagree")
    if artifact["digest"] == candidate["rollbackDigest"]:
        raise ContractError("candidate and rollback digests must differ")
    evidence = _mapping(candidate["evidence"], "candidate evidence")
    _exact_keys(evidence, {"sbom", "provenance", "buildAttestor"}, "candidate evidence")
    for label in ("sbom", "provenance"):
        record = _mapping(evidence[label], f"candidate {label}")
        _exact_keys(record, {"path", "sha256"}, f"candidate {label}")
        evidence_path = (path.parent / str(record["path"])).resolve()
        if evidence_path.parent != path.parent.resolve() or not evidence_path.is_file():
            raise ContractError(f"candidate {label} file is absent or escapes its artifact")
        if not re.fullmatch(r"[0-9a-f]{64}", str(record["sha256"])):
            raise ContractError(f"candidate {label} SHA-256 is malformed")
        if _sha256(evidence_path) != record["sha256"]:
            raise ContractError(f"candidate {label} content hash does not match")
    return candidate


def qualify(candidate_path: Path, expected_image_ref: str, output: Path) -> None:
    candidate = validate_candidate(candidate_path)
    if candidate["artifact"]["imageRef"] != expected_image_ref:
        raise ContractError("workflow image-ref does not match candidate evidence")
    _require_tool("crane")
    registry_digest = _run(["crane", "digest", expected_image_ref], capture=True)
    if registry_digest != candidate["artifact"]["digest"]:
        raise ContractError("registry content does not match the candidate digest")
    command = load_catalog()[candidate["target"]]["qualification"]
    _run(command)
    result = {
        "schemaVersion": 1,
        "passed": True,
        "releaseId": candidate["releaseId"],
        "sourceSha": candidate["sourceSha"],
        "target": candidate["target"],
        "qualifiedAt": _now(),
        "candidateSha256": _sha256(candidate_path),
        "artifact": {"imageRef": expected_image_ref, "digest": registry_digest},
    }
    _write_json(output, result)
    validate_qualification(output, expected_image_ref)


def validate_qualification(path: Path, expected_image_ref: str) -> dict[str, Any]:
    result = _load_json(path)
    _exact_keys(
        result,
        {
            "schemaVersion",
            "passed",
            "releaseId",
            "sourceSha",
            "target",
            "qualifiedAt",
            "candidateSha256",
            "artifact",
        },
        "qualification result",
    )
    if result["schemaVersion"] != 1 or result["passed"] is not True:
        raise ContractError("qualification result is not a passing version 1 result")
    if not RELEASE_RE.fullmatch(str(result["releaseId"])):
        raise ContractError("qualification releaseId is malformed")
    if not SHA_RE.fullmatch(str(result["sourceSha"])):
        raise ContractError("qualification sourceSha is malformed")
    if result["target"] not in load_catalog():
        raise ContractError("qualification target is not in the closed catalog")
    if not re.fullmatch(r"[0-9a-f]{64}", str(result["candidateSha256"])):
        raise ContractError("qualification candidateSha256 is malformed")
    artifact = _mapping(result["artifact"], "qualification artifact")
    _exact_keys(artifact, {"imageRef", "digest"}, "qualification artifact")
    if artifact["imageRef"] != expected_image_ref:
        raise ContractError("qualification result covers a different image")
    if not DIGEST_RE.fullmatch(str(artifact["digest"])):
        raise ContractError("qualification digest is malformed")
    if not expected_image_ref.endswith("@" + artifact["digest"]):
        raise ContractError("qualification imageRef and digest disagree")
    return result


def inspect_request(request_path: str, source_sha: str, github_output: Path) -> None:
    contract = validate_request(request_path, source_sha)
    values = {
        "request-path": contract["pathRelative"],
        "release-id": contract["releaseId"],
        "target": contract["target"],
        "rollback-digest": contract["rollbackDigest"],
    }
    with github_output.open("a", encoding="utf-8") as stream:
        for key, value in values.items():
            stream.write(f"{key}={value}\n")


def parser() -> argparse.ArgumentParser:
    result = argparse.ArgumentParser(description=__doc__)
    commands = result.add_subparsers(dest="command", required=True)
    validate = commands.add_parser("validate")
    validate.add_argument("--request", required=True)
    validate.add_argument("--source-sha", required=True)
    inspect = commands.add_parser("inspect")
    inspect.add_argument("--request", required=True)
    inspect.add_argument("--source-sha", required=True)
    inspect.add_argument("--github-output", type=Path, required=True)
    build_command = commands.add_parser("build")
    build_command.add_argument("--request", required=True)
    build_command.add_argument("--source-sha", required=True)
    build_command.add_argument("--output", type=Path, required=True)
    candidate = commands.add_parser("validate-candidate")
    candidate.add_argument("--candidate", type=Path, required=True)
    qualify_command = commands.add_parser("qualify")
    qualify_command.add_argument("--candidate", type=Path, required=True)
    qualify_command.add_argument("--expected-image-ref", required=True)
    qualify_command.add_argument("--output", type=Path, required=True)
    qualification = commands.add_parser("validate-qualification")
    qualification.add_argument("--qualification", type=Path, required=True)
    qualification.add_argument("--expected-image-ref", required=True)
    return result


def main() -> int:
    arguments = parser().parse_args()
    try:
        if arguments.command == "validate":
            validate_request(arguments.request, arguments.source_sha)
        elif arguments.command == "inspect":
            inspect_request(arguments.request, arguments.source_sha, arguments.github_output)
        elif arguments.command == "build":
            build(arguments.request, arguments.source_sha, arguments.output)
        elif arguments.command == "validate-candidate":
            validate_candidate(arguments.candidate)
        elif arguments.command == "qualify":
            qualify(arguments.candidate, arguments.expected_image_ref, arguments.output)
        elif arguments.command == "validate-qualification":
            validate_qualification(arguments.qualification, arguments.expected_image_ref)
    except ContractError as exc:
        print(f"release request rejected: {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
