#!/usr/bin/env python3
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Run the declared live-datastore Go suites and make a skip an error.

THE DEFECT
==========
`go test` reports a package whose every test called `t.Skip` as `ok`. The durability suites in
this repository all skip when their DSN is unset. So the largest block of unverified production
code in the tree -- transaction rollback, SQLSTATE classification, lease fencing, atomic
audit/outbox composition, idempotent replay under real conflict semantics -- can stop being
exercised entirely, and the summary is byte-identical to the day it was fully green.

`services/studio/internal/{handoff,runlog}` gate on STUDIO_TEST_DATABASE_URL, which was set
nowhere in CI. Twenty-six tests had therefore never run in CI at all, and
`services/studio/internal/handoff/contention_test.go` records the consequence in its own header:
the redemption loop "shipped with no bound at all" because everything that touched it skipped.

THE RULE
========
For every package in `configs/qualification/live_datastore_suites.toml`:

  * every variable in `requires_environment` must be set and non-empty BEFORE the run;
  * no test may report `skip`;
  * at least `minimum_tests` top-level tests must be observed;
  * the package must emit test events at all.

WHY THIS IS NOT AN OPT-IN MODE
==============================
The brief asked for enforcement "at least on protected-main and nightly". This tool enforces
unconditionally instead, because the split buys nothing: the PostgreSQL service container is
available on `pull_request`, `push`, and `merge_group` at identical cost, and the whole defect
being fixed is that a suite could stop running without anyone noticing. An advisory mode on
pull requests would reproduce that defect exactly one event class further along -- a contributor
would see green, merge, and discover on protected main that the durability lane had been dark
for a week.

