# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

from __future__ import annotations

import copy
import datetime as dt
import json
import shutil
from pathlib import Path

import pytest

from services.mlflow import validate_dependency_gate as gate

ROOT = Path(__file__).resolve().parents[2]


def scanner_report(*, vulnerable: bool = True) -> dict:
    finding = {
        "aliases": ["GHSA-g6cj-pr64-35w5", "CVE-2026-69247"],
        "description": "not trusted as policy input",
        "fix_versions": ["50.0.0"],
        "id": "PYSEC-2026-3552",
    }
    return {
        "dependencies": [
            {
                "name": "cryptography",
                "version": "49.0.0",
                "vulns": [finding] if vulnerable else [],
            }
        ],
        "fixes": [],
    }


def write_report(tmp_path: Path, payload: dict) -> Path:
    path = tmp_path / "report.json"
    path.write_text(json.dumps(payload), encoding="utf-8")
    return path


def boundary_root(tmp_path: Path) -> Path:
    for relative in (
        "ci/release/targets.yaml",
        "infra/kubernetes/platform/mlflow/PRODUCTION_READINESS.md",
        "infra/kubernetes/platform/mlflow/chart/values.yaml",
        "services/mlflow/runtime.lock.yaml",
    ):
        destination = tmp_path / relative
        destination.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(ROOT / relative, destination)
    return tmp_path


def test_repository_gate_is_fail_closed() -> None:
    assert gate.validate(ROOT, ROOT / gate.GATE, today=dt.date(2026, 8, 23)) == (
        "blocked and publication-ineligible"
    )


def test_exact_scanner_finding_is_observed_but_not_accepted(tmp_path: Path) -> None:
    report = write_report(tmp_path, scanner_report())
    assert (
        gate.validate(
            ROOT,
            ROOT / gate.GATE,
            report_path=report,
            scanner_exit_code=1,
            today=dt.date(2026, 8, 23),
        )
        == "blocked and publication-ineligible"
    )
    with pytest.raises(gate.GateError, match="blocked until"):
        gate.validate(
            ROOT,
            ROOT / gate.GATE,
            report_path=report,
            scanner_exit_code=1,
            require_clean=True,
            today=dt.date(2026, 8, 23),
        )


def test_new_or_missing_finding_fails_closed(tmp_path: Path) -> None:
    unexpected = scanner_report()
    unexpected["dependencies"][0]["vulns"][0]["id"] = "PYSEC-2099-1"
    with pytest.raises(gate.GateError, match="differ"):
        gate.validate(
            ROOT,
            ROOT / gate.GATE,
            report_path=write_report(tmp_path, unexpected),
            scanner_exit_code=1,
            today=dt.date(2026, 8, 23),
        )
    with pytest.raises(gate.GateError, match="differ"):
        gate.validate(
            ROOT,
            ROOT / gate.GATE,
            report_path=write_report(tmp_path, scanner_report(vulnerable=False)),
            scanner_exit_code=0,
            today=dt.date(2026, 8, 23),
        )


def test_scanner_failure_and_exit_report_mismatch_fail_closed(tmp_path: Path) -> None:
    report = write_report(tmp_path, scanner_report())
    with pytest.raises(gate.GateError, match="unsupported exit code"):
        gate.validate(
            ROOT,
            ROOT / gate.GATE,
            report_path=report,
            scanner_exit_code=2,
            today=dt.date(2026, 8, 23),
        )
    with pytest.raises(gate.GateError, match="disagrees"):
        gate.validate(
            ROOT,
            ROOT / gate.GATE,
            report_path=report,
            scanner_exit_code=0,
            today=dt.date(2026, 8, 23),
        )


def test_expired_or_approved_blocker_fails_closed(tmp_path: Path) -> None:
    payload = gate.load_json(ROOT / gate.GATE)
    with pytest.raises(gate.GateError, match="expired"):
        gate.validate(ROOT, ROOT / gate.GATE, today=dt.date(2026, 9, 7))

    approved = copy.deepcopy(payload)
    approved["approvedException"] = True
    approved_path = tmp_path / "approved.json"
    approved_path.write_text(json.dumps(approved), encoding="utf-8")
    with pytest.raises(gate.GateError, match="must not claim"):
        gate.validate(ROOT, approved_path, today=dt.date(2026, 8, 23))


def test_release_catalog_or_chart_activation_regression_fails_closed(tmp_path: Path) -> None:
    policy = gate.load_json(ROOT / gate.GATE)
    root = boundary_root(tmp_path)
    catalog = root / "ci/release/targets.yaml"
    catalog.write_text(catalog.read_text(encoding="utf-8") + "\n  mlflow:\n", encoding="utf-8")
    with pytest.raises(gate.GateError, match="closed release catalog"):
        gate.require_source_boundary(root, policy)

    root = boundary_root(tmp_path / "chart")
    values = root / "infra/kubernetes/platform/mlflow/chart/values.yaml"
    values.write_text(
        values.read_text(encoding="utf-8").replace("  enabled: false", "  enabled: true", 1),
        encoding="utf-8",
    )
    with pytest.raises(gate.GateError, match="activate MLflow"):
        gate.require_source_boundary(root, policy)
