# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

from __future__ import annotations

import importlib.util
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[3]

HEADER = "package main\n"


def load_module():
    path = ROOT / "tools/analysis/check_go_command_composition.py"
    spec = importlib.util.spec_from_file_location("check_go_command_composition", path)
    if spec is None or spec.loader is None:
        raise RuntimeError(f"unable to load {path}")
    module = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = module
    spec.loader.exec_module(module)
    return module


composition = load_module()


def write_command(root: Path, relative: str, **files: str) -> Path:
    directory = root / relative
    directory.mkdir(parents=True, exist_ok=True)
    for name, body in files.items():
        (directory / f"{name}.go").write_text(HEADER + body, encoding="utf-8")
    return directory


def rules(directory: Path, relative: str) -> set[str]:
    return set(composition.violations(directory, relative))


def test_reads_every_non_test_file_in_the_package(tmp_path: Path) -> None:
    """A sibling file compiles into the same binary as main.go."""
    relative = "services/example/cmd/example"
    directory = write_command(
        tmp_path,
        relative,
        main="func main() {}\n",
        wiring='func wire() { _, _ = servicekit.New("example") }\n',
    )
    assert rules(directory, relative) == {"servicekit.New("}


def test_ignores_test_files(tmp_path: Path) -> None:
    """The mandate exempts tests from the lower-level API ban."""
    relative = "services/example/cmd/example"
    directory = write_command(tmp_path, relative, main="func main() {}\n")
    (directory / "main_test.go").write_text(
        HEADER + 'func TestX() { _, _ = servicekit.New("example") }\n', encoding="utf-8"
    )
    assert rules(directory, relative) == set()


def test_rejects_both_spellings_of_a_detached_goroutine(tmp_path: Path) -> None:
    relative = "services/example/cmd/example"
    directory = write_command(
        tmp_path,
        relative,
        main="func main() {\n\tgo func() {}()\n\tgo start()\n}\n\nfunc start() {}\n",
    )
    findings = composition.violations(directory, relative)
    assert set(findings) == {"go-statement"}
    assert len(findings["go-statement"]) == 2


def test_rejects_a_goroutine_after_a_brace_or_semicolon(tmp_path: Path) -> None:
    """`go` is a statement, not a line: a same-line spelling still counts."""
    relative = "services/example/cmd/example"
    directory = write_command(
        tmp_path, relative, main="func main() { go start() }\n\nfunc start() {}\n"
    )
    assert rules(directory, relative) == {"go-statement"}


def test_catches_signal_notify_context(tmp_path: Path) -> None:
    relative = "services/example/cmd/example"
    directory = write_command(
        tmp_path,
        relative,
        main="func main() { _, _ = signal.NotifyContext(nil) }\n",
    )
    assert rules(directory, relative) == {"signal.Notify"}


def test_ignores_rule_names_in_comments_and_literals(tmp_path: Path) -> None:
    """Without scrubbing, an import path reads as a goroutine and a doc comment
    naming the banned call reads as the banned call."""
    relative = "services/example/cmd/example"
    body = (
        'import "go.mindclade.dev/libs/go/servicekit"\n'
        "\n"
        "// Do not call servicekit.New( here, and never write go func( either.\n"
        "/* servicekit.NewAssembly( and signal.Notify are also named here. */\n"
        "func main() {\n"
        '\t_ = "servicekit.New("\n'
        "\t_ = `go func(`\n"
        "\t_ = '\\''\n"
        "}\n"
    )
    directory = write_command(tmp_path, relative, main=body)
    assert rules(directory, relative) == set()


def test_reports_the_real_line_number(tmp_path: Path) -> None:
    """Scrubbing must preserve line structure or every report points elsewhere."""
    relative = "services/example/cmd/example"
    body = '/* a\nmultiline\ncomment */\nfunc main() { _, _ = servicekit.New("x") }\n'
    directory = write_command(tmp_path, relative, main=body)
    # HEADER occupies line 1, so the comment runs 2-4 and the call is on line 5.
    assert composition.violations(directory, relative)["servicekit.New("] == [
        f"{relative}/main.go:5: {composition.RULES[0][2]}"
    ]


