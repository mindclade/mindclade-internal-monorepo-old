#!/usr/bin/env python3
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

"""Validate the MLflow dependency decision without accepting a vulnerability."""

from __future__ import annotations

import argparse
import datetime as dt
import hashlib
import json
import re
import sys
from pathlib import Path
from typing import Any

ROOT = Path(__file__).resolve().parents[2]
GATE = Path("services/mlflow/security-gate.json")
EXPECTED_GATE_FIELDS = {
    "approvedException",
    "artifact",
    "findings",
    "lock",
    "remediationDeadline",
    "reviewedAt",
    "schemaVersion",
    "status",
    "unblockRequires",
}
EXPECTED_FINDING_FIELDS = {
    "aliases",
    "fixVersions",
    "id",
    "package",
    "version",
}
EXPECTED_LOCK_FIELDS = {"path", "sha256"}
MAX_REMEDIATION_DAYS = 30


class GateError(ValueError):
    """The declared security decision or scanner result is unsafe."""


def unique_object(pairs: list[tuple[str, Any]]) -> dict[str, Any]:
    value: dict[str, Any] = {}
    for key, item in pairs:
        if key in value:
            raise GateError(f"JSON contains duplicate key: {key}")
        value[key] = item
    return value


def reject_constant(value: str) -> None:
    raise GateError(f"JSON contains invalid constant: {value}")


def load_json(path: Path) -> dict[str, Any]:
    try:
        payload = json.loads(
            path.read_text(encoding="utf-8"),
            object_pairs_hook=unique_object,
            parse_constant=reject_constant,
        )
    except GateError:
        raise
    except (OSError, UnicodeError, json.JSONDecodeError) as error:
        raise GateError(f"cannot read valid JSON: {path}") from error
    if not isinstance(payload, dict):
        raise GateError(f"JSON root must be an object: {path}")
    return payload


def iso_date(value: object, label: str) -> dt.date:
    if not isinstance(value, str):
        raise GateError(f"{label} must be an ISO date")
    try:
        return dt.date.fromisoformat(value)
    except ValueError as error:
        raise GateError(f"{label} must be an ISO date") from error


def normalized_finding(
    value: object, label: str
) -> tuple[str, str, str, tuple[str, ...], tuple[str, ...]]:
    if not isinstance(value, dict) or set(value) != EXPECTED_FINDING_FIELDS:
        raise GateError(f"{label} has unsupported or missing fields")
    package = value.get("package")
    version = value.get("version")
    finding_id = value.get("id")
    aliases = value.get("aliases")
    fixes = value.get("fixVersions")
    if not all(isinstance(item, str) and item for item in (package, version, finding_id)):
        raise GateError(f"{label} identity is invalid")
    if (
        not isinstance(aliases, list)
        or not aliases
        or aliases != sorted(set(aliases))
        or not all(isinstance(item, str) and item for item in aliases)
    ):
        raise GateError(f"{label} aliases must be a sorted, unique, nonempty string list")
    if (
        not isinstance(fixes, list)
        or not fixes
        or fixes != sorted(set(fixes))
        or not all(isinstance(item, str) and item for item in fixes)
    ):
        raise GateError(f"{label} fixes must be a sorted, unique, nonempty string list")
    return package, version, finding_id, tuple(aliases), tuple(fixes)


def scanner_findings(
    report: dict[str, Any],
) -> tuple[tuple[str, str, str, tuple[str, ...], tuple[str, ...]], ...]:
    if set(report) != {"dependencies", "fixes"}:
        raise GateError("pip-audit report has unsupported or missing fields")
    dependencies = report.get("dependencies")
    if not isinstance(dependencies, list) or not dependencies:
        raise GateError("pip-audit report contains no dependency inventory")
    findings: list[tuple[str, str, str, tuple[str, ...], tuple[str, ...]]] = []
    for index, dependency in enumerate(dependencies):
        if not isinstance(dependency, dict):
            raise GateError(f"pip-audit dependency {index} is invalid")
        package = dependency.get("name")
        version = dependency.get("version")
        vulnerabilities = dependency.get("vulns")
        if (
            not isinstance(package, str)
            or not package
            or not isinstance(version, str)
            or not version
        ):
            raise GateError(f"pip-audit dependency {index} has no exact identity")
        if not isinstance(vulnerabilities, list):
            raise GateError(f"pip-audit dependency {package} has no vulnerability list")
        for vulnerability in vulnerabilities:
            if not isinstance(vulnerability, dict):
                raise GateError(f"pip-audit finding for {package} is invalid")
            finding_id = vulnerability.get("id")
            aliases = vulnerability.get("aliases")
            fixes = vulnerability.get("fix_versions")
            if not isinstance(finding_id, str) or not finding_id:
                raise GateError(f"pip-audit finding for {package} has no ID")
            if not isinstance(aliases, list) or not all(
                isinstance(item, str) and item for item in aliases
            ):
                raise GateError(f"pip-audit aliases for {package} are invalid")
            if not isinstance(fixes, list) or not all(
                isinstance(item, str) and item for item in fixes
            ):
                raise GateError(f"pip-audit fixes for {package} are invalid")
            findings.append(
                (
                    package,
                    version,
                    finding_id,
                    tuple(sorted(set(aliases))),
                    tuple(sorted(set(fixes))),
                )
            )
    if len(findings) != len(set(findings)):
        raise GateError("pip-audit report contains duplicate findings")
    return tuple(sorted(findings))


