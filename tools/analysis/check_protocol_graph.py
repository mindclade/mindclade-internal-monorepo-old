#!/usr/bin/env python3
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Every promoted `.proto` reaches the Bazel generation graph, or this fails.

ADR-0014 makes Bazel the authority for "the declared schema compilation, language-generation
graph, compatibility tests, and provenance", and `protocols/README.md` says all promoted
packages compile through `//protocols:all_protos`. Both were statements of intent that nothing
checked, and the graph they describe is assembled from four hand-maintained lists:

    proto_library(srcs=)            one per `.proto`
    proto_library(deps=)            the file's `import` statements, spelled as labels
    go_proto_library(protos=)       every `proto_library` in the package
    //protocols:all_protos(deps=)   every `proto_library` in the tree
    //protocols:all_proto_sources   every `.proto` in the tree

A `.proto` added without those five edges is not a build error. Bazel does not mind an
unreferenced source file, so the omission is silent — and every gate that would notice reads
one of the lists rather than the tree:

    `buf lint` / `buf breaking`          read the `protocols/proto` DIRECTORY, so they pass
    `//protocols:protobuf_governance_test`  builds its descriptor set from `all_proto_sources`,
                                         so the new message is absent from both the live
                                         surface and the baseline it is compared against
    `//protocols:typescript_projection_test` takes `all_proto_sources` as its input, so it
                                         proves a projection exists for every file it was told
                                         about
    `//protocols:protobuf_contract_image`   packages `all_proto_sources`, so the released
                                         contract bundle silently omits the file

`verify_generated.py` does walk the real tree, but only into TypeScript: `buf generate` reads
the directory too, so the new file DOES get a `_pb.ts` and the bijection is satisfied. The net
effect is the worst available shape — a contract that reaches `main` with a checked-in
TypeScript projection, no Go or Python bindings, no descriptor-set entry, no compatibility
baseline, and a green board.

So this walks the tree and re-derives the five edges from the `.proto` sources, which is the
one direction none of the existing gates take.

WHY A CHECKER AND NOT A GENERATOR. The derivation is exact enough to emit these lists, and
`tools/codegen/generate_build_files.py` reserved a path to do it. BUILD-metadata generation
authority is already assigned: `//:gazelle` owns Go BUILD metadata with `//:gazelle_check`
failing on diff, and `ci/bazel/README.md` requires any expansion of that authority to be
"pinned in MODULE.bazel, locked for every supported platform, included in downloader/mirror
qualification, and proven against the existing Python graph before its output becomes
authoritative". A repository-local emitter would be exactly the unreviewed second authority
that text refuses. A checker takes no authority: it cannot rewrite a reviewed BUILD file, and
its failure names the edge to add.

Hermetic and standard-library only, because `run_architecture_checks.py` calls `check()` in
the `--static-only` lane. BUILD files are parsed with `ast`: the rule calls here are a syntactic
subset of Python, and a parse failure is reported rather than skipped.

    python3 tools/analysis/check_protocol_graph.py
