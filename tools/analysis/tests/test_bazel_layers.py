# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

from __future__ import annotations

import importlib.util
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[3]


def load(name: str, path: Path):
    spec = importlib.util.spec_from_file_location(name, path)
    if spec is None or spec.loader is None:
        raise RuntimeError(f"unable to load {path}")
    module = importlib.util.module_from_spec(spec)
    sys.modules[name] = module
    spec.loader.exec_module(module)
    return module


layers = load("check_bazel_layers", ROOT / "tools/analysis/check_bazel_layers.py")
POLICY = layers.load_policy(ROOT / "tools/build/bazel/layers.bzl")


def graph(*edges: tuple[str, str]) -> str:
    inputs: dict[str, list[str]] = {}
    for source, target in edges:
        inputs.setdefault(source, []).append(target)
        inputs.setdefault(target, [])
    rules = []
    for source, targets in inputs.items():
        rule_inputs = "".join(f'<rule-input name="{target}"/>' for target in targets)
        rules.append(f'<rule class="filegroup" name="{source}">{rule_inputs}</rule>')
    return f'<query version="2">{"".join(rules)}</query>'


def messages(*edges: tuple[str, str]) -> list[str]:
    parsed = layers.direct_rule_edges(graph(*edges))
    return [violation.render() for violation in layers.check_edges(parsed, POLICY)]


def test_permitted_dependency_directions_pass() -> None:
    assert not messages(
        ("//research/benchmarks:runner", "//serving/runtime:runtime"),
        ("//services/control_plane:server", "//libs/go/servicekit:servicekit"),
        ("//apps/console:bundle", "//sdk/typescript:client"),
    )


def test_serving_cannot_depend_on_training_or_research() -> None:
    violations = messages(
        ("//serving/runtime:runtime", "//training/runtime:trainer"),
        ("//services/runtime_gateway:gateway", "//research/benchmarks:benchmark"),
    )
    assert len(violations) == 2
    assert any("published model contracts" in message for message in violations)
    assert any("must not depend on research" in message for message in violations)


def test_cross_cutting_boundaries_are_enforced() -> None:
    violations = messages(
        ("//apps/admin:admin", "//services/control_plane:server"),
        ("//models/registry:registry", "//infra/kubernetes:manifests"),
        ("//tools/analysis:checker", "//research/experiments/baselines:baseline"),
    )
    assert len(violations) == 3
    assert any("generated SDKs" in message for message in violations)
    assert any("deployment infrastructure" in message for message in violations)
    assert any("only research may consume experiments" in message for message in violations)


def test_external_and_source_file_inputs_are_ignored() -> None:
    xml = """<query version="2">
      <rule class="py_library" name="//serving/runtime:runtime">
        <rule-input name="@pypi//numpy:numpy"/>
        <rule-input name="//serving/runtime:runtime.py"/>
      </rule>
    </query>"""
    assert not layers.check_edges(layers.direct_rule_edges(xml), POLICY)


def test_empty_query_cannot_pass_vacuously() -> None:
    try:
        layers.direct_rule_edges('<query version="2"/>')
    except layers.PolicyError as error:
        assert "no rules" in str(error)
    else:
        raise AssertionError("empty query unexpectedly passed")


def test_exceptions_must_be_exact_and_are_rejected_when_stale() -> None:
    policy = layers.Policy(
        POLICY.groups,
        POLICY.forbidden_edges,
        {"//serving/runtime:runtime -> //training/runtime:trainer": "ADR-0042: migration"},
    )
    edge = ("//serving/runtime:runtime", "//training/runtime:trainer")
    assert not layers.check_edges({edge}, policy)
    violations = layers.check_edges(set(), policy)
    assert len(violations) == 1
    assert "stale layer exception" in violations[0].message