def top_level_block(contents: str, section: str) -> str:
    headings = list(re.finditer(rf"(?m)^{re.escape(section)}:\s*$", contents))
    if len(headings) != 1:
        raise GateError(f"chart {section} boundary must occur exactly once")
    tail = contents[headings[0].end() :]
    next_heading = re.search(r"(?m)^[A-Za-z][A-Za-z0-9_-]*:\s*$", tail)
    return tail[: next_heading.start()] if next_heading else tail


def require_source_boundary(root: Path, gate: dict[str, Any]) -> None:
    runtime_lock = (root / "services/mlflow/runtime.lock.yaml").read_text(encoding="utf-8")
    expected_state = (
        "blocked-security-findings" if gate["status"] == "blocked" else "release-candidate"
    )
    for line in (
        f"  publicationState: {expected_state}",
        "  securityGate: services/mlflow/security-gate.json",
    ):
        if runtime_lock.splitlines().count(line) != 1:
            raise GateError(f"runtime lock omits fail-closed security state: {line.strip()}")

    values = (root / "infra/kubernetes/platform/mlflow/chart/values.yaml").read_text(
        encoding="utf-8"
    )
    activation = top_level_block(values, "activation")
    image = top_level_block(values, "image")
    for contents, pattern, message in (
        (
            activation,
            r"(?m)^  enabled: false$",
            "chart defaults activate MLflow",
        ),
        (
            activation,
            r"(?m)^  releaseEvidenceDigest: blocked$",
            "chart defaults carry releasable evidence",
        ),
        (image, r"(?m)^  digest: sha256:0{64}$", "chart defaults select a releasable image"),
    ):
        if len(re.findall(pattern, contents)) != 1:
            raise GateError(message)

    release_catalog = (root / "ci/release/targets.yaml").read_text(encoding="utf-8")
    release_reference = re.search(
        r"(?m)^\s+(?:buildTarget|pushTarget):\s*//services/mlflow:[^\s#]+\s*$",
        release_catalog,
    )
    release_build = re.search(
        r"(?m)^\s+buildTarget:\s*//services/mlflow:image\s*$", release_catalog
    )
    release_push = re.search(r"(?m)^\s+pushTarget:\s*//services/mlflow:push\s*$", release_catalog)
    if gate["status"] == "blocked" and release_reference:
        raise GateError("blocked MLflow image is present in the closed release catalog")
    if gate["status"] == "clean" and not (release_build and release_push):
        raise GateError("clean MLflow image targets are absent from the closed release catalog")

    readiness = (root / "infra/kubernetes/platform/mlflow/PRODUCTION_READINESS.md").read_text(
        encoding="utf-8"
    )
    required_readiness = (
        (
            "| Observed | Cryptography advisory remediation | BLOCKED |",
            "no override or exception is approved",
            "image promotion remains blocked",
        )
        if gate["status"] == "blocked"
        else ("| Observed | Cryptography advisory remediation | PASS |",)
    )
    for statement in required_readiness:
        if statement.casefold() not in readiness.casefold():
            raise GateError(f"MLflow readiness documentation omits: {statement}")

    if gate.get("approvedException") is not False:
        raise GateError("MLflow security gate must not claim an approved exception")