"""

from __future__ import annotations

import argparse
import ast
from dataclasses import dataclass
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]

PROTO_ROOT = "protocols/proto"
AGGREGATE_BUILD = "protocols/BUILD.bazel"
ALL_PROTOS = "all_protos"
ALL_PROTO_SOURCES = "all_proto_sources"

# `strip_import_prefix` is what makes an `import "mindclade/common/v1/errors.proto";` resolve
# against the repository-relative proto root rather than the Bazel package. A rule that omits it
# still builds, and then imports of it resolve under a different prefix in Bazel than they do
# under buf — two spellings of the same contract, which is the failure ADR-0014 exists to stop.
STRIP_IMPORT_PREFIX = "/protocols/proto"

# Well-known types are not in this repository; protobuf's Bazel module publishes one target per
# file under a fixed name. Mapping them explicitly keeps an unresolvable import an error rather
# than something the deps comparison quietly tolerates.
WELL_KNOWN_REPOSITORY = "@protobuf"


class ProtocolGraphError(Exception):
    """An input could not be read or parsed, which is a failure and never a skip."""


@dataclass(frozen=True)
class Rule:
    """One Bazel rule call, reduced to the attributes this check reasons about."""

    kind: str
    package: str
    name: str
    attributes: dict[str, object]

    @property
    def label(self) -> str:
        return f"//{self.package}:{self.name}"


def _literal(node: ast.expr) -> object | None:
    """The Python value of a literal attribute, or None when it is not one.

    `glob([...])`, `select({...})` and concatenations are calls and comprehensions rather than
    literals. They are returned as None so the caller reports "not a literal list" instead of
    comparing against a value this parser invented.
    """
    try:
        value: object = ast.literal_eval(node)
    except (ValueError, TypeError, SyntaxError, MemoryError, RecursionError):
        return None
    return value


def _parse_build_file(path: Path, package: str) -> list[Rule]:
    """Every top-level rule call in one BUILD file.

    Only top-level calls: a rule produced inside a macro or a comprehension is not something
    this parser can attribute to a name, and pretending otherwise would let a package hide a
    declaration from the comparison below.
    """
    try:
        text = path.read_text(encoding="utf-8")
    except OSError as error:
        raise ProtocolGraphError(f"{package}/BUILD.bazel could not be read: {error}") from error
    try:
        tree = ast.parse(text, filename=str(path))
    except SyntaxError as error:
        raise ProtocolGraphError(f"{package}/BUILD.bazel could not be parsed: {error}") from error

    rules: list[Rule] = []
    for statement in tree.body:
        if not isinstance(statement, ast.Expr) or not isinstance(statement.value, ast.Call):
            continue
        call = statement.value
        if not isinstance(call.func, ast.Name):
            continue
        attributes: dict[str, object] = {}
        for keyword in call.keywords:
            if keyword.arg is None:  # `**kwargs` is not something a BUILD file uses here.
                continue
            attributes[keyword.arg] = _literal(keyword.value)
        name = attributes.get("name")
        if not isinstance(name, str):
            continue
        rules.append(Rule(kind=call.func.id, package=package, name=name, attributes=attributes))
    return rules


def _proto_packages(root: Path) -> list[str]:
    """Repository-relative directories under `protocols/proto` that contain `.proto` files."""
    base = root / PROTO_ROOT
    if not base.is_dir():
        raise ProtocolGraphError(f"{PROTO_ROOT} is missing; there is no protocol graph to check")
    packages = {
        path.parent.relative_to(root).as_posix() for path in base.rglob("*.proto") if path.is_file()
    }
    return sorted(packages)


def _string_list(rule: Rule, attribute: str) -> tuple[list[str], str | None]:
    """A rule's list-of-strings attribute, or a message saying why it could not be read."""
    value = rule.attributes.get(attribute)
    if value is None:
        return [], f"{rule.label}: {attribute} is missing or is not a literal list of strings"
    if not isinstance(value, list) or not all(isinstance(item, str) for item in value):
        return [], f"{rule.label}: {attribute} is not a literal list of strings"
    return list(value), None


def _absolute(package: str, label: str) -> str:
    """`:name` in `package` spelled as `//package:name`; anything else is already absolute."""
    return f"//{package}{label}" if label.startswith(":") else label


def _proto_imports(path: Path) -> list[str]:
    """The `import` targets of one `.proto`, in declaration order.

    Deliberately a line scan rather than a full parser. `import` is one of the few proto
    statements that must start a line and end with `";`, and the alternative — a hand-written
    proto grammar in a static checker — is a much larger surface to be wrong in. `import public`
    and `import weak` are matched too: both create a Bazel dependency edge.
    """
    imports: list[str] = []
    for raw in path.read_text(encoding="utf-8").splitlines():
        line = raw.strip()
        if not line.startswith("import"):
            continue
        remainder = line[len("import") :].lstrip()
        for modifier in ("public ", "weak "):
            if remainder.startswith(modifier):
                remainder = remainder[len(modifier) :].lstrip()
        if not remainder.startswith('"'):
            continue
        closing = remainder.find('"', 1)
        if closing == -1:
            continue
        imports.append(remainder[1:closing])
    return imports


