# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

from __future__ import annotations

import importlib.util
import sys
import textwrap
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


maturity = load("check_component_maturity", ROOT / "tools/analysis/check_component_maturity.py")

_MATURITY = """
schema_version = 1
statuses = ["scaffolded", "implemented", "qualified", "production"]
[rules.scaffolded]
production_dependency = false
[rules.implemented]
requires_tests = true
requires_build_target = true
[rules.qualified]
requires_tests = true
requires_qualification = true
requires_build_target = true
[rules.production]
requires_tests = true
requires_qualification = true
requires_slo = true
requires_runbook = true
requires_release_target = true
requires_build_target = true
"""

_CATALOG = """\
---
schemaVersion: 2
targets:
  go-vanity:
    releaseKind: application
    images:
      primary:
        buildTarget: //services/go_vanity:image
  protobuf-contracts:
    releaseKind: bundle
    qualificationTargets:
      - //protocols:protobuf_governance_test
"""


def build(tmp_path: Path, component: str, *, catalog: str = _CATALOG, ownership: str = "") -> Path:
    (tmp_path / "maturity.toml").write_text(_MATURITY, encoding="utf-8")
    (tmp_path / "components.toml").write_text(
        "schema_version = 1\n\n" + textwrap.dedent(component), encoding="utf-8"
    )
    (tmp_path / "architecture").mkdir(exist_ok=True)
    (tmp_path / "architecture/component_ownership.toml").write_text(
        "schema_version = 1\n" + textwrap.dedent(ownership), encoding="utf-8"
    )
    if catalog is not None:
        (tmp_path / "ci/release").mkdir(parents=True, exist_ok=True)
        (tmp_path / "ci/release/targets.yaml").write_text(catalog, encoding="utf-8")
    package = tmp_path / "svc"
    package.mkdir(exist_ok=True)
    (package / "BUILD.bazel").write_text('go_library(name = "svc")\n', encoding="utf-8")
    (package / "svc_test.go").write_text("package svc\n", encoding="utf-8")
    (tmp_path / "docs").mkdir(exist_ok=True)
    (tmp_path / "docs/slo.md").write_text("slo\n", encoding="utf-8")
    (tmp_path / "docs/runbook.md").write_text("runbook\n", encoding="utf-8")
    (tmp_path / "docs/qual.md").write_text("qualification\n", encoding="utf-8")
    return tmp_path


_PRODUCTION = """
    [[component]]
    name = "svc"
    path = "svc"
    status = "production"
    owner = "platform-control"
    tests = ["svc/svc_test.go"]
    qualification = "docs/qual.md"
    slo = "docs/slo.md"
    runbook = "docs/runbook.md"
    release_target = "go-vanity"
"""

_OWNERSHIP = """
    [component."svc"]
    owner = "platform-control"
    slo = "docs/slo.md"
    runbook = "docs/runbook.md"
"""


def test_fully_evidenced_production_component_passes(tmp_path: Path) -> None:
    root = build(tmp_path, _PRODUCTION, ownership=_OWNERSHIP)
    assert maturity.check(root) == []


def test_slo_must_name_an_existing_document(tmp_path: Path) -> None:
    root = build(
        tmp_path,
        _PRODUCTION.replace('slo = "docs/slo.md"', 'slo = "yes"'),
        ownership=_OWNERSHIP.replace('slo = "docs/slo.md"', 'slo = "yes"'),
    )
    assert "svc: slo file does not exist: yes" in maturity.check(root)


def test_runbook_must_name_an_existing_document(tmp_path: Path) -> None:
    root = build(
        tmp_path,
        _PRODUCTION.replace('runbook = "docs/runbook.md"', 'runbook = "docs/absent.md"'),
        ownership=_OWNERSHIP.replace('runbook = "docs/runbook.md"', 'runbook = "docs/absent.md"'),
    )
    assert "svc: runbook file does not exist: docs/absent.md" in maturity.check(root)


def test_release_target_must_be_in_the_closed_catalog(tmp_path: Path) -> None:
    root = build(
        tmp_path,
        _PRODUCTION.replace('release_target = "go-vanity"', 'release_target = "shipped"'),
        ownership=_OWNERSHIP,
    )
    errors = maturity.check(root)
    assert any("release target 'shipped' is not in the closed catalog" in e for e in errors)


