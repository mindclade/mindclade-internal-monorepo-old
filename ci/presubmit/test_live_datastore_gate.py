# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

from __future__ import annotations

import json
from pathlib import Path

import pytest

from ci.presubmit import live_datastore_gate as gate

CONTRACT = """
schema_version = 1

[[suite]]
id = "example"
directory = "services/example"
requires_environment = ["EXAMPLE_DSN"]
minimum_tests = 2
proves = "something only a real server can show"
"""


def _repo(tmp_path: Path, contract: str = CONTRACT) -> Path:
    (tmp_path / "go.mod").write_text("module go.example.dev\n\ngo 1.26\n")
    (tmp_path / "services" / "example").mkdir(parents=True)
    (tmp_path / "contract.toml").write_text(contract)
    return tmp_path


def _event(action: str, test: str | None = None, package: str = "go.example.dev/services/example"):
    payload: dict[str, object] = {"Action": action, "Package": package}
    if test is not None:
        payload["Test"] = test
    return json.dumps(payload)


def test_contract_derives_the_import_path_from_go_mod(tmp_path: Path) -> None:
    repo = _repo(tmp_path)
    (suite,) = gate.load_contract(repo / "contract.toml", repo)
    assert suite.import_path == "go.example.dev/services/example"


def test_an_empty_contract_is_rejected(tmp_path: Path) -> None:
    # A contract with no suites would let this gate report success while proving nothing.
    repo = _repo(tmp_path, "schema_version = 1\n")
    with pytest.raises(gate.ContractError, match="declares no suites"):
        gate.load_contract(repo / "contract.toml", repo)


def test_a_suite_with_no_datastore_requirement_is_rejected(tmp_path: Path) -> None:
    contract = CONTRACT.replace('requires_environment = ["EXAMPLE_DSN"]\n', "")
    repo = _repo(tmp_path, contract)
    with pytest.raises(gate.ContractError, match="requires_environment"):
        gate.load_contract(repo / "contract.toml", repo)


def test_a_suite_that_cannot_say_what_it_proves_is_rejected(tmp_path: Path) -> None:
    contract = CONTRACT.replace('proves = "something only a real server can show"\n', "")
    repo = _repo(tmp_path, contract)
    with pytest.raises(gate.ContractError, match="does not say what it proves"):
        gate.load_contract(repo / "contract.toml", repo)


def test_an_unsupported_schema_version_is_rejected(tmp_path: Path) -> None:
    repo = _repo(tmp_path, CONTRACT.replace("schema_version = 1", "schema_version = 99"))
    with pytest.raises(gate.ContractError, match="schema_version"):
        gate.load_contract(repo / "contract.toml", repo)


def test_missing_environment_is_reported_before_anything_runs(tmp_path: Path) -> None:
    repo = _repo(tmp_path)
    suites = gate.load_contract(repo / "contract.toml", repo)
    assert gate.missing_environment(suites, {}) != []
    assert gate.missing_environment(suites, {"EXAMPLE_DSN": "   "}) != []
    assert gate.missing_environment(suites, {"EXAMPLE_DSN": "postgres://x"}) == []


def test_a_skipped_test_is_a_failure_even_though_go_test_exits_zero(tmp_path: Path) -> None:
    # THE REGRESSION GUARD. `go test` reports a package whose every test skipped as `ok`;
    # this is the single assertion that stops that reading as evidence.
    repo = _repo(tmp_path)
    suites = gate.load_contract(repo / "contract.toml", repo)
    stream = [
        _event("pass", "TestOne"),
        _event("pass", "TestTwo"),
        _event("skip", "TestLiveThing"),
    ]
    failures = gate.verify(suites, gate.parse_events(stream, suites))
    assert len(failures) == 1
    assert "LIVE-SUITE-002" in failures[0]
    assert "TestLiveThing" in failures[0]


def test_a_fully_executed_suite_passes(tmp_path: Path) -> None:
    repo = _repo(tmp_path)
    suites = gate.load_contract(repo / "contract.toml", repo)
    stream = [_event("pass", "TestOne"), _event("pass", "TestTwo")]
    assert gate.verify(suites, gate.parse_events(stream, suites)) == []


def test_a_failing_test_is_not_a_skip(tmp_path: Path) -> None:
    # A red suite is already reported by `go test`; the gate must not double-count it as a
    # coverage gap, or every genuine failure would arrive with a confusing second complaint.
    repo = _repo(tmp_path)
    suites = gate.load_contract(repo / "contract.toml", repo)
    stream = [_event("fail", "TestOne"), _event("pass", "TestTwo")]
    assert gate.verify(suites, gate.parse_events(stream, suites)) == []


