#!/usr/bin/env python3
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#
"""Enforce the explicit repository architecture against Bazel's direct target graph."""

from __future__ import annotations

import argparse
import ast
import datetime as dt
import os
import re
import subprocess
import sys
import tomllib
import xml.etree.ElementTree as ET
from dataclasses import dataclass
from pathlib import Path

POLICY_FILE = Path("tools/build/bazel/layers.bzl")
OWNERS_FILE = Path("OWNERS.toml")
ADR_DIRECTORY = Path("docs/design")
MAX_EXCEPTION_DAYS = 90
_ADR_ID = re.compile(r"^ADR-\d{4}$")


class PolicyError(ValueError):
    """The checked-in layer policy is malformed."""


@dataclass(frozen=True)
class LayerException:
    owner: str
    adr: str
    reason: str
    expires_on: dt.date


@dataclass(frozen=True)
class Policy:
    layers: dict[str, tuple[str, ...]]
    allow_matrix: dict[str, frozenset[str]]
    exceptions: dict[str, LayerException]


@dataclass(frozen=True)
class RuleGraph:
    rules: frozenset[str]
    edges: frozenset[tuple[str, str]]


@dataclass(frozen=True, order=True)
class Violation:
    source: str
    target: str
    message: str

    def render(self) -> str:
        if self.target:
            return f"{self.source} -> {self.target}: {self.message}"
        return f"{self.source}: {self.message}"


def _literal_assignments(path: Path) -> dict[str, object]:
    tree = ast.parse(path.read_text(encoding="utf-8"), filename=str(path))
    assignments: dict[str, object] = {}
    wanted = {
        "BAZEL_LAYERS",
        "BAZEL_LAYER_ALLOW_MATRIX",
        "BAZEL_LAYER_EXCEPTIONS",
    }
    for node in tree.body:
        if not isinstance(node, ast.Assign) or len(node.targets) != 1:
            continue
        target = node.targets[0]
        if isinstance(target, ast.Name) and target.id in wanted:
            try:
                assignments[target.id] = ast.literal_eval(node.value)
            except (ValueError, TypeError) as error:
                raise PolicyError(f"{path}: {target.id} must be literal data") from error
    missing = wanted - assignments.keys()
    if missing:
        raise PolicyError(f"{path}: missing assignment(s): {', '.join(sorted(missing))}")
    return assignments


def _repository_root(policy_path: Path) -> Path:
    try:
        return policy_path.resolve().parents[3]
    except IndexError as error:
        raise PolicyError(f"cannot derive repository root from {policy_path}") from error


def _owner_teams(repo: Path) -> set[str]:
    owners_path = repo / OWNERS_FILE
    try:
        raw = tomllib.loads(owners_path.read_text(encoding="utf-8"))
    except (OSError, tomllib.TOMLDecodeError) as error:
        raise PolicyError(f"cannot read {owners_path}: {error}") from error
    teams = {
        entry.get("team")
        for entry in raw.get("owners", [])
        if isinstance(entry, dict) and isinstance(entry.get("team"), str)
    }
    if not teams:
        raise PolicyError(f"{owners_path}: no owner teams declared")
    return teams


def _accepted_adrs(repo: Path) -> set[str]:
    accepted: set[str] = set()
    for path in (repo / ADR_DIRECTORY).glob("adr-*.md"):
        identifier = path.name[:8].upper()
        text = path.read_text(encoding="utf-8")
        if re.search(r"(?mi)^- \*\*Status:\*\* Accepted\s*$", text):
            accepted.add(identifier)
    return accepted


