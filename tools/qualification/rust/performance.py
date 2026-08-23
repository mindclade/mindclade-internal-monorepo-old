#!/usr/bin/env python3
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Evaluate Rust performance budgets and optionally produce portable probes.

WHY THIS FILE GREW A MEASUREMENT MODEL
======================================
`unix_ipc_mib_per_s` failed three separate agents with 476.6, then 299.6, against a floor of
500. Each of them A/B'd the failure against a pristine tree on the same machine, and the base
failed identically -- a ~60% swing between consecutive runs of a byte-identical tree. That is
not a regression signal. That is a measurement taken while fifteen other processes were
competing for the same cores.

A gate that fails randomly trains people to ignore gates. It is also how a budget gets quietly
lowered: the fastest way to make red go green is to move the number, and then the gate no longer
measures anything. So the fix here is to the *measurement*, never to the budget. Not one
threshold in `configs/qualification/rust_performance.toml` moved.

THREE MECHANISMS, IN ORDER OF HOW MUCH THEY BUY
===============================================
1. **Median of N whole-process samples, after a discarded warmup.** A single sample is a
   lottery ticket on scheduler luck. The probe already does this internally for
   `artifact_verify_mib_per_s` -- and says why in its own comment -- so this extends an
   existing decision to every metric rather than inventing one.

2. **A split decision is not a decision.** If some samples meet the budget and others do not,
   the run cannot say which side of the line the code is on. Reporting the median as PASS is
   then exactly as wrong as reporting it as FAIL. It is INCONCLUSIVE.

   This is deliberately not a raw dispersion threshold. `runtime_host_invocation_overhead_us`
   measures a sub-microsecond operation against a 750 us budget: its samples routinely vary by
   a factor of ten and every one of them passes by three orders of magnitude, so the scatter
   there is irrelevant and a percentage rule would condemn a perfectly good measurement. What
   matters is never the scatter itself -- it is whether the scatter reaches the threshold.

3. **Host contention is checked directly.** Sustained load defeats both of the above: every
   sample is equally depressed, they unanimously agree, and the metric simply reads low. That
   is the exact shape of the original reports, and measured here it is severe -- in an
   interleaved A/B on a host at 3.4 load per CPU, sampling moved `unix_ipc_mib_per_s` from a
   153.8% spread to 111.1%, and neither number was usable. `os.getloadavg()` normalized by CPU
   count, read before measurement begins so it reflects other people's work rather than the
   probe's own, is what catches it.

WHAT AN INCONCLUSIVE RESULT DOES
================================
It depends on where the measurement was taken, and that split is declared, not inferred:

  * **In CI** (`CI` set by the runner, or `--enforcement required`): a hard failure, with a
    diagnosis that says plainly it is not a budget breach. A hosted runner is dedicated; a
    contended one is an infrastructure defect, and swallowing it would hide the fact that the
    performance lane has stopped producing evidence.

  * **On a developer or agent host** (`--enforcement auto`, the default): reported as ADVISORY
    for `load_sensitive` budgets only, and does not fail. This is the sanctioned "unreliable on
    a loaded host -- advisory here, enforced in CI" split. It is narrow on purpose: every
    deterministic budget still gates, and a load-sensitive budget whose samples *agree* still
    gates in full. A stable measurement is never downgraded.

`load_sensitive` is declared per budget and is mandatory. A budget that omits it is a policy
error, not a default -- picking the permissive default silently is how these gates decay.
"""

from __future__ import annotations

import argparse
import json
import os
import shutil
import statistics
import subprocess
import sys
import tomllib
from dataclasses import dataclass
from pathlib import Path

ROOT = Path(__file__).resolve().parents[3]
POLICY = ROOT / "configs/qualification/rust_performance.toml"


class PolicyError(Exception):
    """The budget policy is unusable, which is a failure and never a reason to proceed."""


@dataclass(frozen=True)
class Measurement:
    """One budget's samples, and what they say about their own trustworthiness."""

    name: str
    samples: tuple[float, ...]

    @property
    def value(self) -> float:
        """The median. Robust to the single interfered-with sample a mean would absorb."""
        return statistics.median(self.samples)

    @property
    def relative_spread(self) -> float:
        """(max - min) / median, for the evidence report only.

        Never gated on. See the module docstring: a percentage of a near-zero median says
        nothing useful, and the question that matters is whether the scatter crosses the budget.
        """
        if len(self.samples) < 2:
            return 0.0
        median = self.value
        if median == 0:
            return float("inf")
        return (max(self.samples) - min(self.samples)) / abs(median)

    def breaches(self, budget: dict) -> tuple[int, int]:
        """How many samples fail this budget, and how many there were.

        A unanimous verdict is trustworthy however scattered the samples are; a split one is not
        trustworthy however tight they look.
        """
        failing = 0
        for sample in self.samples:
            if ("maximum" in budget and sample > float(budget["maximum"])) or (
                "minimum" in budget and sample < float(budget["minimum"])
            ):
                failing += 1
        return failing, len(self.samples)


