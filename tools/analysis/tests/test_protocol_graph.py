# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Each test breaks exactly one edge of the protobuf graph and names the message it expects.

The fixture below is a complete, correctly wired miniature of `protocols/`: two `.proto` files,
one importing the other and a well-known type, their `proto_library` rules, the package's
`go_proto_library`, and the two aggregate lists. Every test mutates one thing about it, so a
failure identifies the edge rather than the fixture.

`test_the_real_repository_graph_is_complete` is the one that matters most: a checker that only
ever runs against a fixture proves that the fixture is consistent with itself.
"""

from __future__ import annotations

import importlib.util
import sys
from pathlib import Path

import pytest

ROOT = Path(__file__).resolve().parents[3]


def _load(name: str, path: Path):
    spec = importlib.util.spec_from_file_location(name, path)
    if spec is None or spec.loader is None:
        raise RuntimeError(f"unable to load {path}")
    module = importlib.util.module_from_spec(spec)
    sys.modules[name] = module
    spec.loader.exec_module(module)
    return module


graph = _load("check_protocol_graph", ROOT / "tools/analysis/check_protocol_graph.py")

PACKAGE = "protocols/proto/example/v1"

ERRORS_PROTO = """syntax = "proto3";
package example.v1;

message Error {
  string code = 1;
}
"""

RECORD_PROTO = """syntax = "proto3";
package example.v1;

import "example/v1/errors.proto";
import "google/protobuf/timestamp.proto";

message Record {
  Error error = 1;
  google.protobuf.Timestamp created_at = 2;
}
"""

PACKAGE_BUILD = """load("@protobuf//bazel:proto_library.bzl", "proto_library")
load("@rules_go//proto:def.bzl", "go_proto_library")

proto_library(
    name = "errors_proto",
    srcs = ["errors.proto"],
    strip_import_prefix = "/protocols/proto",
)

proto_library(
    name = "record_proto",
    srcs = ["record.proto"],
    strip_import_prefix = "/protocols/proto",
    deps = [
        ":errors_proto",
        "@protobuf//:timestamp_proto",
    ],
)

go_proto_library(
    name = "example_go_proto",
    importpath = "go.mindclade.dev/protocols/gen/go/example/v1",
    protos = [
        ":errors_proto",
        ":record_proto",
    ],
)
"""

AGGREGATE_BUILD = """proto_library(
    name = "all_protos",
    deps = [
        "//protocols/proto/example/v1:errors_proto",
        "//protocols/proto/example/v1:record_proto",
    ],
)