def test_deleting_the_live_tests_trips_the_floor(tmp_path: Path) -> None:
    # "Nothing skipped" is trivially satisfiable by deleting the suite. The floor is the other
    # half of the contract.
    repo = _repo(tmp_path)
    suites = gate.load_contract(repo / "contract.toml", repo)
    failures = gate.verify(suites, gate.parse_events([_event("pass", "TestOne")], suites))
    assert len(failures) == 1
    assert "LIVE-SUITE-003" in failures[0]


def test_adding_tests_never_trips_the_floor(tmp_path: Path) -> None:
    repo = _repo(tmp_path)
    suites = gate.load_contract(repo / "contract.toml", repo)
    stream = [_event("pass", f"Test{index}") for index in range(9)]
    assert gate.verify(suites, gate.parse_events(stream, suites)) == []


def test_a_package_that_emits_no_events_is_a_failure(tmp_path: Path) -> None:
    repo = _repo(tmp_path)
    suites = gate.load_contract(repo / "contract.toml", repo)
    failures = gate.verify(suites, gate.parse_events([], suites))
    assert len(failures) == 1
    assert "LIVE-SUITE-004" in failures[0]


def test_a_package_level_skip_is_a_failure(tmp_path: Path) -> None:
    # Build constraints or a vanished _test.go file produce a package-level skip, which reads
    # in the summary as indistinguishable from `ok`.
    repo = _repo(tmp_path)
    suites = gate.load_contract(repo / "contract.toml", repo)
    failures = gate.verify(suites, gate.parse_events([_event("skip")], suites))
    assert len(failures) == 1
    assert "LIVE-SUITE-004" in failures[0]


def test_a_skipped_subtest_counts_but_does_not_inflate_the_floor(tmp_path: Path) -> None:
    repo = _repo(tmp_path)
    suites = gate.load_contract(repo / "contract.toml", repo)
    stream = [
        _event("pass", "TestOne"),
        _event("pass", "TestTwo"),
        _event("skip", "TestTwo/live_case"),
    ]
    observed = gate.parse_events(stream, suites)
    assert observed["go.example.dev/services/example"].tests_run == {"TestOne", "TestTwo"}
    failures = gate.verify(suites, observed)
    assert len(failures) == 1
    assert "TestTwo/live_case" in failures[0]


def test_events_from_other_packages_are_ignored(tmp_path: Path) -> None:
    repo = _repo(tmp_path)
    suites = gate.load_contract(repo / "contract.toml", repo)
    stream = [
        _event("pass", "TestOne"),
        _event("pass", "TestTwo"),
        _event("skip", "TestElsewhere", package="go.example.dev/services/other"),
    ]
    assert gate.verify(suites, gate.parse_events(stream, suites)) == []


def test_a_declared_directory_that_does_not_exist_fails(tmp_path: Path) -> None:
    repo = _repo(tmp_path, CONTRACT.replace("services/example", "services/vanished"))
    (repo / "services" / "vanished").rmdir() if (repo / "services" / "vanished").is_dir() else None
    status = gate.main(["--contract", str(repo / "contract.toml"), "--repo", str(repo)])
    assert status == 1


def test_the_repository_contract_is_loadable_and_points_at_real_packages() -> None:
    # Guards the shipped contract itself: a typo in a directory would otherwise only surface in
    # the one CI job the contract exists to protect.
    suites = gate.load_contract(gate.DEFAULT_CONTRACT)
    assert suites
    for suite in suites:
        assert (gate.REPO / suite.directory).is_dir(), suite.directory


def test_the_shipped_contract_covers_every_dsn_gated_package() -> None:
    """Any package that skips on a datastore variable must be declared, or it stays invisible.

    Finding these by grep rather than by hand is the point: the studio suites were missed for
    exactly as long as the package list lived in a workflow file nobody re-read.
    """
    declared = {suite.directory for suite in gate.load_contract(gate.DEFAULT_CONTRACT)}
    variables = ("MINDCLADE_TEST_POSTGRES_DSN", "STUDIO_TEST_DATABASE_URL")
    gated = set()
    for path in (gate.REPO / "services").rglob("*_test.go"):
        source = path.read_text(encoding="utf-8", errors="replace")
        if any(name in source for name in variables) and "t.Skip" in source:
            gated.add(str(path.parent.relative_to(gate.REPO)))
    assert gated <= declared, f"undeclared live suites: {sorted(gated - declared)}"