@dataclass(frozen=True)
class MeasurementPolicy:
    samples: int
    warmup_runs: int
    max_host_load_per_cpu: float


def load_policy(path: Path = POLICY) -> tuple[dict, MeasurementPolicy]:
    document = tomllib.loads(path.read_text())
    budgets = document.get("budget", {})
    if not budgets:
        raise PolicyError("no Rust performance budgets")
    for name, budget in budgets.items():
        if "load_sensitive" not in budget:
            raise PolicyError(
                f"budget {name!r} does not declare `load_sensitive`. Say whether a loaded host "
                "can move this number; defaulting it silently is how a gate stops meaning "
                "anything."
            )
        if not isinstance(budget["load_sensitive"], bool):
            raise PolicyError(f"budget {name!r}: `load_sensitive` must be a boolean")
    block = document.get("measurement")
    if not block:
        raise PolicyError("policy declares no [measurement] block")
    try:
        measurement = MeasurementPolicy(
            samples=int(block["samples"]),
            warmup_runs=int(block["warmup_runs"]),
            max_host_load_per_cpu=float(block["max_host_load_per_cpu"]),
        )
    except (KeyError, TypeError, ValueError) as error:
        raise PolicyError(f"[measurement] is incomplete: {error}") from error
    if measurement.samples < 3:
        # A median of two is the mean of two, and inherits every problem this exists to fix.
        raise PolicyError("[measurement].samples must be at least 3 for a meaningful median")
    if measurement.max_host_load_per_cpu <= 0:
        raise PolicyError("[measurement].max_host_load_per_cpu must be positive")
    return budgets, measurement


def load_results(paths: list[Path]) -> dict[str, Measurement]:
    """Results handed in from another lane -- the gateway harness, GKE hardware evidence.

    These arrive as one number from a machine this process never saw, so they carry a single
    sample and no dispersion claim. They are evaluated exactly as they were before.
    """
    merged: dict[str, Measurement] = {}
    for path in paths:
        obj = json.loads(path.read_text())
        if not isinstance(obj, dict):
            raise ValueError(f"{path}: expected JSON object")
        for key, value in obj.items():
            merged[key] = Measurement(name=key, samples=(float(value),))
    return merged


def host_load_per_cpu() -> float | None:
    """One-minute load average per CPU, or None where the platform does not report it.

    Deliberately the raw system view rather than this process's own CPU time: the condition
    being detected is *other people's work*, which is what depressed every reading in the
    original reports.
    """
    try:
        one_minute = os.getloadavg()[0]
    except (OSError, AttributeError):
        return None
    cpus = os.cpu_count() or 1
    return one_minute / cpus


def run_probe(cargo: str) -> dict[str, float]:
    proc = subprocess.run(
        [cargo, "run", "-p", "mindclade-rust-perf-probe", "--release", "--locked", "--quiet"],
        cwd=ROOT,
        text=True,
        capture_output=True,
        check=True,
    )
    line = next(
        (line for line in reversed(proc.stdout.splitlines()) if line.strip().startswith("{")), None
    )
    if line is None:
        raise RuntimeError("Rust performance probe did not emit JSON")
    return {k: float(v) for k, v in json.loads(line).items()}


def measure_portable(policy: MeasurementPolicy) -> dict[str, Measurement]:
    """Run the probe `warmup_runs + samples` times and keep the samples after the warmup.

    The warmup runs are discarded rather than averaged in. The first execution of a freshly
    linked binary pays for page faults, the dynamic loader, and a cold file cache, none of which
    the budget is about.
    """
    cargo = shutil.which("cargo")
    if not cargo:
        raise RuntimeError("Cargo is required for --measure")
    for _ in range(policy.warmup_runs):
        run_probe(cargo)
    collected: dict[str, list[float]] = {}
    for _ in range(policy.samples):
        for key, value in run_probe(cargo).items():
            collected.setdefault(key, []).append(value)
    return {key: Measurement(name=key, samples=tuple(values)) for key, values in collected.items()}


@dataclass(frozen=True)
class Verdict:
    failures: list[str]
    advisories: list[str]
    passed: int