def test_release_target_fails_closed_when_the_catalog_is_unreadable(tmp_path: Path) -> None:
    root = build(tmp_path, _PRODUCTION, catalog="schemaVersion: 2\n", ownership=_OWNERSHIP)
    errors = maturity.check(root)
    assert any("declares no targets" in e for e in errors)


def test_slo_must_agree_with_the_ownership_registry(tmp_path: Path) -> None:
    root = build(
        tmp_path,
        _PRODUCTION,
        ownership=_OWNERSHIP.replace('slo = "docs/slo.md"', 'slo = "docs/other.md"'),
    )
    errors = maturity.check(root)
    assert any("slo disagrees with architecture/component_ownership.toml" in e for e in errors)


def test_missing_ownership_record_blocks_a_declared_slo(tmp_path: Path) -> None:
    root = build(tmp_path, _PRODUCTION, ownership="")
    errors = maturity.check(root)
    assert any("slo disagrees with architecture/component_ownership.toml" in e for e in errors)
    assert any("runbook disagrees with architecture/component_ownership.toml" in e for e in errors)


def test_production_still_reports_each_missing_gate(tmp_path: Path) -> None:
    root = build(
        tmp_path,
        """
        [[component]]
        name = "svc"
        path = "svc"
        status = "production"
        owner = "platform-control"
        tests = ["svc/svc_test.go"]
        """,
    )
    errors = maturity.check(root)
    assert "svc: qualification evidence path missing" in errors
    assert "svc: production component requires slo" in errors
    assert "svc: production component requires runbook" in errors
    assert "svc: production component requires release_target" in errors


def test_matrix_reports_unreferenced_registry_evidence(tmp_path: Path) -> None:
    root = build(
        tmp_path,
        """
        [[component]]
        name = "svc"
        path = "svc"
        status = "implemented"
        owner = "platform-control"
        tests = ["svc/svc_test.go"]
        """,
        ownership=_OWNERSHIP,
    )
    (row,) = maturity.matrix(root)
    assert row["gates"]["slo"] == "registry"
    assert row["gates"]["runbook"] == "registry"
    assert row["gates"]["qualification"] == "absent"
    assert row["gates"]["release_target"] == "absent"
    assert row["satisfies"] == ["implemented"]
    assert row["production_blockers"] == ["qualification", "release_target", "runbook", "slo"]


def test_matrix_marks_catalog_coverage_in_both_directions(tmp_path: Path) -> None:
    root = build(
        tmp_path,
        """
        [[component]]
        name = "inside"
        path = "protocols/proto/mindclade/artifact/v1"
        status = "scaffolded"
        owner = "platform-control"

        [[component]]
        name = "outside"
        path = "svc"
        status = "scaffolded"
        owner = "platform-control"
        """,
    )
    gates = {row["name"]: row["gates"]["release_target"] for row in maturity.matrix(root)}
    assert gates == {"inside": "registry", "outside": "absent"}


def test_render_counts_every_production_rule(tmp_path: Path) -> None:
    root = build(tmp_path, _PRODUCTION, ownership=_OWNERSHIP)
    rendered = maturity._render(maturity.matrix(root))
    assert "components: 1" in rendered
    for gate in ("tests", "qualification", "slo", "runbook", "release_target", "build_target"):
        assert f"{gate:<16} met   1/1" in rendered
    assert "satisfies production     1/1" in rendered


LICENCE = (
    "// Copyright © 2026 Mindclade, LLC. All Rights Reserved.\n"
    "// Mindclade Proprietary and Confidential.\n"
    "// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary\n"
    "//\n"
)


def _repository(tmp_path: Path, *, declared: str = "") -> Path:
    """A minimal tree with the two policy files check() reads."""
    (tmp_path / "maturity.toml").write_text(
        'statuses = ["scaffolded", "experimental", "implemented"]\n'
        "[rules.implemented]\nrequires_tests = true\nrequires_build_target = true\n"
    )
    (tmp_path / "components.toml").write_text(declared)
    return tmp_path


