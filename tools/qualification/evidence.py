# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Semantic validation and GitOps handoff for immutable release evidence."""

from __future__ import annotations

import copy
import datetime as dt
import hashlib
import json
import re
from pathlib import Path
from typing import Any, cast

from configs.contract_validation import load_json, validate

MAXIMUM_EVIDENCE_BYTES = 4 << 20
_DIGEST = re.compile(r"sha256:[0-9a-f]{64}")
_PROJECT = re.compile(r"[a-z][a-z0-9-]{4,28}[a-z0-9]")
_ATTESTOR = re.compile(r"[a-z][a-z0-9-]{1,62}")
_WORKFLOW_REF = re.compile(
    r"mindclade/\.github/\.github/workflows/reusable-binauthz-sign\.yml@refs/tags/v[0-9]+\.[0-9]+\.[0-9]+"
)
_PREDICATE_TYPES = {
    "build-provenance": "provenance",
    "qualification": "qualification",
    "sbom": "sbom",
    "vulnerability-scan": "vulnerability-scan",
}
_REQUIRED_ARTIFACT_TYPES = set(_PREDICATE_TYPES.values()) | {"rollback"}


def _as_dict(value: object) -> dict[str, Any]:
    return cast("dict[str, Any]", value) if isinstance(value, dict) else {}


def _as_list(value: object) -> list[Any]:
    return cast("list[Any]", value) if isinstance(value, list) else []


def canonical_bytes(value: dict[str, Any]) -> bytes:
    return json.dumps(value, sort_keys=True, separators=(",", ":"), ensure_ascii=True).encode(
        "utf-8"
    )


def evidence_digest(value: dict[str, Any]) -> str:
    return "sha256:" + hashlib.sha256(canonical_bytes(value)).hexdigest()


def load_evidence(path: Path) -> dict[str, Any]:
    raw = path.read_bytes()
    if len(raw) > MAXIMUM_EVIDENCE_BYTES:
        raise ValueError(f"release evidence exceeds {MAXIMUM_EVIDENCE_BYTES} bytes")
    value = json.loads(raw)
    if not isinstance(value, dict):
        raise ValueError("release evidence must be one JSON object")
    return value


def _timestamp(value: object, label: str, errors: list[str]) -> dt.datetime | None:
    try:
        parsed = dt.datetime.fromisoformat(str(value).replace("Z", "+00:00"))
    except ValueError:
        errors.append(f"{label} must be an ISO-8601 timestamp")
        return None
    if parsed.tzinfo is None or parsed.utcoffset() is None:
        errors.append(f"{label} must include a timezone")
        return None
    return parsed


