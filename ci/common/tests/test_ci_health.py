# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

from __future__ import annotations

import json
from pathlib import Path

import pytest

from ci.common import ci_health

HEAD = "a" * 40
PREFIX = "bazel-performance-123-1-"


def _write_json(path: Path, payload: dict[str, object]) -> None:
    path.write_text(json.dumps(payload), encoding="utf-8")


def _summary(*, wall: int, critical: int, hits: int, misses: int, flaky: str | None = None):
    labels = {} if flaky is None else {"FLAKY": [flaky]}
    return {
        "schema": 2,
        "label": "Bazel test",
        "command": "test",
        "source": "test.bep.json",
        "timing_ms": {
            "wall": wall,
            "cpu": wall,
            "analysis": 1,
            "execution": wall - 1,
            "critical_path": critical,
        },
        "graph": {"packages_loaded": 1, "targets_configured": 2},
        "actions": {
            "created": hits + misses,
            "executed": misses,
            "cache_hits": hits,
            "cache_misses": misses,
            "runners": {},
        },
        "tests": {
            "outcomes": {"PASSED": 1},
            "attempts": 1,
            "total_run_duration_ms": 10,
            "non_passing_labels": labels,
        },
    }


def _worker(root: Path, index: int, *, elapsed: float, flaky: str | None = None) -> None:
    path = root / f"{PREFIX}{index}"
    path.mkdir()
    _write_json(
        path / "run-metrics.json",
        {
            "schema_version": 1,
            "event": "merge_group",
            "mode": "full",
            "reason": "protected_full_graph",
            "head_sha": HEAD,
            "completed_at": "2026-08-23T12:00:00Z",
            "job_elapsed_seconds": elapsed,
            "latency_slo_seconds": 1800,
            "latency_slo_met": None,
            "analysis_target_count": 2,
            "test_target_count": 1,
            "shard_index": index,
            "shard_count": 2,
            "analysis_graph_sha256": "b" * 64,
            "test_graph_sha256": "c" * 64,
        },
    )
    _write_json(path / "analysis.summary.json", _summary(wall=10, critical=2, hits=2, misses=1))
    _write_json(
        path / "test.summary.json",
        _summary(wall=100 + index * 100, critical=50 + index * 50, hits=8, misses=2, flaky=flaky),
    )


def test_aggregates_bep_cache_critical_path_and_shard_health(tmp_path: Path) -> None:
    _worker(tmp_path, 0, elapsed=10.0, flaky="//pkg:flaky")
    _worker(tmp_path, 1, elapsed=20.0)
    payload = ci_health.build_dashboard(
        tmp_path,
        artifact_prefix=PREFIX,
        expected_workers=(0, 1),
        lane="presubmit",
        event="merge_group",
        head_sha=HEAD,
        topology_mode="full-sharded",
        shard_count=2,
    )
    assert payload["aggregate"]["action_cache"] == {
        "hits": 20,
        "misses": 6,
        "requests": 26,
        "hit_percent": 76.92,
    }
    assert payload["aggregate"]["job_elapsed_seconds"]["p95"] == 20.0
    assert payload["aggregate"]["test_critical_path_ms"]["maximum"] == 100
    assert payload["aggregate"]["shard_balance"]["test_wall_max_to_min_ratio"] == 2.0
    assert payload["aggregate"]["flaky_targets"] == ["//pkg:flaky"]
    assert payload["measurement_boundaries"]["queue_seconds"]["value"] is None
    rendered = ci_health.render_html(payload)
    assert "Content-Security-Policy" in rendered
    assert "//pkg:flaky" in rendered


def test_rejects_missing_worker_and_non_full_merge_group(tmp_path: Path) -> None:
    _worker(tmp_path, 0, elapsed=10.0)
    with pytest.raises(ci_health.HealthContractError, match="incomplete"):
        ci_health.build_dashboard(
            tmp_path,
            artifact_prefix=PREFIX,
            expected_workers=(0, 1),
            lane="presubmit",
            event="merge_group",
            head_sha=HEAD,
            topology_mode="full-sharded",
            shard_count=2,
        )
    metrics = json.loads((tmp_path / f"{PREFIX}0/run-metrics.json").read_text())
    metrics["mode"] = "affected"
    _write_json(tmp_path / f"{PREFIX}0/run-metrics.json", metrics)
    _worker(tmp_path, 1, elapsed=11.0)
    with pytest.raises(ci_health.HealthContractError, match="did not run full mode"):
        ci_health.build_dashboard(
            tmp_path,
            artifact_prefix=PREFIX,
            expected_workers=(0, 1),
            lane="presubmit",
            event="merge_group",
            head_sha=HEAD,
            topology_mode="full-sharded",
            shard_count=2,
        )


def test_escapes_untrusted_test_labels_in_html(tmp_path: Path) -> None:
    _worker(tmp_path, 0, elapsed=10.0, flaky="//pkg:<script>")
    _worker(tmp_path, 1, elapsed=11.0)
    payload = ci_health.build_dashboard(
        tmp_path,
        artifact_prefix=PREFIX,
        expected_workers=(0, 1),
        lane="presubmit",
        event="merge_group",
        head_sha=HEAD,
        topology_mode="full-sharded",
        shard_count=2,
    )
    rendered = ci_health.render_html(payload)
    assert "<script>" not in rendered
    assert "&lt;script&gt;" in rendered


def test_rejects_symlinked_worker_evidence(tmp_path: Path) -> None:
    evidence = tmp_path / "evidence"
    evidence.mkdir()
    real = tmp_path / "real"
    real.mkdir()
    (evidence / f"{PREFIX}0").symlink_to(real, target_is_directory=True)
    (evidence / f"{PREFIX}1").mkdir()
    with pytest.raises(ci_health.HealthContractError, match="artifact is invalid"):
        ci_health.build_dashboard(
            evidence,
            artifact_prefix=PREFIX,
            expected_workers=(0, 1),
            lane="presubmit",
            event="merge_group",
            head_sha=HEAD,
            topology_mode="full-sharded",
            shard_count=2,
        )


def test_rejects_symlinked_phase_summary(tmp_path: Path) -> None:
    _worker(tmp_path, 0, elapsed=10.0)
    _worker(tmp_path, 1, elapsed=11.0)
    summary = tmp_path / f"{PREFIX}0/analysis.summary.json"
    outside = tmp_path.parent / f"{tmp_path.name}-outside.json"
    summary.replace(outside)
    summary.symlink_to(outside)
    with pytest.raises(ci_health.HealthContractError, match="summary is unsafe"):
        ci_health.build_dashboard(
            tmp_path,
            artifact_prefix=PREFIX,
            expected_workers=(0, 1),
            lane="presubmit",
            event="merge_group",
            head_sha=HEAD,
            topology_mode="full-sharded",
            shard_count=2,
        )