def _go(path: Path, body: str) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(LICENCE + "\n" + body)


def test_undeclared_production_go_is_reported(tmp_path: Path) -> None:
    root = _repository(tmp_path)
    _go(
        root / "control/evidence/service.go",
        "package evidence\n\nfunc Evaluate() error { return nil }\n",
    )
    errors = maturity._undeclared_go_packages(root, [])
    assert len(errors) == 1
    # The message has to name the directory and the files; "something is undeclared" is not
    # actionable, and this gate's whole value is telling an owner which package to look at.
    assert errors[0].startswith("control/evidence: 1 production Go file(s)")
    assert "service.go" in errors[0]


def test_scaffold_only_directory_is_not_a_component(tmp_path: Path) -> None:
    root = _repository(tmp_path)
    _go(
        root / "control/audit/model.go",
        "// Package audit reserves the boundary defined by the production blueprint.\n"
        "package audit\n\n"
        'const scaffold_model = "control/audit/model.go"\n',
    )
    assert maturity._undeclared_go_packages(root, []) == []


def test_declaration_covers_the_whole_subtree(tmp_path: Path) -> None:
    # `libs/go` is one entry standing for its whole subtree, so a nested package it already
    # covers must not be reported. Prefix matching is on path segments: `libs/gopher` is a
    # different package and stays uncovered.
    root = _repository(tmp_path)
    _go(root / "libs/go/retry/retry.go", "package retry\n\nfunc Do() error { return nil }\n")
    _go(root / "libs/gopher/gopher.go", "package gopher\n\nfunc Do() error { return nil }\n")
    errors = maturity._undeclared_go_packages(root, ["libs/go"])
    assert [e.split(":")[0] for e in errors] == ["libs/gopher"]


def test_test_only_files_do_not_demand_a_declaration(tmp_path: Path) -> None:
    root = _repository(tmp_path)
    _go(root / "control/probe/probe_test.go", "package probe\n\nfunc TestX(t *testing.T) {}\n")
    assert maturity._undeclared_go_packages(root, []) == []


def test_scaffold_recognition_is_fail_closed(tmp_path: Path) -> None:
    # A file that merely mentions a scaffold constant alongside real code is production Go.
    # Recognising it as a scaffold would hand back the hole the gate exists to close.
    root = _repository(tmp_path)
    _go(
        root / "control/mixed/service.go",
        "package mixed\n\n"
        'const scaffold_service = "control/mixed/service.go"\n\n'
        "func Serve() error { return nil }\n",
    )
    errors = maturity._undeclared_go_packages(root, [])
    assert len(errors) == 1
    assert errors[0].startswith("control/mixed: 1 production Go file(s)")


def test_repository_control_and_libs_go_packages_are_all_declared() -> None:
    # The invariant on the real tree, not a fixture: every control/ and libs/ package holding
    # non-scaffold production Go has an entry. This is the assertion that fails when someone
    # adds a package and forgets the record.
    import tomllib

    components = tomllib.loads((ROOT / "components.toml").read_text())["component"]
    declared = sorted({c["path"] for c in components if c.get("path")})
    assert maturity._undeclared_go_packages(ROOT, declared) == []


def test_a_governed_root_can_be_narrower_than_a_top_level_directory(monkeypatch, tmp_path) -> None:
    # The entries are path prefixes, not top-level directories. That is what lets `services/`
    # be governed for the deployables whose owners have made a declaration decision without
    # claiming the whole directory is covered — the distinction between an allowlist of what
    # IS governed and a denylist of paths waved through a check that says it covers them.
    root = _repository(tmp_path)
    _go(
        root / "services/go_vanity/internal/vanity/vanity.go",
        "package vanity\n\nfunc Handler() error { return nil }\n",
    )
    _go(
        root / "services/control_plane/internal/config/config.go",
        "package config\n\nfunc Load() error { return nil }\n",
    )
    monkeypatch.setattr(maturity, "_GO_DECLARATION_GOVERNED_ROOTS", ("services/go_vanity",))
    errors = maturity._undeclared_go_packages(root, [])
    assert [e.split(":")[0] for e in errors] == ["services/go_vanity/internal/vanity"]
