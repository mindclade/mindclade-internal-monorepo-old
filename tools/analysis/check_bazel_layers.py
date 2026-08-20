#!/usr/bin/env python3
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#
"""Enforce repository architecture against Bazel's direct target graph."""

from __future__ import annotations

import argparse
import ast
import os
import re
import subprocess
import sys
import xml.etree.ElementTree as ET
from dataclasses import dataclass
from pathlib import Path

POLICY_FILE = Path("tools/build/bazel/layers.bzl")
_ADR_REASON = re.compile(r"^ADR-\d{4}:\s+\S")


class PolicyError(ValueError):
    """The checked-in layer policy is malformed."""


@dataclass(frozen=True)
class ForbiddenEdge:
    source_group: str
    target_group: str
    reason: str


@dataclass(frozen=True)
class Policy:
    groups: dict[str, tuple[str, ...]]
    forbidden_edges: tuple[ForbiddenEdge, ...]
    exceptions: dict[str, str]


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
        "BAZEL_PACKAGE_GROUPS",
        "BAZEL_FORBIDDEN_EDGES",
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


def load_policy(path: Path) -> Policy:
    """Load and validate the Starlark data shared with the root package groups."""
    values = _literal_assignments(path)
    raw_groups = values["BAZEL_PACKAGE_GROUPS"]
    if not isinstance(raw_groups, dict) or not raw_groups:
        raise PolicyError(f"{path}: BAZEL_PACKAGE_GROUPS must be a non-empty dict")

    groups: dict[str, tuple[str, ...]] = {}
    for name, raw_patterns in raw_groups.items():
        if not isinstance(name, str) or not isinstance(raw_patterns, list) or not raw_patterns:
            raise PolicyError(f"{path}: every package group needs a name and package patterns")
        patterns: list[str] = []
        for pattern in raw_patterns:
            if not isinstance(pattern, str) or not pattern.startswith("//"):
                raise PolicyError(f"{path}: invalid package pattern {pattern!r} in {name}")
            patterns.append(pattern)
        groups[name] = tuple(patterns)

    raw_edges = values["BAZEL_FORBIDDEN_EDGES"]
    if not isinstance(raw_edges, list) or not raw_edges:
        raise PolicyError(f"{path}: BAZEL_FORBIDDEN_EDGES must be a non-empty list")
    forbidden_edges: list[ForbiddenEdge] = []
    for raw_edge in raw_edges:
        if (
            not isinstance(raw_edge, list)
            or len(raw_edge) != 3
            or not all(isinstance(value, str) and value for value in raw_edge)
        ):
            raise PolicyError(f"{path}: invalid forbidden edge {raw_edge!r}")
        source_group, target_group, reason = raw_edge
        unknown = {source_group, target_group} - groups.keys()
        if unknown:
            raise PolicyError(
                f"{path}: forbidden edge references unknown group(s): {', '.join(sorted(unknown))}"
            )
        forbidden_edges.append(ForbiddenEdge(source_group, target_group, reason))

    raw_exceptions = values["BAZEL_LAYER_EXCEPTIONS"]
    if not isinstance(raw_exceptions, dict):
        raise PolicyError(f"{path}: BAZEL_LAYER_EXCEPTIONS must be a dict")
    exceptions: dict[str, str] = {}
    for edge, reason in raw_exceptions.items():
        if (
            not isinstance(edge, str)
            or edge.count(" -> ") != 1
            or not all(label.startswith("//") for label in edge.split(" -> "))
            or not isinstance(reason, str)
            or not _ADR_REASON.match(reason)
        ):
            raise PolicyError(
                f"{path}: exception {edge!r} must be an exact '//source -> //target' edge "
                "with an 'ADR-NNNN: rationale' value"
            )
        exceptions[edge] = reason

    return Policy(groups, tuple(forbidden_edges), exceptions)


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


def _in_group(label: str, patterns: tuple[str, ...]) -> bool:
    package = _package(label)
    return package is not None and any(_matches_pattern(package, pattern) for pattern in patterns)


def direct_rule_edges(xml: str) -> set[tuple[str, str]]:
    """Return direct internal rule-input edges from Bazel query XML."""
    try:
        root = ET.fromstring(xml)
    except ET.ParseError as error:
        raise PolicyError(f"invalid Bazel query XML: {error}") from error

    edges: set[tuple[str, str]] = set()
    rules = root.findall("rule")
    if not rules:
        raise PolicyError("Bazel query returned no rules; refusing to pass an empty graph")
    for rule in rules:
        source = rule.get("name", "")
        if not source.startswith("//"):
            continue
        for rule_input in rule.findall("rule-input"):
            target = rule_input.get("name", "")
            if target.startswith("//") and target != source:
                edges.add((source, target))
    return edges


def check_edges(edges: set[tuple[str, str]], policy: Policy) -> list[Violation]:
    """Check direct graph edges and reject undocumented or stale exceptions."""
    violations: list[Violation] = []
    used_exceptions: set[str] = set()
    for source, target in sorted(edges):
        for forbidden in policy.forbidden_edges:
            if not _in_group(source, policy.groups[forbidden.source_group]):
                continue
            if not _in_group(target, policy.groups[forbidden.target_group]):
                continue
            exception_key = f"{source} -> {target}"
            if exception_key in policy.exceptions:
                used_exceptions.add(exception_key)
            else:
                violations.append(
                    Violation(
                        source,
                        target,
                        f"forbidden Bazel dependency: {forbidden.reason} "
                        f"({forbidden.source_group} -> {forbidden.target_group})",
                    )
                )
            break

    for stale in sorted(policy.exceptions.keys() - used_exceptions):
        violations.append(
            Violation(stale, "", "stale layer exception; remove it or restore its exact edge")
        )
    return sorted(violations)


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
    completed = subprocess.run(
        command,
        cwd=repo,
        capture_output=True,
        check=False,
        text=True,
    )
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
        violations = check_edges(direct_rule_edges(xml), policy)
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