There is deliberately no flag that downgrades a skip to a warning. A developer without a local
database runs `go test` directly; they do not need this tool to lie to them.
"""

from __future__ import annotations

import argparse
import json
import os
import re
import subprocess
import sys
import tomllib
from collections.abc import Iterable, Iterator
from dataclasses import dataclass, field
from pathlib import Path

REPO = Path(__file__).resolve().parents[2]
DEFAULT_CONTRACT = REPO / "configs/qualification/live_datastore_suites.toml"
SUPPORTED_SCHEMA_VERSION = 1

_MODULE_DIRECTIVE = re.compile(r"^module\s+(\S+)", re.MULTILINE)


class ContractError(Exception):
    """The contract file itself is unusable, which is a failure, never a reason to proceed."""


@dataclass(frozen=True)
class Suite:
    identifier: str
    directory: str
    requires_environment: tuple[str, ...]
    minimum_tests: int
    import_path: str


@dataclass
class Observation:
    """What a `go test -json` stream actually said about one package."""

    tests_run: set[str] = field(default_factory=set)
    skipped: list[str] = field(default_factory=list)
    package_skipped: bool = False
    saw_any_event: bool = False


def module_path(repo: Path) -> str:
    """Read the module path from go.mod rather than hardcoding `go.mindclade.dev`."""
    match = _MODULE_DIRECTIVE.search((repo / "go.mod").read_text(encoding="utf-8"))
    if match is None:
        raise ContractError("root go.mod declares no module path")
    return match.group(1)


def load_contract(path: Path, repo: Path = REPO) -> list[Suite]:
    try:
        document = tomllib.loads(path.read_text(encoding="utf-8"))
    except (OSError, tomllib.TOMLDecodeError) as error:
        raise ContractError(f"cannot read {path}: {error}") from error
    version = document.get("schema_version")
    if version != SUPPORTED_SCHEMA_VERSION:
        raise ContractError(
            f"{path}: schema_version {version!r} is not the supported {SUPPORTED_SCHEMA_VERSION}"
        )
    entries = document.get("suite")
    if not entries:
        # An empty contract would make this gate pass while proving nothing, which is the
        # failure mode the gate exists to remove.
        raise ContractError(f"{path}: declares no suites")
    module = module_path(repo)
    suites = []
    seen: set[str] = set()
    for entry in entries:
        identifier = entry.get("id")
        directory = entry.get("directory")
        if not identifier or not directory:
            raise ContractError(f"{path}: every suite needs both `id` and `directory`")
        if identifier in seen:
            raise ContractError(f"{path}: duplicate suite id {identifier!r}")
        seen.add(identifier)
        environment = tuple(entry.get("requires_environment", ()))
        if not environment:
            raise ContractError(
                f"{path}: suite {identifier!r} declares no `requires_environment`; a live suite "
                "with no datastore requirement does not belong in this contract"
            )
        minimum = entry.get("minimum_tests")
        if not isinstance(minimum, int) or minimum < 1:
            raise ContractError(
                f"{path}: suite {identifier!r} needs a positive integer `minimum_tests`"
            )
        if not entry.get("proves"):
            raise ContractError(
                f"{path}: suite {identifier!r} does not say what it proves. If that sentence "
                "cannot be written, the suite does not belong here."
            )
        suites.append(
            Suite(
                identifier=identifier,
                directory=directory,
                requires_environment=environment,
                minimum_tests=minimum,
                import_path=f"{module}/{directory}",
            )
        )
    return suites


def missing_environment(suites: Iterable[Suite], environ: dict[str, str]) -> list[str]:
    """Variables a declared suite needs that are unset or empty.

    Checked before the run on purpose. Without it an unset DSN reports as sixty individually
    skipped tests, which is the wall of noise that let this go unnoticed in the first place.
    """
    required: dict[str, list[str]] = {}
    for suite in suites:
        for name in suite.requires_environment:
            required.setdefault(name, []).append(suite.identifier)
    return [
        f"LIVE-SUITE-001: {name} is unset or empty; suites {', '.join(users)} would skip and "
        "`go test` would report them as ok"
        for name, users in sorted(required.items())
        if not environ.get(name, "").strip()
    ]


def parse_events(lines: Iterable[str], suites: Iterable[Suite]) -> dict[str, Observation]:
    """Fold a `go test -json` stream into one `Observation` per declared package."""
    watched = {suite.import_path: Observation() for suite in suites}
    for line in lines:
        stripped = line.strip()
        if not stripped.startswith("{"):
            continue
        try:
            event = json.loads(stripped)
        except json.JSONDecodeError:
            continue
        observation = watched.get(event.get("Package", ""))
        if observation is None:
            continue
        action = event.get("Action")
        name = event.get("Test")
        if name is None:
            # A package-level skip means `go test` never ran the package at all -- no test
            # files, or a build constraint excluded them. Silently identical to `ok`.
            if action == "skip":
                observation.package_skipped = True
            continue
        observation.saw_any_event = True
        if "/" in name:
            # Subtests still count as skips, but only top-level tests count toward the floor.
            if action == "skip":
                observation.skipped.append(name)
            continue
        if action in {"pass", "fail"}:
            observation.tests_run.add(name)
        elif action == "skip":
            observation.skipped.append(name)
    return watched


def verify(suites: Iterable[Suite], observed: dict[str, Observation]) -> list[str]:
    failures = []
    for suite in suites:
        observation = observed[suite.import_path]
        if observation.package_skipped or not observation.saw_any_event:
            failures.append(
                f"LIVE-SUITE-004: {suite.identifier} ({suite.directory}) produced no test "
                "events. A package that never runs cannot fail, so `ok` here means nothing."
            )
            continue
        if observation.skipped:
            listed = ", ".join(sorted(observation.skipped)[:10])
            extra = len(observation.skipped) - 10
            more = "" if extra <= 0 else f" (+{extra} more)"
            failures.append(
                f"LIVE-SUITE-002: {suite.identifier} ({suite.directory}) skipped "
                f"{len(observation.skipped)} test(s): {listed}{more}. This suite is declared "
                "live in configs/qualification/live_datastore_suites.toml; a skip here is a "
                "durability check that did not happen, reported as a pass."
            )
        if len(observation.tests_run) < suite.minimum_tests:
            failures.append(
                f"LIVE-SUITE-003: {suite.identifier} ({suite.directory}) ran "
                f"{len(observation.tests_run)} top-level tests, below the declared floor of "
                f"{suite.minimum_tests}. Either tests were deleted, or the contract's floor is "
                "stale -- lower it only with a note saying which tests went away and why."
            )
    return failures


class GoTestRun:
    """A `go test -json` invocation that echoes human-readable output as it streams.

    The JSON stream is the machine input to `parse_events`; a reader watching the CI log still
    needs the ordinary `--- PASS` lines, so `Output` events are re-emitted verbatim.
    """

    def __init__(self, command: list[str], repo: Path) -> None:
        self.command = command
        self.repo = repo
        self.returncode = 1

    def lines(self) -> Iterator[str]:
        # Fixed argv, no shell: the package list comes from the reviewed contract, never
        # from a caller-supplied string.
        process = subprocess.Popen(
            self.command,
            cwd=self.repo,
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
            text=True,
            bufsize=1,
        )
        assert process.stdout is not None
        for line in process.stdout:
            stripped = line.strip()
            if stripped.startswith("{"):
                yield line
                try:
                    event = json.loads(stripped)
                except json.JSONDecodeError:
                    sys.stdout.write(line)
                    continue
                output = event.get("Output")
                if output:
                    sys.stdout.write(output)
            else:
                # Build failures and vet diagnostics arrive outside the JSON stream.
                sys.stdout.write(line)
        process.wait()
        self.returncode = process.returncode


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--contract", type=Path, default=DEFAULT_CONTRACT)
    parser.add_argument("--repo", type=Path, default=REPO)
    parser.add_argument(
        "--events",
        type=Path,
        help="Verify an already-captured `go test -json` stream instead of running one.",
    )
    parser.add_argument(
        "--print-packages",
        action="store_true",
        help="Print the declared package patterns and exit.",
    )
    parser.add_argument(
        "--go", default=os.environ.get("MINDCLADE_GO", "go"), help="Go binary to invoke."
    )
    arguments = parser.parse_args(argv)

    try:
        suites = load_contract(arguments.contract, arguments.repo)
    except ContractError as error:
        print(f"live datastore contract is invalid: {error}", file=sys.stderr)
        return 2

    if arguments.print_packages:
        for suite in suites:
            print(f"./{suite.directory}")
        return 0

    missing = [
        f"LIVE-SUITE-005: {suite.identifier} declares {suite.directory}, which does not exist"
        for suite in suites
        if not (arguments.repo / suite.directory).is_dir()
    ]
    if missing:
        print("\n".join(missing), file=sys.stderr)
        return 1

    if arguments.events is not None:
        try:
            lines: Iterable[str] = arguments.events.read_text(encoding="utf-8").splitlines()
        except OSError as error:
            print(f"cannot read {arguments.events}: {error}", file=sys.stderr)
            return 1
        test_status = 0
        observed = parse_events(lines, suites)
    else:
        environment_failures = missing_environment(suites, dict(os.environ))
        if environment_failures:
            print("\n".join(environment_failures), file=sys.stderr)
            return 1
        run = GoTestRun(
            [
                arguments.go,
                "test",
                "-race",
                "-count=1",
                "-json",
                *(f"./{suite.directory}" for suite in suites),
            ],
            arguments.repo,
        )
        print(f"live datastore gate: {' '.join(run.command)}", flush=True)
        observed = parse_events(run.lines(), suites)
        test_status = run.returncode

    failures = verify(suites, observed)
    for failure in failures:
        print(failure, file=sys.stderr)
    if failures:
        return 1
    if test_status != 0:
        print(f"live datastore suites failed (go test exit {test_status})", file=sys.stderr)
        return test_status
    total = sum(len(observed[suite.import_path].tests_run) for suite in suites)
    print(f"live datastore gate passed: {len(suites)} suites, {total} tests executed, 0 skipped")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