def validate(
    root: Path,
    gate_path: Path,
    *,
    report_path: Path | None = None,
    scanner_exit_code: int | None = None,
    require_clean: bool = False,
    today: dt.date | None = None,
) -> str:
    gate = load_json(gate_path)
    if set(gate) != EXPECTED_GATE_FIELDS or gate.get("schemaVersion") != 1:
        raise GateError("MLflow security gate has unsupported or missing fields")
    if gate.get("artifact") != "//services/mlflow:image":
        raise GateError("MLflow security gate names the wrong artifact")
    if gate.get("status") not in {"blocked", "clean"}:
        raise GateError("MLflow security gate status must be blocked or clean")
    unblock = gate.get("unblockRequires")
    if (
        not isinstance(unblock, list)
        or len(unblock) < 3
        or not all(isinstance(item, str) and item.strip() for item in unblock)
    ):
        raise GateError("MLflow security gate has no complete unblock procedure")

    lock = gate.get("lock")
    if not isinstance(lock, dict) or set(lock) != EXPECTED_LOCK_FIELDS:
        raise GateError("MLflow security gate lock identity is invalid")
    if lock.get("path") != "services/mlflow/requirements.lock.txt":
        raise GateError("MLflow security gate names the wrong lock")
    digest = hashlib.sha256((root / lock["path"]).read_bytes()).hexdigest()
    if lock.get("sha256") != digest:
        raise GateError("MLflow security gate lock digest is stale")

    reviewed = iso_date(gate.get("reviewedAt"), "reviewedAt")
    deadline = iso_date(gate.get("remediationDeadline"), "remediationDeadline")
    if deadline < reviewed or (deadline - reviewed).days > MAX_REMEDIATION_DAYS:
        raise GateError("MLflow remediation deadline is invalid or exceeds 30 days")
    effective_today = today or dt.datetime.now(dt.UTC).date()
    if reviewed > effective_today:
        raise GateError("MLflow security review date is in the future")
    if gate["status"] == "blocked" and effective_today > deadline:
        raise GateError(f"MLflow remediation deadline expired on {deadline.isoformat()}")

    raw_findings = gate.get("findings")
    if not isinstance(raw_findings, list):
        raise GateError("MLflow security gate findings must be a list")
    declared = tuple(
        sorted(
            normalized_finding(value, f"finding {index}")
            for index, value in enumerate(raw_findings)
        )
    )
    if len(declared) != len(set(declared)):
        raise GateError("MLflow security gate findings are duplicated")
    if gate["status"] == "blocked" and not declared:
        raise GateError("blocked MLflow security gate declares no finding")
    if gate["status"] == "clean" and declared:
        raise GateError("clean MLflow security gate retains findings")

    require_source_boundary(root, gate)

    if (report_path is None) != (scanner_exit_code is None):
        raise GateError("scanner report and exit code must be supplied together")
    observed: tuple[tuple[str, str, str, tuple[str, ...], tuple[str, ...]], ...] | None = None
    if report_path is not None:
        if scanner_exit_code not in {0, 1}:
            raise GateError(f"pip-audit failed with unsupported exit code {scanner_exit_code}")
        observed = scanner_findings(load_json(report_path))
        expected_exit = 1 if observed else 0
        if scanner_exit_code != expected_exit:
            raise GateError("pip-audit exit code disagrees with its report")
        if observed != declared:
            raise GateError("pip-audit findings differ from the declared MLflow security gate")

    if require_clean:
        if report_path is None:
            raise GateError("release validation requires a current pip-audit report")
        if gate["status"] != "clean" or declared or observed:
            raise GateError("MLflow release is blocked until the dependency scan is clean")
        return "clean and release-eligible"
    return "blocked and publication-ineligible" if gate["status"] == "blocked" else "clean"


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--root", type=Path, default=ROOT)
    parser.add_argument("--gate", type=Path)
    parser.add_argument("--report", type=Path)
    parser.add_argument("--scanner-exit-code", type=int)
    parser.add_argument("--require-clean", action="store_true")
    args = parser.parse_args()
    root = args.root.resolve()
    gate = args.gate.resolve() if args.gate else root / GATE
    try:
        state = validate(
            root,
            gate,
            report_path=args.report,
            scanner_exit_code=args.scanner_exit_code,
            require_clean=args.require_clean,
        )
    except (GateError, OSError, UnicodeError) as error:
        print(f"MLflow dependency gate failed: {error}", file=sys.stderr)
        return 1
    print(f"MLflow dependency gate passed: {state}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