def _well_known_label(target: str) -> str:
    """`google/protobuf/timestamp.proto` as the label protobuf's Bazel module publishes."""
    stem = target.rsplit("/", 1)[-1].removesuffix(".proto")
    return f"{WELL_KNOWN_REPOSITORY}//:{stem}_proto"


def _derive(root: Path) -> list[str]:
    """Errors describing every `.proto` that is not fully wired into the Bazel graph.

    Five passes, in dependency order: each needs the index the one before it builds, and each
    reports its own edge independently so a partly-wired file names every step it still needs
    rather than only the first.
    """
    packages = _proto_packages(root)
    base = root / PROTO_ROOT

    # Pass one: the per-package declarations, and the source -> label index the rest needs.
    owner: dict[str, str] = {}  # proto path relative to PROTO_ROOT -> proto_library label
    package_libraries: dict[str, list[str]] = {}
    rules_by_package: dict[str, list[Rule]] = {}
    errors: list[str] = []

    for package in packages:
        rules = _parse_build_file(root / package / "BUILD.bazel", package)
        rules_by_package[package] = rules
        libraries = [rule for rule in rules if rule.kind == "proto_library"]
        package_libraries[package] = [rule.label for rule in libraries]

        for rule in libraries:
            srcs, failure = _string_list(rule, "srcs")
            if failure:
                errors.append(failure)
                continue
            if len(srcs) != 1:
                errors.append(
                    f"{rule.label}: declares {len(srcs)} srcs; one proto_library per `.proto` "
                    f"keeps the dependency edges below derivable from the import graph"
                )
            for src in srcs:
                relative = f"{package[len(PROTO_ROOT) + 1 :]}/{src}"
                if not (base / relative).is_file():
                    errors.append(
                        f"{rule.label}: srcs names {src}, which does not exist in {package}"
                    )
                    continue
                if relative in owner:
                    errors.append(
                        f"{PROTO_ROOT}/{relative} is claimed by both {owner[relative]} and "
                        f"{rule.label}; two proto_library targets over one source compile it "
                        f"twice and register its descriptor twice"
                    )
                    continue
                owner[relative] = rule.label
            if rule.attributes.get("strip_import_prefix") != STRIP_IMPORT_PREFIX:
                errors.append(
                    f"{rule.label}: strip_import_prefix must be {STRIP_IMPORT_PREFIX!r} so an "
                    f"`import` resolves to the same file under Bazel as under buf"
                )

    # Pass two: every `.proto` in the tree has an owner. This is the check the whole file exists
    # for; the rest verify that a declared owner is wired correctly.
    for path in sorted(base.rglob("*.proto")):
        relative = path.relative_to(base).as_posix()
        if relative not in owner:
            errors.append(
                f"{PROTO_ROOT}/{relative} has no proto_library in "
                f"{path.parent.relative_to(root).as_posix()}/BUILD.bazel, so it is absent from "
                f"the descriptor set, the compatibility baseline, the contract image, and the "
                f"Go and Python bindings while `buf` still lints and projects it"
            )

    # Pass three: declared deps equal the file's imports. A missing dep builds only by accident
    # -- through another target that happens to pull the same file in -- and stops building on
    # any unrelated change that removes that path.
    for package in packages:
        for rule in (r for r in rules_by_package[package] if r.kind == "proto_library"):
            srcs, failure = _string_list(rule, "srcs")
            if failure or len(srcs) != 1:
                continue  # already reported above
            relative = f"{package[len(PROTO_ROOT) + 1 :]}/{srcs[0]}"
            source = base / relative
            if not source.is_file():
                continue  # already reported above

            expected: set[str] = set()
            for target in _proto_imports(source):
                if target.startswith("google/protobuf/"):
                    expected.add(_well_known_label(target))
                elif target in owner:
                    expected.add(owner[target])
                else:
                    errors.append(
                        f"{PROTO_ROOT}/{relative}: imports {target!r}, which no proto_library "
                        f"under {PROTO_ROOT} declares"
                    )
            # An absent `deps` is legitimate -- a leaf `.proto` imports nothing -- and is a very
            # different statement from a `deps` this parser could not read. Distinguishing them
            # by key presence rather than by value keeps the second from being read as the first,
            # which would silently compare every import against an empty set.
            if "deps" not in rule.attributes:
                declared: set[str] = set()
            else:
                items, failure = _string_list(rule, "deps")
                if failure:
                    errors.append(failure)
                    continue
                declared = {_absolute(package, item) for item in items}
            for label in sorted(expected - declared):
                errors.append(
                    f"{rule.label}: {PROTO_ROOT}/{relative} imports the source behind {label} "
                    f"but the rule does not depend on it"
                )
            for label in sorted(declared - expected):
                errors.append(
                    f"{rule.label}: depends on {label}, which {PROTO_ROOT}/{relative} does not "
                    f"import"
                )

    # Pass four: one go_proto_library per package, covering the whole package. ADR-0014 makes
    # the Go bindings a build output rather than a checked-in tree, so a proto_library left out
    # of this list is a message with no Go type and nothing that reports its absence.
    for package in packages:
        generators = [r for r in rules_by_package[package] if r.kind == "go_proto_library"]
        library_labels = set(package_libraries[package])
        if not library_labels:
            continue  # the package's real defect is already reported by pass two
        if len(generators) != 1:
            errors.append(
                f"//{package}: declares {len(generators)} go_proto_library targets; exactly one "
                f"is required so every message in the package has one Go type"
            )
            continue
        generator = generators[0]
        items, failure = _string_list(generator, "protos")
        if failure:
            errors.append(failure)
            continue
        declared = {_absolute(package, item) for item in items}
        for label in sorted(library_labels - declared):
            errors.append(f"{generator.label}: does not generate Go bindings for {label}")
        for label in sorted(declared - library_labels):
            errors.append(
                f"{generator.label}: names {label}, which is not a proto_library in {package}"
            )

    # Pass five: the two aggregate lists in protocols/BUILD.bazel. These are the inputs the
    # governance test, the TypeScript projection test, and the released contract image are all
    # built from, which is why an omission here is invisible rather than loud.
    aggregate = _parse_build_file(root / AGGREGATE_BUILD, "protocols")
    expected_libraries = {label for labels in package_libraries.values() for label in labels}
    expected_sources = {
        f"//{path.parent.relative_to(root).as_posix()}:{path.name}"
        for path in sorted(base.rglob("*.proto"))
    }

    for name, expected_set, attribute, consequence in (
        (
            ALL_PROTOS,
            expected_libraries,
            "deps",
            "it is not compiled by the descriptor set the compatibility baseline is compared "
            "against",
        ),
        (
            ALL_PROTO_SOURCES,
            expected_sources,
            "srcs",
            "it is not a runfile of the projection and compatibility tests, which then pass "
            "over a tree that does not contain it",
        ),
    ):
        matches = [rule for rule in aggregate if rule.name == name]
        if len(matches) != 1:
            errors.append(
                f"//protocols:{name} is declared {len(matches)} times in {AGGREGATE_BUILD}; "
                f"exactly one declaration is the whole point of an aggregate"
            )
            continue
        items, failure = _string_list(matches[0], attribute)
        if failure:
            errors.append(failure)
            continue
        declared = {_absolute("protocols", item) for item in items}
        for label in sorted(expected_set - declared):
            errors.append(f"//protocols:{name} omits {label}, so {consequence}")
        for label in sorted(declared - expected_set):
            errors.append(
                f"//protocols:{name} names {label}, which no longer exists under {PROTO_ROOT}"
            )

    return sorted(set(errors))


def check(root: Path) -> list[str]:
    """`_derive`, with an unreadable input reported as a failure rather than raised.

    `run_architecture_checks.py` calls this in a loop with no exception handling, so a raise
    here would abort the other checks and report nothing about them. An input this tool cannot
    read is still a failure — it is returned as one, never as an empty list.
    """
    try:
        return _derive(root)
    except (ProtocolGraphError, OSError, ValueError) as error:
        return [f"protocol graph could not be verified: {error}"]


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--repo", type=Path, default=ROOT)
    options = parser.parse_args()
    errors = check(options.repo.resolve())
    for error in errors:
        print(error)
    print("protocol graph check failed" if errors else "protocol graph check passed")
    return 1 if errors else 0


if __name__ == "__main__":
    raise SystemExit(main())