def validate_evidence(value: dict[str, Any], schema: dict[str, Any]) -> tuple[str, ...]:
    errors = [f"{failure.path}: {failure.message}" for failure in validate(value, schema)]
    subject = _as_dict(value.get("subject"))
    subject_digest = str(subject.get("digest", ""))
    images = _as_dict(value.get("images"))
    image_names = list(images)
    if all(isinstance(item, str) for item in image_names) and image_names != sorted(image_names):
        errors.append("images must be sorted by name")
    image_references = list(images.values())
    if all(isinstance(item, str) for item in image_references) and len(
        set(image_references)
    ) != len(image_references):
        errors.append("images must not repeat an immutable reference")

    artifacts = _as_list(value.get("artifacts"))
    names: list[str] = []
    by_name: dict[str, dict[str, Any]] = {}
    by_type: dict[str, list[str]] = {}
    for artifact in artifacts:
        if not isinstance(artifact, dict):
            continue
        name = str(artifact.get("name", ""))
        artifact_type = str(artifact.get("type", ""))
        names.append(name)
        if name in by_name:
            errors.append(f"duplicate artifact name {name!r}")
        by_name[name] = artifact
        by_type.setdefault(artifact_type, []).append(name)
    if names != sorted(names):
        errors.append("artifacts must be sorted by name")
    for artifact_type in sorted(_REQUIRED_ARTIFACT_TYPES):
        if len(by_type.get(artifact_type, [])) != 1:
            errors.append(f"artifacts must contain exactly one {artifact_type!r}")

    evidence = _as_dict(value.get("evidence"))
    graph = _as_list(evidence.get("graph"))
    predicates: set[str] = set()
    for edge in graph:
        if not isinstance(edge, dict):
            continue
        predicate = str(edge.get("predicate_type", ""))
        if predicate in predicates:
            errors.append(f"duplicate evidence predicate {predicate!r}")
        predicates.add(predicate)
        if edge.get("subject_digest") != subject_digest:
            errors.append(f"evidence predicate {predicate!r} does not bind the release subject")
        artifact = by_name.get(str(edge.get("artifact", "")))
        expected = _PREDICATE_TYPES.get(predicate)
        if artifact is None or artifact.get("type") != expected:
            errors.append(f"evidence predicate {predicate!r} references the wrong artifact")
        if predicate != "vulnerability-scan" and edge.get("result") != "pass":
            errors.append(f"evidence predicate {predicate!r} must pass")
    if predicates != set(_PREDICATE_TYPES):
        errors.append("evidence graph must cover each required predicate exactly once")

    vulnerability = _as_dict(value.get("vulnerability"))
    counts = _as_dict(vulnerability.get("finding_counts"))
    result = vulnerability.get("result")
    exception = vulnerability.get("exception")
    approved_at: dt.datetime | None = None
    expires_at: dt.datetime | None = None
    if result == "pass":
        if counts.get("critical") != 0 or counts.get("high") != 0 or counts.get("unknown") != 0:
            errors.append(
                "passing vulnerability evidence requires zero critical, high, and unknown findings"
            )
        if exception is not None:
            errors.append("passing vulnerability evidence may not include an exception")
    elif result == "approved-exception":
        if not isinstance(exception, dict):
            errors.append("approved vulnerability evidence requires an exception")
        else:
            if set(exception) != {
                "ticket",
                "approved_by",
                "approved_at",
                "expires_at",
                "justification",
            }:
                errors.append("vulnerability exception has unsupported or missing fields")
            if exception.get("approved_by") != "@mindclade/security":
                errors.append("vulnerability exception requires @mindclade/security approval")
            approved_at = _timestamp(
                exception.get("approved_at"), "vulnerability approved_at", errors
            )
            expires_at = _timestamp(exception.get("expires_at"), "vulnerability expires_at", errors)
            if (
                approved_at
                and expires_at
                and (expires_at <= approved_at or expires_at - approved_at > dt.timedelta(days=90))
            ):
                errors.append("vulnerability exception must expire within 90 days")
            for field in ("ticket", "justification"):
                if not isinstance(exception.get(field), str) or not exception[field].strip():
                    errors.append(f"vulnerability exception {field} is required")
        blocking_counts = [counts.get(name) for name in ("critical", "high", "unknown")]
        if not any(
            isinstance(item, int) and not isinstance(item, bool) and item > 0
            for item in blocking_counts
        ):
            errors.append("vulnerability exception requires at least one blocking finding")

    vulnerability_edges = [
        edge
        for edge in graph
        if isinstance(edge, dict) and edge.get("predicate_type") == "vulnerability-scan"
    ]
    if len(vulnerability_edges) == 1:
        expected_graph_result = "pass" if result == "pass" else "approved"
        if vulnerability_edges[0].get("result") != expected_graph_result:
            errors.append("vulnerability graph result must match the scan decision")

    qualification_epoch = _timestamp(
        evidence.get("qualification_epoch"), "qualification_epoch", errors
    )
    scanned_at = _timestamp(vulnerability.get("scanned_at"), "vulnerability scanned_at", errors)
    created_at = _timestamp(value.get("created_at"), "created_at", errors)
    if qualification_epoch and created_at and qualification_epoch > created_at:
        errors.append("qualification_epoch may not be later than created_at")
    if scanned_at and created_at and scanned_at > created_at:
        errors.append("vulnerability scanned_at may not be later than created_at")
    if scanned_at and approved_at and scanned_at > approved_at:
        errors.append("vulnerability approval may not predate the scan")
    if approved_at and created_at and approved_at > created_at:
        errors.append("vulnerability approval may not be later than created_at")
    if expires_at and created_at and expires_at <= created_at:
        errors.append("vulnerability exception must be active when evidence is created")

    attestations = _as_dict(value.get("attestations"))
    build = _as_dict(attestations.get("build"))
    qualification = _as_dict(attestations.get("qualification"))
    if (build.get("project"), build.get("attestor")) == (
        qualification.get("project"),
        qualification.get("attestor"),
    ):
        errors.append("build and qualification attestor roots must be distinct")

    migration = _as_dict(value.get("migration"))
    migration_artifact = by_name.get(str(migration.get("artifact")))
    if migration.get("required") is True:
        if migration_artifact is None or migration_artifact.get("type") != "migration":
            errors.append("required migration must reference one migration artifact")
    elif migration.get("artifact") is not None:
        errors.append("migration artifact must be null when migration is not required")
    rollback = _as_dict(value.get("rollback"))
    rollback_artifact = by_name.get(str(rollback.get("artifact")))
    if rollback_artifact is None or rollback_artifact.get("type") != "rollback":
        errors.append("rollback must reference one rollback artifact")
    if rollback.get("strategy") == "previous-release":
        if not rollback.get("previous_release_id") or not _DIGEST.fullmatch(
            str(rollback.get("previous_subject_digest", ""))
        ):
            errors.append("previous-release rollback requires exact prior lineage")
    elif rollback.get("strategy") == "bootstrap" and (
        rollback.get("previous_release_id") is not None
        or rollback.get("previous_subject_digest") is not None
    ):
        errors.append("bootstrap rollback may not claim prior lineage")
    return tuple(sorted(set(errors)))


def to_gitops_record(
    value: dict[str, Any],
    *,
    deployment_project: str,
    deployment_attestor: str,
    signer_workflow_ref: str,
    schema: dict[str, Any] | None = None,
) -> dict[str, Any]:
    """Create consumer-shaped data only after producer evidence is valid."""

    schema = schema or load_json(
        Path(__file__).resolve().parent / "schemas/release-evidence.schema.json"
    )
    errors = validate_evidence(value, schema)
    if errors:
        raise ValueError("invalid producer evidence: " + "; ".join(errors))
    if not _PROJECT.fullmatch(deployment_project) or not _ATTESTOR.fullmatch(deployment_attestor):
        raise ValueError("deployment attestor project and name must be bounded identifiers")
    if not isinstance(signer_workflow_ref, str) or not _WORKFLOW_REF.fullmatch(signer_workflow_ref):
        raise ValueError("signer workflow must be an immutable governed release")
    roots = {
        (entry["project"], entry["attestor"])
        for entry in value["attestations"].values()
        if isinstance(entry, dict)
    }
    if (deployment_project, deployment_attestor) in roots:
        raise ValueError("deployment attestor root must be distinct from producer attestors")
    record = copy.deepcopy(value)
    record["contract_version"] = "4.0.0"
    del record["schema_version"]
    record["attestations"]["deployment"] = {
        "project": deployment_project,
        "attestor": deployment_attestor,
        "signer_workflow_ref": signer_workflow_ref,
    }
    return record


def validate_file(path: Path, schema_path: Path) -> tuple[dict[str, Any], tuple[str, ...]]:
    value = load_evidence(path)
    schema = load_json(schema_path)
    return value, validate_evidence(value, schema)
