# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

from __future__ import annotations

import datetime as dt
import importlib.util
import subprocess
import sys
import tempfile
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


def graph(*edges: tuple[str, str], isolated: tuple[str, ...] = ()) -> str:
    inputs: dict[str, list[str]] = {label: [] for label in isolated}
    for source, target in edges:
        inputs.setdefault(source, []).append(target)
        inputs.setdefault(target, [])
    rules = []
    for source, targets in inputs.items():
        rule_inputs = "".join(f'<rule-input name="{target}"/>' for target in targets)
        rules.append(f'<rule class="filegroup" name="{source}">{rule_inputs}</rule>')
    return f'<query version="2">{"".join(rules)}</query>'


def messages(*edges: tuple[str, str], isolated: tuple[str, ...] = ()) -> list[str]:
    parsed = layers.direct_rule_graph(graph(*edges, isolated=isolated))
    return [violation.render() for violation in layers.check_graph(parsed, POLICY)]


def test_permitted_dependency_directions_pass() -> None:
    assert not messages(
        ("//research/benchmarks:runner", "//serving/runtime:runtime"),
        ("//services/control_plane:server", "//libs/go/servicekit:servicekit"),
        ("//apps/console:bundle", "//sdk/typescript:client"),
        ("//training/runtime:trainer", "//models/reference:model"),
    )


def test_allow_matrix_rejects_undeclared_directions() -> None:
    violations = messages(
        ("//apps/admin:admin", "//services/control_plane:server"),
        ("//libs/python/runtime:runtime", "//models/reference:model"),
        ("//serving/runtime:runtime", "//training/runtime:trainer"),
        ("//services/runtime_gateway:gateway", "//research/benchmarks:benchmark"),
    )
    assert len(violations) == 4
    assert all("undeclared Bazel dependency direction" in message for message in violations)
    assert any("apps -> services" in message for message in violations)
    assert any("foundation -> offline" in message for message in violations)


def test_unclassified_and_multiply_classified_packages_fail() -> None:
    unclassified = layers.Policy(
        {"foundation": ("//libs/...",)},
        {"foundation": frozenset({"foundation"})},
        {},
    )
    graph_value = layers.RuleGraph(frozenset({"//new_domain/pkg:target"}), frozenset())
    violations = layers.check_graph(graph_value, unclassified)
    assert len(violations) == 1
    assert "unclassified" in violations[0].message

    overlapping = layers.Policy(
        {"one": ("//libs/...",), "two": ("//libs/python/...",)},
        {"one": frozenset({"one"}), "two": frozenset({"two"})},
        {},
    )
    graph_value = layers.RuleGraph(frozenset({"//libs/python/pkg:target"}), frozenset())
    violations = layers.check_graph(graph_value, overlapping)
    assert len(violations) == 1
    assert "multiple" in violations[0].message


def test_external_and_source_file_inputs_are_ignored() -> None:
    xml = """<query version="2">
      <rule class="py_library" name="//serving/runtime:runtime">
        <rule-input name="@pypi//numpy:numpy"/>
        <rule-input name="//serving/runtime:runtime.py"/>
      </rule>
    </query>"""
    assert not layers.check_graph(layers.direct_rule_graph(xml), POLICY)


def test_empty_query_cannot_pass_vacuously() -> None:
    try:
        layers.direct_rule_graph('<query version="2"/>')
    except layers.PolicyError as error:
        assert "no internal rules" in str(error)
    else:
        raise AssertionError("empty query unexpectedly passed")


def test_live_query_cannot_update_the_committed_module_lock(monkeypatch, tmp_path: Path) -> None:
    recorded: list[str] = []

    def run(command, **_kwargs):
        recorded.extend(command)
        return subprocess.CompletedProcess(command, 0, stdout=graph(isolated=("//libs/go:go",)))

    monkeypatch.setattr(layers.subprocess, "run", run)
    layers.query_graph(tmp_path, tmp_path / "tools/dev/bazelw")

    assert "--lockfile_mode=error" in recorded


def test_exceptions_must_be_exact_and_are_rejected_when_stale() -> None:
    edge = ("//serving/runtime:runtime", "//training/runtime:trainer")
    exception_key = " -> ".join(edge)
    policy = layers.Policy(
        POLICY.layers,
        POLICY.allow_matrix,
        {
            exception_key: layers.LayerException(
                "release-engineering",
                "ADR-0024",
                "bounded migration",
                dt.date.today() + dt.timedelta(days=30),
            )
        },
    )
    assert not layers.check_edges({edge}, policy)
    violations = layers.check_edges(set(), policy)
    assert len(violations) == 1
    assert "stale layer exception" in violations[0].message


def _write_policy_repo(expires_on: str) -> Path:
    root = Path(tempfile.mkdtemp(prefix="mindclade-layer-policy-"))
    policy_dir = root / "tools/build/bazel"
    design_dir = root / "docs/design"
    policy_dir.mkdir(parents=True)
    design_dir.mkdir(parents=True)
    (root / "OWNERS.toml").write_text(
        'schema_version = 1\n[[owners]]\nteam = "release-engineering"\npaths = ["**"]\n',
        encoding="utf-8",
    )
    (design_dir / "adr-0024-policy.md").write_text(
        "# ADR-0024\n\n- **Status:** Accepted\n",
        encoding="utf-8",
    )
    (policy_dir / "layers.bzl").write_text(
        'BAZEL_LAYERS = {"one": ["//one/..."]}\n'
        'BAZEL_LAYER_ALLOW_MATRIX = {"one": ["one"]}\n'
        "BAZEL_LAYER_EXCEPTIONS = {\n"
        '  "//one:a -> //one:b": {\n'
        '    "owner": "release-engineering",\n'
        '    "adr": "ADR-0024",\n'
        '    "reason": "bounded migration",\n'
        f'    "expires_on": "{expires_on}",\n'
        "  },\n"
        "}\n",
        encoding="utf-8",
    )
    return policy_dir / "layers.bzl"


def test_exception_expiry_is_bounded_to_ninety_days() -> None:
    today = dt.date(2026, 8, 20)
    valid = _write_policy_repo("2026-11-18")
    layers.load_policy(valid, today=today)

    excessive = _write_policy_repo("2026-11-19")
    try:
        layers.load_policy(excessive, today=today)
    except layers.PolicyError as error:
        assert "90-day maximum" in str(error)
    else:
        raise AssertionError("overlong exception unexpectedly passed")

    expired = _write_policy_repo("2026-08-19")
    try:
        layers.load_policy(expired, today=today)
    except layers.PolicyError as error:
        assert "expired" in str(error)
    else:
        raise AssertionError("expired exception unexpectedly passed")