def load_policy(path: Path, *, today: dt.date | None = None) -> Policy:
    """Load and validate the literal Starlark policy plus exception governance."""
    values = _literal_assignments(path)
    raw_layers = values["BAZEL_LAYERS"]
    if not isinstance(raw_layers, dict) or not raw_layers:
        raise PolicyError(f"{path}: BAZEL_LAYERS must be a non-empty dict")

    layers: dict[str, tuple[str, ...]] = {}
    for name, raw_patterns in raw_layers.items():
        if not isinstance(name, str) or not isinstance(raw_patterns, list) or not raw_patterns:
            raise PolicyError(f"{path}: every layer needs a name and package patterns")
        patterns: list[str] = []
        for pattern in raw_patterns:
            if not isinstance(pattern, str) or not pattern.startswith("//"):
                raise PolicyError(f"{path}: invalid package pattern {pattern!r} in {name}")
            patterns.append(pattern)
        layers[name] = tuple(patterns)

    raw_matrix = values["BAZEL_LAYER_ALLOW_MATRIX"]
    if not isinstance(raw_matrix, dict):
        raise PolicyError(f"{path}: BAZEL_LAYER_ALLOW_MATRIX must be a dict")
    layer_names = set(layers)
    matrix_names = set(raw_matrix) if all(isinstance(key, str) for key in raw_matrix) else set()
    if matrix_names != layer_names:
        missing = layer_names - matrix_names
        extra = matrix_names - layer_names
        raise PolicyError(
            f"{path}: allow matrix must declare every layer exactly once "
            f"(missing={sorted(missing)}, unknown={sorted(extra)})"
        )
    allow_matrix: dict[str, frozenset[str]] = {}
    for source, raw_destinations in raw_matrix.items():
        if not isinstance(raw_destinations, list) or not raw_destinations:
            raise PolicyError(f"{path}: allow matrix entry {source!r} must be a non-empty list")
        if not all(isinstance(destination, str) for destination in raw_destinations):
            raise PolicyError(f"{path}: allow matrix entry {source!r} contains a non-string")
        unknown = set(raw_destinations) - layer_names
        if unknown:
            raise PolicyError(
                f"{path}: allow matrix entry {source!r} references unknown layers: {sorted(unknown)}"
            )
        if len(set(raw_destinations)) != len(raw_destinations):
            raise PolicyError(f"{path}: allow matrix entry {source!r} contains duplicates")
        allow_matrix[source] = frozenset(raw_destinations)

    raw_exceptions = values["BAZEL_LAYER_EXCEPTIONS"]
    if not isinstance(raw_exceptions, dict):
        raise PolicyError(f"{path}: BAZEL_LAYER_EXCEPTIONS must be a dict")
    repo = _repository_root(path)
    owner_teams = _owner_teams(repo)
    accepted_adrs = _accepted_adrs(repo)
    current_date = today or dt.date.today()
    exceptions: dict[str, LayerException] = {}
    for edge, metadata in raw_exceptions.items():
        if (
            not isinstance(edge, str)
            or edge.count(" -> ") != 1
            or not all(label.startswith("//") for label in edge.split(" -> "))
            or not isinstance(metadata, dict)
        ):
            raise PolicyError(f"{path}: exception {edge!r} must identify one exact internal edge")
        expected_keys = {"owner", "adr", "reason", "expires_on"}
        if set(metadata) != expected_keys:
            raise PolicyError(
                f"{path}: exception {edge!r} must contain exactly {sorted(expected_keys)}"
            )
        owner = metadata["owner"]
        adr = metadata["adr"]
        reason = metadata["reason"]
        expires_raw = metadata["expires_on"]
        if not isinstance(owner, str) or owner not in owner_teams:
            raise PolicyError(f"{path}: exception {edge!r} has unknown owner {owner!r}")
        if not isinstance(adr, str) or not _ADR_ID.fullmatch(adr) or adr not in accepted_adrs:
            raise PolicyError(f"{path}: exception {edge!r} must cite an existing accepted ADR")
        if not isinstance(reason, str) or not reason.strip():
            raise PolicyError(f"{path}: exception {edge!r} needs a non-empty reason")
        if not isinstance(expires_raw, str):
            raise PolicyError(f"{path}: exception {edge!r} expires_on must be YYYY-MM-DD")
        try:
            expires_on = dt.date.fromisoformat(expires_raw)
        except ValueError as error:
            raise PolicyError(
                f"{path}: exception {edge!r} expires_on must be YYYY-MM-DD"
            ) from error
        if expires_on < current_date:
            raise PolicyError(f"{path}: exception {edge!r} expired on {expires_on}")
        if expires_on > current_date + dt.timedelta(days=MAX_EXCEPTION_DAYS):
            raise PolicyError(
                f"{path}: exception {edge!r} exceeds the {MAX_EXCEPTION_DAYS}-day maximum"
            )
        exceptions[edge] = LayerException(owner, adr, reason.strip(), expires_on)

    return Policy(layers, allow_matrix, exceptions)


def _package(label: str) -> str | None:
    if not label.startswith("//"):
        return None
    return label[2:].split(":", 1)[0].strip("/")


def _matches_pattern(package: str, pattern: str) -> bool:
    body = pattern[2:]
    if body.endswith("/..."):
        prefix = body[:-4].strip("/")
        return package == prefix or package.startswith(prefix + "/")
    return package == body.split(":", 1)[0].strip("/")


def classify(label: str, policy: Policy) -> tuple[str, ...]:
    package = _package(label)
    if package is None:
        return ()
    return tuple(
        name
        for name, patterns in policy.layers.items()
        if any(_matches_pattern(package, pattern) for pattern in patterns)
    )


