# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

from __future__ import annotations

from pathlib import Path

import pytest

from tools.qualification.rust import performance

FLOOR = {"minimum": 500, "load_sensitive": True}
CEILING = {"maximum": 100, "load_sensitive": True}
DETERMINISTIC = {"maximum": 4096, "load_sensitive": False}

IDLE = 0.3
LOADED = 3.4
POLICY = performance.MeasurementPolicy(samples=5, warmup_runs=1, max_host_load_per_cpu=1.0)


def _verdict(budgets, samples, *, enforce=False, load=IDLE, complete=False):
    results = {
        name: performance.Measurement(name=name, samples=tuple(values))
        for name, values in samples.items()
    }
    return performance.evaluate(
        budgets,
        results,
        POLICY,
        require_complete=complete,
        enforce_inconclusive=enforce,
        load_per_cpu=load,
    )


def test_a_real_regression_on_an_idle_host_still_fails() -> None:
    """The whole point. Stabilising the measurement must not blunt the gate."""
    verdict = _verdict({"ipc": FLOOR}, {"ipc": (410.0, 405.0, 398.0, 412.0, 401.0)})
    assert verdict.failures == ["ipc: 405.0 < 500"]
    assert verdict.advisories == []


def test_a_healthy_measurement_on_an_idle_host_passes() -> None:
    verdict = _verdict({"ipc": FLOOR}, {"ipc": (1835.5, 1883.1, 1870.8, 1611.5, 1822.7)})
    assert verdict.failures == []
    assert verdict.passed == 1


def test_a_contended_host_yields_inconclusive_not_a_regression() -> None:
    """THE REGRESSION GUARD.

    These are real numbers: the same binary that measures 1835 MiB/s on an idle host measured
    203 while fifteen agents competed for the cores. Reporting that as a budget breach is what
    trained people to ignore the gate.
    """
    verdict = _verdict({"ipc": FLOOR}, {"ipc": (224.5, 164.3, 157.6, 203.0, 274.6)}, load=LOADED)
    assert verdict.failures == []
    assert len(verdict.advisories) == 1
    assert "INCONCLUSIVE" in verdict.advisories[0]
    assert "not evidence of a regression" in verdict.advisories[0]


def test_a_contended_host_does_not_manufacture_a_pass_either() -> None:
    """Inconclusive cuts both ways: a compliant-looking median on a loaded host proves nothing."""
    verdict = _verdict(
        {"ipc": FLOOR}, {"ipc": (9000.0, 9100.0, 8900.0, 9050.0, 8950.0)}, load=LOADED
    )
    assert verdict.passed == 0
    assert "not evidence of compliance" in verdict.advisories[0]


def test_in_ci_an_unusable_measurement_is_a_failure() -> None:
    """A dedicated runner that is contended is an infrastructure defect worth seeing."""
    verdict = _verdict(
        {"ipc": FLOOR}, {"ipc": (224.5, 164.3, 157.6, 203.0, 274.6)}, load=LOADED, enforce=True
    )
    assert verdict.advisories == []
    assert len(verdict.failures) == 1
    assert "enforcement required" in verdict.failures[0]


def test_split_samples_are_inconclusive_even_on_an_idle_host() -> None:
    """Three samples above the floor and two below cannot say which side the code is on."""
    verdict = _verdict({"ipc": FLOOR}, {"ipc": (520.0, 480.0, 530.0, 470.0, 540.0)})
    assert verdict.failures == []
    assert "cannot say which side of 500" in verdict.advisories[0]


def test_wide_scatter_that_never_reaches_the_budget_is_not_condemned() -> None:
    """Why there is no dispersion threshold.

    `runtime_host_invocation_overhead_us` measures a sub-microsecond operation against a 750 us
    ceiling. Its samples vary by an order of magnitude and every one passes by three orders of
    magnitude. A percentage rule would call that unusable; it is the opposite of unusable.
    """
    budget = {"maximum": 750, "load_sensitive": True}
    verdict = _verdict({"overhead": budget}, {"overhead": (0.02, 0.2, 0.03, 0.5, 0.04)})
    assert verdict.failures == []
    assert verdict.advisories == []
    assert verdict.passed == 1


