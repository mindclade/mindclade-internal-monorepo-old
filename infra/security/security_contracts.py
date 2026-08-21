# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Validate source-owned infrastructure security contracts and their enforcement links."""

from __future__ import annotations

import json
from pathlib import Path, PurePosixPath
from typing import Any, cast

from configs.contract_validation import load_json, validate

_EXPECTED_FAMILIES = {
    "audit-retention.yaml": "audit-retention",
    "break-glass.yaml": "break-glass",
    "image-policy.yaml": "image-policy",
    "model-weight-access.yaml": "model-weight-access",
    "network-policies.yaml": "network-policy",
    "node-attestation.yaml": "node-attestation",
    "pod-security.yaml": "pod-security",
    "secrets-rotation.yaml": "secrets-rotation",
    "supply-chain-policy.yaml": "supply-chain",
}
_REQUIRED_PLANES = {"ci", "gcp", "kubernetes", "terraform"}


def load_json_yaml(path: Path) -> dict[str, Any]:
    """Load the repository's JSON-compatible YAML subset without a YAML dependency."""

    payload = "\n".join(
        line
        for line in path.read_text(encoding="utf-8").splitlines()
        if not line.startswith("#") and line.strip() != "---"
    ).strip()
    value = json.loads(payload)
    if not isinstance(value, dict):
        raise ValueError(f"{path}: expected one object")
    return cast("dict[str, Any]", value)


def _strings(value: object) -> list[str]:
    if not isinstance(value, list) or not all(isinstance(item, str) for item in value):
        return []
    return cast("list[str]", value)


def _safe_repository_path(root: Path, raw: str) -> Path | None:
    path = PurePosixPath(raw)
    if path.is_absolute() or ".." in path.parts or "." in path.parts:
        return None
    candidate = root.joinpath(*path.parts)
    try:
        candidate.resolve().relative_to(root.resolve())
    except (OSError, ValueError):
        return None
    return candidate


def validate_catalog(root: Path) -> list[str]:
    """Return deterministic errors for schema, lifecycle, and source-reference drift."""

    security_root = root / "infra/security"
    errors: list[str] = []
    try:
        schema = load_json(security_root / "control-contract.schema.json")
    except (OSError, ValueError, json.JSONDecodeError) as exc:
        return [str(exc)]

    paths = sorted(security_root.glob("*.yaml"))
    observed_names = {path.name for path in paths}
    expected_names = set(_EXPECTED_FAMILIES)
    if observed_names != expected_names:
        errors.append(
            "security control inventory drifted: "
            f"missing={sorted(expected_names - observed_names)}, "
            f"unexpected={sorted(observed_names - expected_names)}"
        )

    all_planes: set[str] = set()
    all_families: list[str] = []
    for path in paths:
        try:
            document = load_json_yaml(path)
        except (OSError, ValueError, json.JSONDecodeError) as exc:
            errors.append(f"{path.name}: {exc}")
            continue
        errors.extend(
            f"{path.name} {failure.path}: {failure.message}"
            for failure in validate(document, schema)
        )
        if document.get("name") != path.stem:
            errors.append(f"{path.name}: name must match the file stem")
        expected_family = _EXPECTED_FAMILIES.get(path.name)
        if expected_family is not None and document.get("controlFamily") != expected_family:
            errors.append(f"{path.name}: controlFamily must be {expected_family!r}")

        all_families.append(str(document.get("controlFamily", "")))
        planes = _strings(document.get("planes"))
        sources = _strings(document.get("enforcementSources"))
        tests = _strings(document.get("testSources"))
        evidence = _strings(document.get("requiredEvidence"))
        all_planes.update(planes)
        for label, values in (
            ("planes", planes),
            ("enforcementSources", sources),
            ("testSources", tests),
            ("requiredEvidence", evidence),
        ):
            if values != sorted(values):
                errors.append(f"{path.name}: {label} must be sorted")

        if not any(not source.startswith("infra/security/") for source in sources):
            errors.append(f"{path.name}: at least one enforcement source must be independently owned")
        for label, values in (("enforcementSources", sources), ("testSources", tests)):
            for raw in values:
                candidate = _safe_repository_path(root, raw)
                if candidate is None:
                    errors.append(f"{path.name}: unsafe {label} path {raw!r}")
                elif not candidate.exists():
                    errors.append(f"{path.name}: missing {label} path {raw!r}")

        failure_policy = document.get("failurePolicy", {})
        retry = failure_policy.get("retry", {}) if isinstance(failure_policy, dict) else {}
        strategy = retry.get("strategy") if isinstance(retry, dict) else None
        attempts = retry.get("maxAttempts") if isinstance(retry, dict) else None
        if strategy == "none" and attempts != 0:
            errors.append(f"{path.name}: retry strategy 'none' requires zero attempts")
        if strategy == "bounded" and (not isinstance(attempts, int) or attempts < 1):
            errors.append(f"{path.name}: bounded retry requires at least one attempt")
        if strategy == "manual" and attempts != 0:
            errors.append(f"{path.name}: manual retry requires zero automatic attempts")

    if len(all_families) != len(set(all_families)):
        errors.append("security control families must be unique")
    if all_planes != _REQUIRED_PLANES:
        errors.append(
            f"security catalog plane coverage drifted: expected {sorted(_REQUIRED_PLANES)}, "
            f"observed {sorted(all_planes)}"
        )
    return sorted(set(errors))


def main() -> int:
    root = Path(__file__).resolve().parents[2]
    errors = validate_catalog(root)
    if errors:
        for error in errors:
            print(error)
        return 1
    print(f"Validated {len(_EXPECTED_FAMILIES)} infrastructure security control contracts.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