def direct_rule_graph(xml: str) -> RuleGraph:
    """Return internal rules and their direct rule-to-rule input edges."""
    try:
        root = ET.fromstring(xml)
    except ET.ParseError as error:
        raise PolicyError(f"invalid Bazel query XML: {error}") from error

    rule_elements = root.findall("rule")
    rules = frozenset(
        name for rule in rule_elements if (name := rule.get("name", "")).startswith("//")
    )
    if not rules:
        raise PolicyError("Bazel query returned no internal rules; refusing to pass an empty graph")
    edges = frozenset(
        (source, target)
        for rule in rule_elements
        if (source := rule.get("name", "")) in rules
        for rule_input in rule.findall("rule-input")
        if (target := rule_input.get("name", "")) in rules and target != source
    )
    return RuleGraph(rules, edges)


def direct_rule_edges(xml: str) -> set[tuple[str, str]]:
    """Compatibility helper for callers interested only in edges."""
    return set(direct_rule_graph(xml).edges)


def check_graph(graph: RuleGraph, policy: Policy) -> list[Violation]:
    """Check classification, allowed directions, and exact exception liveness."""
    violations: list[Violation] = []
    classification: dict[str, str] = {}
    for label in sorted(graph.rules):
        matches = classify(label, policy)
        if not matches:
            violations.append(Violation(label, "", "unclassified Bazel package"))
        elif len(matches) > 1:
            violations.append(
                Violation(label, "", f"package matches multiple Bazel layers: {', '.join(matches)}")
            )
        else:
            classification[label] = matches[0]

    used_exceptions: set[str] = set()
    for source, target in sorted(graph.edges):
        source_layer = classification.get(source)
        target_layer = classification.get(target)
        if source_layer is None or target_layer is None:
            continue
        if target_layer in policy.allow_matrix[source_layer]:
            continue
        exception_key = f"{source} -> {target}"
        if exception_key in policy.exceptions:
            used_exceptions.add(exception_key)
            continue
        violations.append(
            Violation(
                source,
                target,
                f"undeclared Bazel dependency direction ({source_layer} -> {target_layer})",
            )
        )

    for stale in sorted(policy.exceptions.keys() - used_exceptions):
        violations.append(
            Violation(stale, "", "stale layer exception; remove it or restore its exact edge")
        )
    return sorted(violations)


def check_edges(edges: set[tuple[str, str]], policy: Policy) -> list[Violation]:
    """Check a synthetic edge set; production callers should use check_graph."""
    rules = frozenset(label for edge in edges for label in edge)
    if not rules and policy.exceptions:
        rules = frozenset(label for edge in policy.exceptions for label in edge.split(" -> "))
    return check_graph(RuleGraph(rules, frozenset(edges)), policy)


def query_graph(repo: Path, bazel: Path) -> str:
    command = [
        str(bazel),
        "query",
        "//...",
        "--output=xml",
        "--noimplicit_deps",
        "--order_output=no",
        "--curses=no",
        "--color=no",
    ]
    completed = subprocess.run(command, cwd=repo, capture_output=True, check=False, text=True)
    if completed.returncode:
        detail = completed.stderr.strip() or "no diagnostic output"
        raise PolicyError(f"Bazel query failed with exit {completed.returncode}:\n{detail}")
    return completed.stdout


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    default_repo = Path(
        os.environ.get("BUILD_WORKSPACE_DIRECTORY", Path(__file__).resolve().parents[2])
    )
    parser.add_argument("--repo", type=Path, default=default_repo)
    parser.add_argument("--bazel", type=Path, help="Bazel wrapper; defaults to tools/dev/bazelw")
    parser.add_argument("--query-xml", type=Path, help="Read precomputed query XML instead")
    args = parser.parse_args(argv)

    repo = args.repo.resolve()
    try:
        policy = load_policy(repo / POLICY_FILE)
        if args.query_xml:
            xml = sys.stdin.read() if str(args.query_xml) == "-" else args.query_xml.read_text()
        else:
            bazel = args.bazel or repo / "tools/dev/bazelw"
            xml = query_graph(repo, bazel)
        violations = check_graph(direct_rule_graph(xml), policy)
    except (OSError, PolicyError) as error:
        print(f"bazel layer check could not run: {error}", file=sys.stderr)
        return 2

    for violation in violations:
        print(violation.render())
    if violations:
        print(f"bazel layer check failed: {len(violations)} violation(s)")
        return 1
    print("bazel layer check passed")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
