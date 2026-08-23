#!/usr/bin/env python3
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Enforce `production_dependency` from maturity.toml against the Go import graph.

ADR-0021 states the maturity model's central prohibition — "Scaffolded/experimental
components cannot become production dependencies by path existence alone" — and
`maturity.toml` encodes it as `production_dependency = false` on `planned`, `scaffolded`
and `experimental`. Until this checker existed, `production_dependency` appeared nowhere
in `tools/` or `ci/`: a repository-wide grep returned zero hits. Every other clause of
`maturity.toml` was enforced by check_component_maturity.py, and the one clause the whole
model exists for was read by nothing — so the record could say `experimental` while an
`implemented` component linked it from production code, and the presubmit printed a pass.

What this gate compares, and why each half is what it is:

    the declaration   components.toml, resolved by longest path prefix, because owners
                      declare components at the granularity they chose — `libs/go` is one
                      entry standing for a whole subtree, `control/registry/checkpoints`
                      is a leaf.
    the reality       tools/analysis/go_import_graph.py, the same graph the foundation
                      consumption and control-plane command gates already read.
                      Documentation and hand-written tables drift from the build; import
                      edges cannot.

Direct edges only. Every transitive path from a permitted status into a forbidden one
crosses some direct edge, so the direct edge is where the finding belongs; walking the
closure would report one root cause as a cascade of derived findings.

