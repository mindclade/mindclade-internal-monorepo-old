# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Declaration coverage: production Go that components.toml has never heard of.

The gate these cover exists because an undeclared package was neither pass nor fail. Every
rule in maturity.toml is a rule about a component that is declared, so a directory with no
entry inherited no status, and the model's central prohibition — production may not depend on
planned/scaffolded/experimental — had nothing to attach to. `control/evidence` sat in that
hole with 890 lines of signed production-eligibility policy.

The scaffold cases matter as much as the undeclared ones. If a `const scaffold_<name>`
placeholder counted as production Go, the gate would demand a status for sixteen reserved
control/ directories that have nothing to have a status about, and the pressure would be to
declare ten placeholders rather than to fix the one real omission.
"""

from __future__ import annotations

import importlib.util
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[3]

LICENCE = (
    "// Copyright © 2026 Mindclade, LLC. All Rights Reserved.\n"
    "// Mindclade Proprietary and Confidential.\n"
    "// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary\n"
    "//\n"
)


def load(name: str, path: Path):
    spec = importlib.util.spec_from_file_location(name, path)
    # None means the path is not importable, which is how a drifting ROOT (parents[3]) goes
    # wrong. Raised rather than asserted: `python -O` drops asserts, and this runs at import
    # time where the message is all anyone gets.
    if spec is None or spec.loader is None:
        raise RuntimeError(f"unable to load {path}")
    module = importlib.util.module_from_spec(spec)
    sys.modules[name] = module
    spec.loader.exec_module(module)
    return module


maturity = load("check_component_maturity", ROOT / "tools/analysis/check_component_maturity.py")


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
