# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Conservative affected Bazel-test selection for presubmit.

Bazel remains the execution authority. This module only narrows the candidate
set by computing package reverse dependencies from first-party BUILD files.
Changes to global build/toolchain/protocol policy intentionally expand to the
full test graph.
"""

from __future__ import annotations

import argparse
import json
import re
import subprocess
from collections.abc import Iterable
from dataclasses import dataclass
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
GLOBAL_PREFIXES = (
    ".github/",
    ".buildkite/",
    "ci/",
    "tools/build/",
    "tools/qualification/",
    "MODULE.bazel",
    "MODULE.bazel.lock",
    "Cargo.toml",
    "Cargo.lock",
    "go.mod",
    "go.sum",
    "flake.nix",
    "flake.lock",
    "protocols/",
    "architecture/",
    "components.toml",
    "maturity.toml",
)

RUST_PREFIXES = (
    "libs/rust/",
    "protocols/rust/",
    "protocols/proto/mindclade/runtime/",
    "services/runtime_gateway/",
    "services/runtime_host/",
    "services/artifact_proxy/",
    "services/node_agent/",
    "services/workers/ingestion/",
    "serving/runtime/",
    "Cargo.toml",
    "Cargo.lock",
    "deny.toml",
    "security/rust-supply-chain.toml",
    "tools/qualification/rust/",
    "tools/build/nix/",
    "flake.nix",
    "flake.lock",
)


def rust_qualification_required(changed: list[str]) -> bool:
    return not changed or any(
        path == prefix or path.startswith(prefix) for path in changed for prefix in RUST_PREFIXES
    )


DEP_ATTRIBUTES = ("deps", "runtime_deps", "data", "exports")
RULE_RE = re.compile(r"(?P<kind>[A-Za-z_][A-Za-z0-9_]*)\s*\((?P<body>.*?)\n\)", re.S)
NAME_RE = re.compile(r"\bname\s*=\s*[\"\']([^\"\']+)[\"\']")
LABEL_RE = re.compile(r"[\"\'](//[^\"\']+|:[A-Za-z0-9_.+\-/]+)[\"\']")
ATTR_RE = re.compile(
    r"\b(" + "|".join(DEP_ATTRIBUTES) + r")\s*=\s*(\[[^\]]*\]|[\"\'][^\"\']+[\"\'])", re.S
)


@dataclass(frozen=True)
class Target:
    label: str
    package: str
    kind: str
    deps: tuple[str, ...]

    @property
    def is_test(self) -> bool:
        return self.kind.endswith("_test") or self.kind in {"test_suite", "sh_test"}


def package_for(path: Path) -> Path | None:
    current = path if path.is_dir() else path.parent
    while current != ROOT.parent:
        if (current / "BUILD.bazel").exists() or (current / "BUILD").exists():
            return current
        if current == ROOT:
            break
        current = current.parent
    return None


def package_label(directory: Path) -> str:
    rel = directory.relative_to(ROOT).as_posix()
    return f"//{rel}" if rel != "." else "//"


def normalize_label(package: str, raw: str) -> str:
    if raw.startswith("//"):
        return raw.split("[", 1)[0]
    if raw.startswith(":"):
        return f"{package}{raw}"
    return raw


def parse_build(path: Path) -> list[Target]:
    package = package_label(path.parent)
    text = path.read_text(errors="replace")
    targets: list[Target] = []
    # Append a newline before a closing paren to let compact one-line rules
    # participate in the same parser without interpreting arbitrary macros.
    normalized = re.sub(r"\)\s*$", "\n)", text, flags=re.M)
    for match in RULE_RE.finditer(normalized):
        kind, body = match.group("kind"), match.group("body")
        name = NAME_RE.search(body)
        if not name:
            continue
        deps: set[str] = set()
        for attr in ATTR_RE.finditer(body):
            for label in LABEL_RE.findall(attr.group(2)):
                deps.add(normalize_label(package, label))
        label = f"{package}:{name.group(1)}"
        targets.append(Target(label, package, kind, tuple(sorted(deps))))
    return targets


def graph(repo: Path = ROOT) -> tuple[dict[str, Target], dict[str, set[str]]]:
    targets: dict[str, Target] = {}
    reverse: dict[str, set[str]] = {}
    for build in sorted(repo.rglob("BUILD.bazel")):
        if any(part.startswith("bazel-") for part in build.parts):
            continue
        for target in parse_build(build):
            targets[target.label] = target
    for target in targets.values():
        for dep in target.deps:
            reverse.setdefault(dep, set()).add(target.label)
            # A dependency on a package target should make that package's
            # targets affect the consumer even if target names differ.
            dep_pkg = dep.split(":", 1)[0]
            reverse.setdefault(dep_pkg, set()).add(target.label)
    return targets, reverse


def changed_packages(changed: Iterable[str]) -> set[str]:
    packages: set[str] = set()
    for raw in changed:
        path = (ROOT / raw).resolve()
        try:
            path.relative_to(ROOT)
        except ValueError:
            continue
        package = package_for(path)
        if package:
            packages.add(package_label(package))
    return packages


def select(changed: list[str]) -> list[str]:
    if not changed or any(
        path == prefix or path.startswith(prefix) for path in changed for prefix in GLOBAL_PREFIXES
    ):
        return ["//..."]
    targets, reverse = graph()
    packages = changed_packages(changed)
    seeds = {label for label, target in targets.items() if target.package in packages}
    queue = list(seeds | packages)
    affected = set(seeds)
    seen = set(queue)
    while queue:
        current = queue.pop()
        for consumer in reverse.get(current, ()):
            if consumer not in seen:
                seen.add(consumer)
                queue.append(consumer)
                affected.add(consumer)
            pkg = consumer.split(":", 1)[0]
            if pkg not in seen:
                seen.add(pkg)
                queue.append(pkg)
    tests = sorted(label for label in affected if label in targets and targets[label].is_test)
    # If a package has no explicit parsed test targets, preserving its package
    # pattern is safer than incorrectly claiming no tests are affected.
    covered = {targets[label].package for label in tests if label in targets}
    tests.extend(sorted(f"{pkg}/..." for pkg in packages if pkg not in covered and pkg != "//"))
    return sorted(set(tests)) or ["//..."]


def git_changed(base: str | None) -> list[str]:
    if base:
        command = ["git", "diff", "--name-only", f"{base}...HEAD"]
    else:
        command = ["git", "diff", "--name-only", "HEAD^", "HEAD"]
    result = subprocess.run(command, cwd=ROOT, text=True, capture_output=True, check=False)
    return (
        [line.strip() for line in result.stdout.splitlines() if line.strip()]
        if result.returncode == 0
        else []
    )


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("paths", nargs="*")
    parser.add_argument("--base")
    parser.add_argument("--format", choices=("lines", "json"), default="lines")
    args = parser.parse_args()
    changed = args.paths or git_changed(args.base)
    targets = select(changed)
    if args.format == "json":
        print(json.dumps({"changed": changed, "targets": targets}, indent=2))
    else:
        print("\n".join(targets))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