filegroup(
    name = "all_proto_sources",
    srcs = [
        "//protocols/proto/example/v1:errors.proto",
        "//protocols/proto/example/v1:record.proto",
    ],
)
"""


@pytest.fixture
def tree(tmp_path: Path) -> Path:
    """A minimal repository whose protobuf graph is completely and correctly wired."""
    package = tmp_path / PACKAGE
    package.mkdir(parents=True)
    (package / "errors.proto").write_text(ERRORS_PROTO, encoding="utf-8")
    (package / "record.proto").write_text(RECORD_PROTO, encoding="utf-8")
    (package / "BUILD.bazel").write_text(PACKAGE_BUILD, encoding="utf-8")
    (tmp_path / "protocols/BUILD.bazel").write_text(AGGREGATE_BUILD, encoding="utf-8")
    return tmp_path


def _rewrite(path: Path, old: str, new: str) -> None:
    text = path.read_text(encoding="utf-8")
    assert old in text, f"fixture no longer contains {old!r}"
    path.write_text(text.replace(old, new, 1), encoding="utf-8")


def test_a_correctly_wired_graph_reports_nothing(tree: Path) -> None:
    assert graph.check(tree) == []


def test_the_real_repository_graph_is_complete() -> None:
    """The checker and `protocols/` agree today.

    Without this the suite would prove only that the fixture matches the parser. It is also the
    assertion that makes the gate actionable: it holds now, so any future failure is the change
    under review rather than a pre-existing gap.
    """
    assert graph.check(ROOT) == []


def test_a_proto_with_no_proto_library_is_reported(tree: Path) -> None:
    """The defect the file exists for: a source file that never enters the graph."""
    (tree / PACKAGE / "orphan.proto").write_text(
        'syntax = "proto3";\npackage example.v1;\n', encoding="utf-8"
    )

    errors = graph.check(tree)

    assert any("orphan.proto has no proto_library" in error for error in errors), errors


def test_an_orphan_proto_is_still_reported_when_every_other_edge_is_intact(tree: Path) -> None:
    """The aggregates are derived from the tree, not from the declarations.

    A tree-derived expectation is what makes this catch the omission. Deriving the aggregate
    from the `proto_library` rules instead would make the lists agree with each other and with
    nothing else, which is the same fail-open shape as the tests that read `all_proto_sources`.
    """
    (tree / PACKAGE / "orphan.proto").write_text(
        'syntax = "proto3";\npackage example.v1;\n', encoding="utf-8"
    )

    errors = graph.check(tree)

    assert any("all_proto_sources omits" in error and "orphan.proto" in error for error in errors)


def test_a_missing_dependency_edge_is_reported(tree: Path) -> None:
    _rewrite(tree / PACKAGE / "BUILD.bazel", '        ":errors_proto",\n', "")

    errors = graph.check(tree)

    assert any(
        "record_proto: protocols/proto/example/v1/record.proto imports the source behind "
        "//protocols/proto/example/v1:errors_proto but the rule does not depend on it" in error
        for error in errors
    ), errors


def test_a_dependency_the_source_does_not_import_is_reported(tree: Path) -> None:
    """Stale edges matter as much as missing ones: they hide a removed import."""
    _rewrite(
        tree / PACKAGE / "BUILD.bazel",
        '    strip_import_prefix = "/protocols/proto",\n)\n\nproto_library(\n    name = "record_proto"',
        '    strip_import_prefix = "/protocols/proto",\n    deps = ["@protobuf//:duration_proto"],\n)'
        '\n\nproto_library(\n    name = "record_proto"',
    )

    errors = graph.check(tree)

    assert any(
        "errors_proto: depends on @protobuf//:duration_proto, which "
        "protocols/proto/example/v1/errors.proto does not import" in error
        for error in errors
    ), errors


def test_a_well_known_import_resolves_to_the_protobuf_module_target(tree: Path) -> None:
    """`google/protobuf/*` is a real dependency edge, not something to skip past."""
    _rewrite(tree / PACKAGE / "BUILD.bazel", '        "@protobuf//:timestamp_proto",\n', "")

    errors = graph.check(tree)

    assert any(
        "@protobuf//:timestamp_proto but the rule does not depend on it" in e for e in errors
    )


def test_a_proto_left_out_of_the_go_bindings_is_reported(tree: Path) -> None:
    _rewrite(tree / PACKAGE / "BUILD.bazel", '        ":record_proto",\n    ],\n)\n', "    ],\n)\n")

    errors = graph.check(tree)

    assert any(
        "example_go_proto: does not generate Go bindings for "
        "//protocols/proto/example/v1:record_proto" in error
        for error in errors
    ), errors


def test_a_package_without_a_go_proto_library_is_reported(tree: Path) -> None:
    """Go bindings are a build output, so their absence has no other symptom."""
    build = tree / PACKAGE / "BUILD.bazel"
    build.write_text(build.read_text(encoding="utf-8").split("go_proto_library(")[0], "utf-8")

    errors = graph.check(tree)

    assert any("declares 0 go_proto_library targets" in error for error in errors), errors


def test_a_proto_missing_from_all_protos_is_reported(tree: Path) -> None:
    _rewrite(
        tree / "protocols/BUILD.bazel",
        '        "//protocols/proto/example/v1:record_proto",\n',
        "",
    )

    errors = graph.check(tree)

    assert any(
        "all_protos omits //protocols/proto/example/v1:record_proto" in error for error in errors
    ), errors


def test_a_stale_aggregate_entry_is_reported(tree: Path) -> None:
    _rewrite(
        tree / "protocols/BUILD.bazel",
        '"//protocols/proto/example/v1:record.proto",',
        '"//protocols/proto/example/v1:removed.proto",',
    )

    errors = graph.check(tree)

    assert any("no longer exists under protocols/proto" in error for error in errors), errors


def test_two_rules_over_one_source_are_reported(tree: Path) -> None:
    build = tree / PACKAGE / "BUILD.bazel"
    build.write_text(
        build.read_text(encoding="utf-8")
        + '\nproto_library(\n    name = "errors_duplicate_proto",\n'
        '    srcs = ["errors.proto"],\n    strip_import_prefix = "/protocols/proto",\n)\n',
        encoding="utf-8",
    )

    errors = graph.check(tree)

    assert any("is claimed by both" in error for error in errors), errors


def test_a_wrong_strip_import_prefix_is_reported(tree: Path) -> None:
    _rewrite(
        tree / PACKAGE / "BUILD.bazel",
        'name = "errors_proto",\n    srcs = ["errors.proto"],\n'
        '    strip_import_prefix = "/protocols/proto",',
        'name = "errors_proto",\n    srcs = ["errors.proto"],',
    )

    errors = graph.check(tree)

    assert any("strip_import_prefix must be" in error for error in errors), errors


def test_a_globbed_srcs_attribute_is_reported_rather_than_guessed(tree: Path) -> None:
    """`glob()` is not a literal, and a parser that quietly treats it as empty fails open."""
    _rewrite(tree / PACKAGE / "BUILD.bazel", 'srcs = ["errors.proto"]', 'srcs = glob(["*.proto"])')

    errors = graph.check(tree)

    assert any("srcs is missing or is not a literal list" in error for error in errors), errors
    assert any("errors.proto has no proto_library" in error for error in errors), errors


def test_an_unparseable_build_file_fails_rather_than_passing(tree: Path) -> None:
    (tree / PACKAGE / "BUILD.bazel").write_text("proto_library(name = \n", encoding="utf-8")

    errors = graph.check(tree)

    assert any("could not be parsed" in error for error in errors), errors


def test_a_missing_proto_root_fails_rather_than_passing(tmp_path: Path) -> None:
    """An empty answer from a checker that could not look is the failure mode to avoid."""
    errors = graph.check(tmp_path)

    assert any("there is no protocol graph" in error for error in errors), errors


def test_an_unresolvable_import_is_reported(tree: Path) -> None:
    _rewrite(
        tree / PACKAGE / "record.proto", 'import "example/v1/errors.proto";', 'import "gone.proto";'
    )

    errors = graph.check(tree)

    assert any("imports 'gone.proto', which no proto_library" in error for error in errors), errors


def test_a_non_literal_deps_attribute_is_reported_rather_than_read_as_empty(tree: Path) -> None:
    """An unreadable `deps` must not be mistaken for a leaf rule that legitimately has none.

    The distinction is by key presence, not by value: reading a `deps` this parser cannot
    evaluate as an empty set would compare every import against nothing and report a missing
    edge for each -- or, worse in a future refactor, report none at all.
    """
    _rewrite(
        tree / PACKAGE / "BUILD.bazel",
        '    deps = [\n        ":errors_proto",\n        "@protobuf//:timestamp_proto",\n    ],',
        "    deps = RECORD_DEPS,",
    )

    errors = graph.check(tree)

    assert any("deps is missing or is not a literal list" in error for error in errors), errors