def test_scope_covers_every_service_not_one_directory(tmp_path: Path) -> None:
    write_command(tmp_path, "services/control_plane/cmd/api", main="func main() {}\n")
    write_command(tmp_path, "services/studio/cmd/studio", main="func main() {}\n")
    write_command(tmp_path, "services/go_vanity/cmd/go_vanity", main="func main() {}\n")
    found = {
        path.relative_to(tmp_path).as_posix() for path in composition.command_packages(tmp_path)
    }
    assert found == {
        "services/control_plane/cmd/api",
        "services/studio/cmd/studio",
        "services/go_vanity/cmd/go_vanity",
    }


def test_non_main_packages_are_not_commands(tmp_path: Path) -> None:
    directory = tmp_path / "services/studio/internal/server"
    directory.mkdir(parents=True)
    (directory / "server.go").write_text(
        "package server\n\nfunc Run() { go start() }\n\nfunc start() {}\n", encoding="utf-8"
    )
    assert composition.command_packages(tmp_path) == []


def test_empty_scan_fails_closed(tmp_path: Path) -> None:
    """A checker that passes on zero inputs is the defect, not the guard."""
    assert composition.check(tmp_path) == ["services: no Go command packages were found"]


def test_exemption_is_pinned_to_its_rule(tmp_path: Path, monkeypatch) -> None:
    relative = "services/example/cmd/example"
    write_command(
        tmp_path,
        relative,
        main='func main() {\n\t_, _ = servicekit.New("x")\n\tgo start()\n}\n\nfunc start() {}\n',
    )
    monkeypatch.setattr(
        composition,
        "EXEMPTIONS",
        {relative: composition.Exemption(rules=frozenset({"servicekit.New("}), reason="test")},
    )
    errors = composition.check(tmp_path)
    assert len(errors) == 1
    assert "detached goroutine" in errors[0]


def test_stale_exemption_is_an_error(tmp_path: Path, monkeypatch) -> None:
    """The table can only shrink without someone editing the checker."""
    relative = "services/example/cmd/example"
    write_command(tmp_path, relative, main="func main() {}\n")
    monkeypatch.setattr(
        composition,
        "EXEMPTIONS",
        {relative: composition.Exemption(rules=frozenset({"servicekit.New("}), reason="test")},
    )
    errors = composition.check(tmp_path)
    assert len(errors) == 1
    assert "is stale" in errors[0]


def test_exemption_for_a_missing_command_is_an_error(tmp_path: Path, monkeypatch) -> None:
    write_command(tmp_path, "services/example/cmd/example", main="func main() {}\n")
    monkeypatch.setattr(
        composition,
        "EXEMPTIONS",
        {"services/gone/cmd/gone": composition.Exemption(rules=frozenset(), reason="test")},
    )
    assert composition.check(tmp_path) == [
        "services/gone/cmd/gone: exemption names a command package that does not exist"
    ]


def test_exemption_naming_an_unknown_rule_is_an_error(tmp_path: Path, monkeypatch) -> None:
    relative = "services/example/cmd/example"
    write_command(tmp_path, relative, main="func main() {}\n")
    monkeypatch.setattr(
        composition,
        "EXEMPTIONS",
        {relative: composition.Exemption(rules=frozenset({"typo.New("}), reason="test")},
    )
    errors = composition.check(tmp_path)
    assert any("unknown rule" in error for error in errors)


def test_repository_exemptions_are_well_formed() -> None:
    """Every shipped exemption names a real rule and carries a reason.

    Whether the package still exists, and whether it still trips the rule, are
    checked by `check()` against the real checkout in the architecture suite --
    a runfiles tree does not contain the command sources, so asserting it here
    would only prove the sandbox is empty.
    """
    for name, exemption in composition.EXEMPTIONS.items():
        assert exemption.rules <= composition.RULE_NAMES, name
        assert exemption.reason.strip(), name
