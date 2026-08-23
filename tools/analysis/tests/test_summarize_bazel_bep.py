# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

from __future__ import annotations

import gzip
import importlib.util
import json
import sys
from pathlib import Path

import pytest

ROOT = Path(__file__).resolve().parents[3]


def load(name: str, path: Path):
    spec = importlib.util.spec_from_file_location(name, path)
    if spec is None or spec.loader is None:
        raise RuntimeError(f"unable to load {path}")
    module = importlib.util.module_from_spec(spec)
    sys.modules[name] = module
    spec.loader.exec_module(module)
    return module


summarizer = load("summarize_bazel_bep", ROOT / "tools/analysis/summarize_bazel_bep.py")


def fixture() -> list[dict[str, object]]:
    return [
        {"id": {"started": {}}, "started": {"command": "test"}},
        {
            "id": {"testSummary": {"label": "//pkg:one"}},
            "testSummary": {
                "overallStatus": "PASSED",
                "attemptCount": 1,
                "totalRunDurationMillis": 125,
            },
        },
        {
            "id": {"testSummary": {"label": "//pkg:two"}},
            "testSummary": {
                "overallStatus": "FLAKY",
                "attemptCount": 2,
                "totalRunDurationMillis": 250,
            },
        },
        {
            "id": {"buildMetrics": {}},
            "buildMetrics": {
                "actionSummary": {
                    "actionsCreated": 17,
                    "actionsExecuted": 11,
                    "actionCacheStatistics": {"hits": 5, "misses": 6},
                    "runnerCount": [
                        {"name": "linux-sandbox", "count": 9},
                        {"name": "internal", "count": 2},
                    ],
                },
                "targetMetrics": {"targetsConfigured": 42},
                "packageMetrics": {"packagesLoaded": 13},
                "timingMetrics": {
                    "wallTimeInMs": 1000,
                    "cpuTimeInMs": 700,
                    "analysisPhaseTimeInMs": 300,
                    "executionPhaseTimeInMs": 600,
                    "criticalPathTimeInMs": 450,
                },
            },
        },
    ]


def write(path: Path) -> None:
    content = "".join(json.dumps(event) + "\n" for event in fixture())
    if path.suffix == ".gz":
        with gzip.open(path, "wt", encoding="utf-8") as handle:
            handle.write(content)
    else:
        path.write_text(content, encoding="utf-8")


@pytest.mark.parametrize("suffix", [".json", ".json.gz"])
def test_summarizes_plain_and_compressed_bep(tmp_path: Path, suffix: str) -> None:
    path = tmp_path / f"events{suffix}"
    write(path)
    summary = summarizer.summarize(path, "full test")
    assert summary["command"] == "test"
    assert summary["timing_ms"]["critical_path"] == 450
    assert summary["graph"] == {"packages_loaded": 13, "targets_configured": 42}
    assert summary["actions"]["cache_hits"] == 5
    assert summary["actions"]["runners"] == {"internal": 2, "linux-sandbox": 9}
    assert summary["tests"]["outcomes"] == {"FLAKY": 1, "PASSED": 1}
    assert summary["tests"]["attempts"] == 3
    assert summary["tests"]["non_passing_labels"] == {"FLAKY": ["//pkg:two"]}
    assert "Bazel full test performance" in summarizer.markdown(summary)


def test_missing_metrics_is_rejected(tmp_path: Path) -> None:
    path = tmp_path / "events.json"
    path.write_text('{"started":{"command":"build"}}\n', encoding="utf-8")
    with pytest.raises(summarizer.BepError, match="no buildMetrics"):
        summarizer.summarize(path, "analysis")