def test_a_deterministic_budget_gates_even_on_a_loaded_host() -> None:
    """File descriptor counts do not care who else is on the box, so they are never downgraded."""
    verdict = _verdict({"fds": DETERMINISTIC}, {"fds": (9000.0,) * 5}, load=LOADED)
    assert verdict.advisories == []
    assert verdict.failures == ["fds: 9000.0 > 4096"]


def test_a_single_sample_from_another_lane_is_evaluated_as_before() -> None:
    """GKE hardware evidence arrives as one number from a machine this process never saw."""
    verdict = _verdict({"stage": CEILING}, {"stage": (140.0,)}, load=LOADED)
    assert verdict.failures == ["stage: 140.0 > 100"]


def test_the_median_is_used_not_the_mean() -> None:
    """One interfered-with sample must not drag the verdict; a mean here would read 1216."""
    samples = (1500.0, 1520.0, 1510.0, 1490.0, 60.0)
    verdict = _verdict({"ipc": FLOOR}, {"ipc": samples})
    # Four of five samples clear the floor, so the verdict is a unanimous-enough pass on the
    # median rather than an inconclusive split.
    assert verdict.failures == []


def test_missing_measurements_are_reported_only_when_completeness_is_required() -> None:
    assert _verdict({"ipc": FLOOR}, {}).failures == []
    assert _verdict({"ipc": FLOOR}, {}, complete=True).failures == ["missing measurement: ipc"]


def test_a_budget_without_load_sensitive_is_a_policy_error(tmp_path: Path) -> None:
    """Fail closed. Inheriting the permissive default silently is how these gates decay."""
    policy = tmp_path / "policy.toml"
    policy.write_text(
        "schema_version = 1\n"
        "[measurement]\nsamples = 5\nwarmup_runs = 1\nmax_host_load_per_cpu = 1.0\n"
        "[budget.ipc]\nminimum = 500\n"
    )
    with pytest.raises(performance.PolicyError, match="load_sensitive"):
        performance.load_policy(policy)


def test_a_policy_without_a_measurement_block_is_rejected(tmp_path: Path) -> None:
    policy = tmp_path / "policy.toml"
    policy.write_text("schema_version = 1\n[budget.ipc]\nminimum = 500\nload_sensitive = true\n")
    with pytest.raises(performance.PolicyError, match="measurement"):
        performance.load_policy(policy)


def test_too_few_samples_is_rejected(tmp_path: Path) -> None:
    """A median of two is the mean of two and inherits every problem this exists to fix."""
    policy = tmp_path / "policy.toml"
    policy.write_text(
        "schema_version = 1\n"
        "[measurement]\nsamples = 2\nwarmup_runs = 1\nmax_host_load_per_cpu = 1.0\n"
        "[budget.ipc]\nminimum = 500\nload_sensitive = true\n"
    )
    with pytest.raises(performance.PolicyError, match="at least 3"):
        performance.load_policy(policy)


def test_the_shipped_policy_loads_and_declares_every_budget() -> None:
    budgets, measurement = performance.load_policy()
    assert budgets
    assert measurement.samples >= 3
    assert all("load_sensitive" in budget for budget in budgets.values())


def test_no_shipped_threshold_was_relaxed() -> None:
    """The budgets this change was tempted to move. Pinned so a future edit has to argue.

    Gap 2 was reported as `unix_ipc_mib_per_s` failing at 476.6 and 299.6 against 500. The
    supported fix is the measurement model in this module; lowering the floor would delete the
    gate, because a floor that survives a contended host cannot detect the regression it exists
    for.
    """
    budgets, _ = performance.load_policy()
    assert budgets["unix_ipc_mib_per_s"]["minimum"] == 500
    assert budgets["artifact_verify_mib_per_s"]["minimum"] == 500
    assert budgets["verified_range_4k_ops_per_s"]["minimum"] == 50
    assert budgets["local_store_contended_4k_ops_per_s"]["minimum"] == 100
    assert budgets["checkpoint_staging_mib_per_s"]["minimum"] == 1000
    assert budgets["node_stage_start_ms"]["maximum"] == 100
    assert budgets["runtime_host_invocation_overhead_us"]["maximum"] == 750
