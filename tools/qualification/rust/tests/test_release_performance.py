# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

from __future__ import annotations

import pytest

from tools.qualification.rust.release_performance import aggregate_gke_evidence, parse_cancellation


def _profile(name: str, factor: float) -> dict[str, object]:
    return {
        "schema_version": 1,
        "hardware_profile": name,
        "gpu_name": f"NVIDIA {name.upper()}",
        "gpu_memory_bytes": 80_000_000_000 * factor,
        "gpu_matmul_p50_ms": 1.0 * factor,
        "gpu_matmul_p95_ms": 2.0 * factor,
        "gpu_matmul_p99_ms": 3.0 * factor,
        "gpu_peak_allocated_bytes": 1_000_000 * factor,
        "checkpoint_staging_mib_per_s": 2_000.0 * factor,
        "worker_startup_p95_ms": 20.0 * factor,
        "unix_ipc_mib_per_s": 1_000.0 * factor,
        "verified_range_4k_ops_per_s": 200.0 * factor,
        "local_store_contended_4k_ops_per_s": 300.0 * factor,
        "data_stream_copy_bytes_per_byte": 1.0 * factor,
        "parser_allocated_bytes_per_input_byte": 2.0 * factor,
    }


def test_aggregate_uses_conservative_hardware_values() -> None:
    evidence = {
        "schema_version": 1,
        "profiles": {"h100": _profile("h100", 1.0), "h200": _profile("h200", 1.5)},
    }
    result = aggregate_gke_evidence(evidence)
    assert result["checkpoint_staging_mib_per_s"] == 2_000.0
    assert result["data_stream_copy_bytes_per_byte"] == 1.5
    assert result["node_stage_start_ms"] == 30.0
    assert result["h100_gpu_matmul_p99_ms"] == 3.0
    assert result["h200_gpu_matmul_p99_ms"] == 4.5


def test_release_inputs_reject_partial_profiles_and_fabricated_cancellation() -> None:
    with pytest.raises(ValueError, match="H100 and H200"):
        aggregate_gke_evidence({"schema_version": 1, "profiles": {"h100": _profile("h100", 1)}})
    assert parse_cancellation('{"runtime_gateway_cancellation_ms": 12.5}') == {
        "runtime_gateway_cancellation_ms": 12.5
    }
    with pytest.raises(ValueError, match="one positive"):
        parse_cancellation('{"runtime_gateway_cancellation_ms": 0}')