Run standalone with `python3 tools/analysis/check_production_dependencies.py`.
"""

from __future__ import annotations

import argparse
import datetime as dt
import re
import sys
import tomllib
from dataclasses import dataclass
from pathlib import Path

sys.dont_write_bytecode = True
HERE = Path(__file__).resolve().parent
if str(HERE) not in sys.path:
    sys.path.insert(0, str(HERE))
import go_import_graph as graphs  # noqa: E402  (path is established immediately above)

POLICY_FILE = "maturity.toml"
COMPONENTS_FILE = "components.toml"
OWNERS_FILE = "OWNERS.toml"
ADR_DIRECTORY = "docs/design"
RULE_KEY = "production_dependency"

# The same cap and the same four required fields as BAZEL_LAYER_EXCEPTIONS in
# tools/build/bazel/layers.bzl. One exception idiom in the tree is worth more than a second
# one tuned to this checker, and the properties that idiom buys are exactly the ones needed
# here: an exception that cannot outlive its date, and that cannot be renewed by editing a
# list — only by re-arguing it against an accepted decision.
MAX_EXCEPTION_DAYS = 90
_ADR_ID = re.compile(r"^ADR-\d{4}$")
_ADR_ACCEPTED = re.compile(r"(?mi)^- \*\*Status:\*\* Accepted\s*$")


@dataclass(frozen=True)
class DependencyException:
    """One time-boxed grant. The four fields are required; the dataclass is the schema.

    layers.bzl validates "exactly these four keys, no more, no fewer" at runtime because its
    entries arrive as literal Starlark dicts. Here the same guarantee is structural: a
    missing field is a TypeError at import and an extra one is an unexpected-argument error,
    both of which fail before this module can report anything at all.
    """

    owner: str
    adr: str
    reason: str
    expires_on: str


# Exact `(importer package, importee package)` Go package edges, repository-relative.
#
# Keys are package edges rather than component edges deliberately. A component-level key
# would cover every package in the subtree, so `control/ingestion/adapters/kubernetes`
# would silently inherit a grant that was argued for `control/ingestion` alone. Keys are
# tuples rather than the `"a -> b"` strings layers.bzl uses because there is no Starlark
# round-trip to survive here, and a key that cannot be mis-parsed is one failure mode fewer.
#
# An entry here is not an allowlist row. `_exception_findings` rejects it when the owner is
# not a team in OWNERS.toml, when the ADR is not accepted, when the reason is empty, when
# `expires_on` is in the past or more than 90 days out, or when the edge it names is no
# longer a live violation. A stale grant fails the build rather than sitting here covering
# whatever appears at that path next.
# Empty, and that is the intended steady state rather than a table waiting to be filled.
#
# It held one entry: control/ingestion (implemented) importing control/orchestration, which was
# then experimental because ten of its files were const scaffold_* placeholders. That entry
# named its own two honest resolutions -- "control/orchestration advancing on its own evidence,
# or the shared stage vocabulary moving to a package whose status matches it" -- and explicitly
# ruled out the third, "editing a status until this check passes". The first happened: those ten
# files carry implementations with tests, the component advanced to implemented on that
# evidence, and the edge stopped being a violation. The grant went stale in the same change,
# which is what this checker is built to notice.
PRODUCTION_DEPENDENCY_EXCEPTIONS: dict[tuple[str, str], DependencyException] = {}


@dataclass(frozen=True)
class Policy:
    statuses: frozenset[str]
    forbidden: frozenset[str]


@dataclass(frozen=True)
class Component:
    name: str
    path: str
    status: str


def load_policy(root: Path) -> tuple[Policy | None, list[str]]:
    """Read from maturity.toml the statuses that may not be depended on.

    The forbidden set is derived, never hard-coded, so maturity.toml stays the source of
    truth its own documentation claims it is: moving `production_dependency = false` onto
    `deprecated` starts gating deprecated components without touching this file.

    Every integrity failure below is a distinct way this gate could quietly stop gating. A
    typo in a `[rules.<status>]` header, a `production_dependency = "false"` string that is
    truthy in Python, or the wholesale removal of the flag would each leave a checker that
    runs, prints a pass, and enforces nothing. That is the precise failure this module
    exists to end, so it is reported rather than absorbed.
    """
    errors: list[str] = []
    document = tomllib.loads((root / POLICY_FILE).read_text(encoding="utf-8"))
    raw_statuses = document.get("statuses")
    if not isinstance(raw_statuses, list) or not raw_statuses:
        return None, [f"{POLICY_FILE}: `statuses` must be a non-empty list"]
    statuses = frozenset(str(value) for value in raw_statuses)

    rules = document.get("rules", {})
    if not isinstance(rules, dict):
        return None, [f"{POLICY_FILE}: `rules` must be a table"]
    for status in sorted(set(rules) - statuses):
        errors.append(
            f"{POLICY_FILE}: [rules.{status}] names a status that is not in `statuses`, "
            "so its rules apply to nothing"
        )

    forbidden: set[str] = set()
    for status, rule in sorted(rules.items()):
        if not isinstance(rule, dict) or RULE_KEY not in rule:
            continue
        value = rule[RULE_KEY]
        if not isinstance(value, bool):
            errors.append(
                f"{POLICY_FILE}: [rules.{status}].{RULE_KEY} is {value!r}, not a boolean; "
                "a non-boolean is truthy in Python and would silently permit the dependency"
            )
            continue
        if value is False:
            forbidden.add(status)
    if not forbidden and not errors:
        errors.append(
            f"{POLICY_FILE}: no status declares `{RULE_KEY} = false`, so this gate would "
            "permit every dependency; ADR-0021 requires the prohibition on at least "
            "planned, scaffolded and experimental"
        )
    return Policy(statuses, frozenset(forbidden)), errors


def load_components(root: Path, policy: Policy) -> tuple[list[Component], list[str]]:
    """Read components.toml, refusing anything this gate could not classify.

    Fails closed on an unknown status for the reason check_go_layers.py fails closed on an
    unplaceable package: a status this module does not recognise is not "nothing to check",
    it is an edge whose consumer side cannot be evaluated, and treating it as a permitted
    consumer would exempt precisely the least-reviewed line in the file. Duplicate paths
    fail for the same reason — two statuses for one path make every edge touching it
    ambiguous, and choosing either one is a guess.
    """
    errors: list[str] = []
    document = tomllib.loads((root / COMPONENTS_FILE).read_text(encoding="utf-8"))
    components: list[Component] = []
    seen: dict[str, str] = {}
    for entry in document.get("component", []):
        name = str(entry.get("name", ""))
        path = str(entry.get("path", ""))
        status = str(entry.get("status", ""))
        if not path:
            errors.append(f"{COMPONENTS_FILE}: component {name!r} declares no path")
            continue
        if status not in policy.statuses:
            errors.append(
                f"{COMPONENTS_FILE}: component {name!r} declares status {status!r}, which "
                f"{POLICY_FILE} does not list; this gate cannot classify it as a permitted "
                "or a forbidden dependency"
            )
            continue
        if path in seen:
            errors.append(
                f"{COMPONENTS_FILE}: path {path!r} is declared twice ({seen[path]!r} and "
                f"{name!r}); one path cannot carry two statuses"
            )
            continue
        seen[path] = name
        components.append(Component(name, path, status))
    return components, errors


def _resolver(components: list[Component]):
    """Map a repository-relative package path to the component that declares it.

    Longest prefix wins, so a subtree entry and a leaf entry inside it can coexist and each
    means exactly what it says.
    """
    ordered = sorted(components, key=lambda component: -len(component.path))

    def resolve(relative: str) -> Component | None:
        for component in ordered:
            if relative == component.path or relative.startswith(component.path + "/"):
                return component
        return None

    return resolve


def _owner_teams(root: Path) -> set[str]:
    document = tomllib.loads((root / OWNERS_FILE).read_text(encoding="utf-8"))
    return {
        entry["team"]
        for entry in document.get("owners", [])
        if isinstance(entry, dict) and isinstance(entry.get("team"), str)
    }


def _accepted_adrs(root: Path) -> set[str]:
    accepted: set[str] = set()
    for path in (root / ADR_DIRECTORY).glob("adr-*.md"):
        if _ADR_ACCEPTED.search(path.read_text(encoding="utf-8", errors="replace")):
            accepted.add(path.name[:8].upper())
    return accepted


def _exception_findings(
    root: Path,
    exceptions: dict[tuple[str, str], DependencyException],
    excused: set[tuple[str, str]],
    today: dt.date,
) -> list[str]:
    """Validate every exception and report the ones that no longer describe reality.

    An unvalidated exception table is an allowlist, and an allowlist grows. Each clause
    below is one way this table could otherwise become permanent: no owner to ask, no
    accepted decision behind it, no stated reason, no date it dies on, or an edge that was
    resolved long ago while the entry stayed on to cover its successor.

    `excused` holds the edges that would have been findings but for an exception, so a grant
    is stale both when the import is gone and when the import stopped violating anything —
    a promoted dependency leaves the grant meaningless, and a meaningless grant is how the
    next real violation at that path gets waved through.
    """
    errors: list[str] = []
    teams = _owner_teams(root)
    if not teams:
        errors.append(f"{OWNERS_FILE}: no owner teams declared; exceptions cannot be attributed")
    accepted = _accepted_adrs(root)
    for (source, target), exception in sorted(exceptions.items()):
        label = f"exception {source} -> {target}"
        if not exception.owner:
            errors.append(f"{label}: owner is required and is empty")
        elif exception.owner not in teams:
            errors.append(
                f"{label}: owner {exception.owner!r} is not a team declared in {OWNERS_FILE}"
            )
        if not _ADR_ID.match(exception.adr):
            errors.append(f"{label}: adr {exception.adr!r} is not of the form ADR-NNNN")
        elif exception.adr not in accepted:
            errors.append(
                f"{label}: {exception.adr} is not an accepted decision in {ADR_DIRECTORY}"
            )
        if not exception.reason.strip():
            errors.append(f"{label}: reason is required and is empty")
        try:
            expires = dt.date.fromisoformat(exception.expires_on)
        except ValueError:
            errors.append(f"{label}: expires_on {exception.expires_on!r} is not an ISO date")
        else:
            remaining = (expires - today).days
            if remaining < 0:
                errors.append(
                    f"{label}: expired on {exception.expires_on}; resolve the dependency or "
                    "re-argue the exception against an accepted ADR"
                )
            elif remaining > MAX_EXCEPTION_DAYS:
                errors.append(
                    f"{label}: expires_on {exception.expires_on} is {remaining} days out, "
                    f"more than the {MAX_EXCEPTION_DAYS}-day cap"
                )
        if (source, target) not in excused:
            errors.append(
                f"{label}: {source} no longer imports {target} in production code, or the "
                "import no longer violates the maturity model; the exception is stale and "
                "must be deleted"
            )
    return errors


def check(root: Path, *, today: dt.date | None = None) -> list[str]:
    today = today or dt.date.today()
    policy, errors = load_policy(root)
    if policy is None:
        return errors
    components, component_errors = load_components(root, policy)
    errors.extend(component_errors)
    resolve = _resolver(components)

    # The production graph, not the graph with tests. `production_dependency` says
    # production; go_import_graph excludes `_test.go` for the matching reason ("a binary
    # does not link them"); and a test that exercises an experimental package is how that
    # package earns its way to implemented, so gating test edges would make the model's
    # lower three statuses unreachable by design.
    #
    # services/control_plane/tests -> control/routing (experimental) is exactly this shape
    # and is deliberately not a finding: that edge exists only in _test.go files, so it is
    # absent from the graph below. The rule is per file, not per package — a non-test file
    # in that same directory importing control/routing would still be caught.
    module = graphs.module_path(root)
    graph = graphs.import_graph(root, False)

    def relative(package: str) -> str:
        return "." if package == module else package[len(module) + 1 :]

    findings: list[str] = []
    excused: set[tuple[str, str]] = set()
    for package in sorted(graph):
        source_path = relative(package)
        source = resolve(source_path)
        for imported in sorted(graph[package]):
            target_path = relative(imported)
            target = resolve(target_path)
            # An undeclared importee carries no status, so no rule attaches to it and this
            # gate has nothing to say about it. That hole is real, and it is closed where it
            # belongs: check_component_maturity.py fails closed on undeclared production Go
            # under control/ and libs/. Restating the declaration requirement here would
            # double-report one defect and let the two definitions of "declared" drift.
            if target is None or target.status not in policy.forbidden:
                continue
            # A component may reach into its own subtree freely. Both sides carry the same
            # status by definition, so there is no status transition to police.
            if source is not None and source.path == target.path:
                continue
            if source is not None and source.status in policy.forbidden:
                # An experimental component depending on another one claims no readiness
                # its dependency cannot support. The prohibition protects production
                # callers, and neither of these is one.
                continue
            if (source_path, target_path) in PRODUCTION_DEPENDENCY_EXCEPTIONS:
                excused.add((source_path, target_path))
                continue
            if source is None:
                # Fail closed. An undeclared package holding a real non-test Go file with a
                # real import is production code the record has never heard of; a scaffold
                # placeholder is a licence header, a package clause and a `const
                # scaffold_*`, and imports nothing, so it can never reach this line.
                # Skipping the undeclared importer would exempt the ~17.5k lines of
                # undeclared production Go under services/ — including deployable services
                # that already have release-catalog entries — which is the population most
                # in need of the rule, not least.
                findings.append(
                    f"{source_path} has no {COMPONENTS_FILE} entry and imports {target_path}, "
                    f"declared {target.status!r} by {target.name}, whose status forbids being "
                    f"a production dependency; declare {source_path} or drop the import"
                )
                continue
            findings.append(
                f"{source.name} ({source_path}) is declared {source.status!r} and imports "
                f"{target_path} from {target.name}, declared {target.status!r}; "
                f"{POLICY_FILE} sets `{RULE_KEY} = false` for {target.status!r}"
            )

    errors.extend(sorted(findings))
    errors.extend(_exception_findings(root, PRODUCTION_DEPENDENCY_EXCEPTIONS, excused, today))
    return errors


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--repo", type=Path, default=Path(__file__).resolve().parents[2])
    args = parser.parse_args(argv)
    errors = check(args.repo.resolve())
    for error in errors:
        print(error, file=sys.stderr)
    if errors:
        print(
            f"production dependency check failed with {len(errors)} finding(s)",
            file=sys.stderr,
        )
        return 1
    print("production dependency check passed")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