def evaluate(
    budgets: dict,
    results: dict[str, Measurement],
    measurement: MeasurementPolicy,
    *,
    require_complete: bool,
    enforce_inconclusive: bool,
    load_per_cpu: float | None,
) -> Verdict:
    failures: list[str] = []
    advisories: list[str] = []
    passed = 0
    contended = load_per_cpu is not None and load_per_cpu > measurement.max_host_load_per_cpu
    for name, budget in sorted(budgets.items()):
        result = results.get(name)
        if result is None:
            if require_complete:
                failures.append(f"missing measurement: {name}")
            continue
        value = result.value
        load_sensitive = bool(budget["load_sensitive"])
        limit = budget.get("maximum", budget.get("minimum"))
        comparison = ">" if "maximum" in budget else "<"
        failing, total = result.breaches(budget)
        breaches = [f"{name}: {value} {comparison} {limit}"] if failing * 2 > total else []

        # Trustworthiness is decided before the verdict, because an untrustworthy number must
        # not be reported as a pass OR as a breach.
        reasons = []
        if load_sensitive and 0 < failing < total:
            reasons.append(
                f"{failing} of {total} samples missed the budget and {total - failing} met it, "
                f"so this run cannot say which side of {limit} the code is on"
            )
        if load_sensitive and contended and total > 1:
            reasons.append(
                f"host load was {load_per_cpu:.2f} per CPU "
                f"(limit {measurement.max_host_load_per_cpu:.2f})"
            )

        if reasons:
            printable = [round(sample, 1) for sample in result.samples]
            detail = (
                f"{name}: INCONCLUSIVE -- {'; '.join(reasons)}. "
                f"samples={printable}; median was {value:.1f}"
            )
            if breaches:
                detail += (
                    ", which is outside the budget, but on this host that is not evidence of "
                    "a regression."
                )
            else:
                detail += " and inside the budget, but that is not evidence of compliance."
            if enforce_inconclusive:
                failures.append(
                    f"{detail} This ran with enforcement required, where an unusable "
                    "measurement is a failure of the lane, not a tolerated condition."
                )
            else:
                advisories.append(detail)
            continue
        if breaches:
            failures.extend(breaches)
            continue
        passed += 1
    return Verdict(failures=failures, advisories=advisories, passed=passed)


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--results", type=Path, action="append", default=[])
    parser.add_argument("--measure", action="store_true")
    parser.add_argument("--output", type=Path)
    parser.add_argument("--require-complete", action="store_true")
    parser.add_argument("--policy", type=Path, default=POLICY)
    parser.add_argument(
        "--samples",
        type=int,
        help="Override [measurement].samples. Raising it buys stability; lowering it below the "
        "policy value is not a supported way to make a run finish faster.",
    )
    parser.add_argument(
        "--enforcement",
        choices=("auto", "required", "advisory"),
        default="auto",
        help="How an INCONCLUSIVE measurement is treated. `auto` (default) requires enforcement "
        "when CI is set and otherwise reports load-sensitive inconclusives as advisory. "
        "`required` always fails on one. `advisory` never does and is for local triage only.",
    )
    parser.add_argument(
        "--measurement-report",
        type=Path,
        help="Write every sample, the median, and the host load, as evidence.",
    )
    arguments = parser.parse_args(argv)

    try:
        budgets, measurement = load_policy(arguments.policy)
    except (OSError, PolicyError, tomllib.TOMLDecodeError) as error:
        print(error, file=sys.stderr)
        return 1
    if arguments.samples is not None:
        if arguments.samples < 3:
            parser.error("--samples must be at least 3 for a meaningful median")
        measurement = MeasurementPolicy(
            samples=arguments.samples,
            warmup_runs=measurement.warmup_runs,
            max_host_load_per_cpu=measurement.max_host_load_per_cpu,
        )

    try:
        results = load_results(arguments.results)
    except (OSError, ValueError, json.JSONDecodeError) as error:
        print(error, file=sys.stderr)
        return 1

    load_per_cpu = None
    if arguments.measure:
        load_per_cpu = host_load_per_cpu()
        try:
            results.update(measure_portable(measurement))
        except (RuntimeError, subprocess.CalledProcessError, json.JSONDecodeError) as error:
            print(error, file=sys.stderr)
            return 1

    if arguments.output:
        arguments.output.parent.mkdir(parents=True, exist_ok=True)
        flat = {name: result.value for name, result in results.items()}
        arguments.output.write_text(json.dumps(flat, sort_keys=True, indent=2) + "\n")
    if arguments.measurement_report:
        arguments.measurement_report.parent.mkdir(parents=True, exist_ok=True)
        arguments.measurement_report.write_text(
            json.dumps(
                {
                    "host_load_per_cpu": load_per_cpu,
                    "cpu_count": os.cpu_count(),
                    "samples": measurement.samples,
                    "measurements": {
                        name: {
                            "median": result.value,
                            "relative_spread": result.relative_spread,
                            "samples": list(result.samples),
                        }
                        for name, result in sorted(results.items())
                    },
                },
                sort_keys=True,
                indent=2,
            )
            + "\n"
        )

    if arguments.enforcement == "required":
        enforce = True
    elif arguments.enforcement == "advisory":
        enforce = False
    else:
        enforce = bool(os.environ.get("CI", "").strip())

    verdict = evaluate(
        budgets,
        results,
        measurement,
        require_complete=arguments.require_complete,
        enforce_inconclusive=enforce,
        load_per_cpu=load_per_cpu,
    )
    for advisory in verdict.advisories:
        print(f"ADVISORY {advisory}")
    if verdict.advisories:
        print(
            f"{len(verdict.advisories)} budget(s) could not be measured reliably on this host. "
            "They are enforced in CI, where the runner is dedicated. Re-run on an idle machine "
            "to get a verdict locally; do not move a threshold to silence one of these."
        )
    if verdict.failures:
        print("\n".join(verdict.failures), file=sys.stderr)
        return 1
    print(
        f"Rust performance policy passed ({len(budgets)} budgets; {verdict.passed} measured "
        f"and enforced; {len(verdict.advisories)} advisory)"
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
