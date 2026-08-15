#!/usr/bin/env python3
"""Evaluate Rust performance budgets and optionally produce portable probes."""

from __future__ import annotations

import argparse
import json
import shutil
import subprocess
import tomllib
from pathlib import Path

ROOT = Path(__file__).resolve().parents[3]
POLICY = ROOT / "configs/qualification/rust_performance.toml"


def load_results(paths: list[Path]) -> dict[str, float]:
    merged = {}
    for path in paths:
        obj = json.loads(path.read_text())
        if not isinstance(obj, dict):
            raise ValueError(f"{path}: expected JSON object")
        for key, value in obj.items():
            merged[key] = float(value)
    return merged


def measure_portable() -> dict[str, float]:
    cargo = shutil.which("cargo")
    if not cargo:
        raise RuntimeError("Cargo is required for --measure")
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


def evaluate(budgets: dict, results: dict[str, float], require_complete: bool) -> list[str]:
    failures = []
    for name, policy in budgets.items():
        if name not in results:
            if require_complete:
                failures.append(f"missing measurement: {name}")
            continue
        value = results[name]
        if "maximum" in policy and value > float(policy["maximum"]):
            failures.append(f"{name}: {value} > {policy['maximum']}")
        if "minimum" in policy and value < float(policy["minimum"]):
            failures.append(f"{name}: {value} < {policy['minimum']}")
    return failures


def main() -> int:
    p = argparse.ArgumentParser()
    p.add_argument("--results", type=Path, action="append", default=[])
    p.add_argument("--measure", action="store_true")
    p.add_argument("--output", type=Path)
    p.add_argument("--require-complete", action="store_true")
    a = p.parse_args()
    budgets = tomllib.loads(POLICY.read_text()).get("budget", {})
    if not budgets:
        print("no Rust performance budgets")
        return 1
    try:
        results = load_results(a.results)
    except (OSError, ValueError, json.JSONDecodeError) as e:
        print(e)
        return 1
    if a.measure:
        try:
            results.update(measure_portable())
        except (RuntimeError, subprocess.CalledProcessError, json.JSONDecodeError) as e:
            print(e)
            return 1
    if a.output:
        a.output.parent.mkdir(parents=True, exist_ok=True)
        a.output.write_text(json.dumps(results, sort_keys=True, indent=2) + "\n")
    failures = evaluate(budgets, results, a.require_complete)
    if failures:
        print("\n".join(failures))
        return 1
    measured = len(set(results) & set(budgets))
    print(f"Rust performance policy passed ({len(budgets)} budgets; {measured} measured)")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
