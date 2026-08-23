# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Proves the drift gate can fail, which is the only property that makes it a gate.

`tests/integration/cross_language/test_error_codes.py` in this repository asserts that two files
exist and nothing else — a check that passes whatever the files contain. A verifier is worth
exactly what it rejects, so most of what follows perturbs a real committed artifact and asserts
a specific finding, rather than asserting that the unperturbed tree passes.

The fixtures are copies of real repository files, not invented ones. A synthetic `_pb.ts` would
prove the checker agrees with the author's idea of protoc-gen-es output; a copy of the committed
file proves it agrees with the generator.
"""

from __future__ import annotations

import importlib.util
import shutil
import sys
from pathlib import Path

import pytest

ROOT = Path(__file__).resolve().parents[3]


def _load(name: str, path: Path):
    """Import a repository tool by path, the way `tools/analysis/tests` does.

    Not `from tools.codegen import ...`: `tools/` is not a package, and making it one to satisfy
    an import would change how every checker in the tree resolves.
    """
    spec = importlib.util.spec_from_file_location(name, path)
    if spec is None or spec.loader is None:  # pragma: no cover - a drifting ROOT, not a code path
        raise RuntimeError(f"unable to load {path}")
    module = importlib.util.module_from_spec(spec)
    sys.modules[name] = module
    spec.loader.exec_module(module)
    return module


verify_generated = _load("verify_generated", ROOT / "tools/codegen/verify_generated.py")

# One protobuf contract, small enough to reason about and exercising the common shapes: a
# package, two messages, scalar fields, and snake_case names that become lowerCamelCase.
SAMPLE_PROTO = "mindclade/common/v1/pagination.proto"

# A second contract, chosen for the one property the first one lacks: SINGLE-WORD field names
# (`reserved`, `backend`). See test_renaming_a_single_word_field_is_rejected — the first draft of
# the member check passed for every single-word field in the tree, and a fixture whose fields are
# all multi-word could not have caught it. It is also self-contained (no imports) and carries two
# top-level enums, so it covers the enum path as well.
SINGLE_WORD_PROTO = "mindclade/training/v1/topology.proto"

# The files a fake repository root needs before the static lane will run against it, beyond the
# contract under test.
SHARED_FIXTURE_FILES = (
    ".gitattributes",
    "package.json",
    verify_generated.BUF_TEMPLATE,
    verify_generated.OPENAPI_SOURCE,
    verify_generated.TS_OPENAPI_FILE,
)


def _binding_relpath(proto_relpath: str) -> str:
    return f"{verify_generated.TS_PROTO_ROOT}/{proto_relpath[: -len('.proto')]}_pb.ts"


SAMPLE_BINDING = _binding_relpath(SAMPLE_PROTO)


def _make_root(tmp_path: Path, proto_relpath: str) -> Path:
    """A repository root holding one contract and its generated artifacts, copied from the tree.

    Copied rather than invented. A synthetic `_pb.ts` would prove the checker agrees with the
    author's idea of protoc-gen-es output; a copy proves it agrees with the generator.
    """
    for relpath in (
        *SHARED_FIXTURE_FILES,
        f"{verify_generated.PROTO_ROOT}/{proto_relpath}",
        _binding_relpath(proto_relpath),
    ):
        destination = tmp_path / relpath
        destination.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(ROOT / relpath, destination)
    return tmp_path


@pytest.fixture
def fake_root(tmp_path: Path) -> Path:
    return _make_root(tmp_path, SAMPLE_PROTO)


def _binding(root: Path) -> Path:
    return root / SAMPLE_BINDING


def test_the_fixture_root_passes_before_it_is_perturbed(fake_root: Path) -> None:
    """The baseline. Every negative case below is only meaningful against a clean start."""
    assert verify_generated.check(fake_root) == []


def test_renaming_a_field_in_the_typescript_is_rejected(fake_root: Path) -> None:
    """The hand-edit the generated tree is most exposed to: `linguist-generated` collapses it."""
    path = _binding(fake_root)
    path.write_text(
        path.read_text(encoding="utf-8").replace("pageSize: number", "pageCount: number"),
        encoding="utf-8",
    )
    errors = verify_generated.check(fake_root)
    assert any("has no 'pageSize' member" in error for error in errors), errors


def test_renaming_a_single_word_field_is_rejected(tmp_path: Path) -> None:
    """The regression for a fail-open this checker shipped with in its first draft.

    The member check searched the declaration block for the camelCase name with `\b` word
    boundaries and no anchor. Directly above every member sits its own annotation —
    `* @generated from field: mindclade.training.v1.CollectiveBackend backend = 7;` — which
    contains that name whenever the proto field is a single word. The comment satisfied the
    check, so the TypeScript underneath was never read, and renaming `backend:` to anything at
    all passed. 228 of this tree's 564 fields are single-word, so roughly 40% of the surface the
    gate claimed to cover was unverified.

    `pagination.proto` could not catch it: `page_size`, `page_token` and `next_page_token` are
    all multi-word, so their annotations spell `page_size` while the member spells `pageSize`.
    This case exists to keep a single-word field in the suite permanently.
    """
    root = _make_root(tmp_path, SINGLE_WORD_PROTO)
    assert verify_generated.check(root) == []

    binding = root / _binding_relpath(SINGLE_WORD_PROTO)
    text = binding.read_text(encoding="utf-8")
    assert "\n  backend: CollectiveBackend;\n" in text, "fixture no longer has a single-word field"
    binding.write_text(
        text.replace(
            "\n  backend: CollectiveBackend;\n", "\n  backendRENAMED: CollectiveBackend;\n"
        ),
        encoding="utf-8",
    )
    errors = verify_generated.check(root)
    assert any("has no 'backend' member" in error for error in errors), errors


def test_renaming_a_field_and_its_annotation_together_is_rejected(fake_root: Path) -> None:
    """The annotation is prose; the embedded descriptor is not.

    Editing the comment and the property in step defeats any checker that only compares the two
    to each other. The descriptor is a compiled artifact of the `.proto`, so it still disagrees.
    """
    path = _binding(fake_root)
    text = path.read_text(encoding="utf-8")
    text = text.replace("int32 page_size = 1;", "int32 page_count = 1;")
    text = text.replace("pageSize: number", "pageCount: number")
    path.write_text(text, encoding="utf-8")
    errors = verify_generated.check(fake_root)
    assert any("embedded descriptor" in error and "page_size" in error for error in errors), errors


def test_editing_the_proto_without_regenerating_is_rejected(fake_root: Path) -> None:
    """The other direction of drift, and the one that makes `protocols/` the authority."""
    source = fake_root / verify_generated.PROTO_ROOT / SAMPLE_PROTO
    source.write_text(
        source.read_text(encoding="utf-8").replace(
            "string page_token = 2;", "string page_token = 2;\n  int32 page_offset = 3;"
        ),
        encoding="utf-8",
    )
    errors = verify_generated.check(fake_root)
    assert any("page_offset" in error for error in errors), errors


def test_deleting_a_generated_binding_is_rejected(fake_root: Path) -> None:
    _binding(fake_root).unlink()
    errors = verify_generated.check(fake_root)
    assert any("has no generated" in error for error in errors), errors


def test_an_orphan_binding_without_a_proto_is_rejected(fake_root: Path) -> None:
    """A renamed or deleted contract leaves output behind; `clean: true` removes it, review does not."""
    orphan = _binding(fake_root).with_name("removed_contract_pb.ts")
    shutil.copyfile(_binding(fake_root), orphan)
    errors = verify_generated.check(fake_root)
    assert any("has no source under" in error for error in errors), errors


def test_a_stale_plugin_version_stamp_is_rejected(fake_root: Path) -> None:
    """A protoc-gen-es bump without regenerated output is drift, not a version string."""
    path = _binding(fake_root)
    path.write_text(
        path.read_text(encoding="utf-8").replace("protoc-gen-es v", "protoc-gen-es v0.0.1-"),
        encoding="utf-8",
    )
    errors = verify_generated.check(fake_root)
    assert any("package.json pins" in error for error in errors), errors


def test_a_file_that_is_not_generator_output_at_all_is_rejected(fake_root: Path) -> None:
    _binding(fake_root).write_text("export const hand = 1;\n", encoding="utf-8")
    errors = verify_generated.check(fake_root)
    assert any("not produced by the generator" in error for error in errors), errors


def test_a_committed_build_time_binding_is_rejected(fake_root: Path) -> None:
    """`.gitattributes` declares `protocols/**/*.pb.go` generated; the tree must stay empty."""
    committed = fake_root / verify_generated.PROTO_ROOT / "mindclade/common/v1/pagination.pb.go"
    committed.write_text("package commonv1\n", encoding="utf-8")
    errors = verify_generated.check(fake_root)
    assert any("pagination.pb.go is committed" in error for error in errors), errors


def test_an_undeclared_generated_rule_is_rejected(fake_root: Path) -> None:
    """Coverage is gated too: a new generated tree may not arrive with no disposition."""
    attributes = fake_root / ".gitattributes"
    attributes.write_text(
        attributes.read_text(encoding="utf-8")
        + "\nsdk/python/generated/** linguist-generated=true\n",
        encoding="utf-8",
    )
    errors = verify_generated.check(fake_root)
    assert any("has no disposition for it" in error for error in errors), errors


def test_dropping_a_generated_declaration_is_rejected(fake_root: Path) -> None:
    """The inverse: an artifact that stops being declared generated stops being accounted for."""
    attributes = fake_root / ".gitattributes"
    kept = [
        line
        for line in attributes.read_text(encoding="utf-8").splitlines()
        if not line.startswith("protocols/**/*.pb.go")
    ]
    attributes.write_text("\n".join(kept) + "\n", encoding="utf-8")
    errors = verify_generated.check(fake_root)
    assert any("no longer marks it" in error for error in errors), errors


def test_an_openapi_path_missing_from_the_client_is_rejected(fake_root: Path) -> None:
    client = fake_root / verify_generated.TS_OPENAPI_FILE
    client.write_text(
        client.read_text(encoding="utf-8").replace('    "/v1/runs": {', '    "/v1/jobs": {', 1),
        encoding="utf-8",
    )
    errors = verify_generated.check(fake_root)
    assert any("/v1/runs" in error for error in errors), errors


def test_a_missing_witness_is_a_failure_and_never_a_skip(tmp_path: Path) -> None:
    """An empty tree must not read as a clean tree.

    This is the shape of the anti-pattern the gate was written against: a checker that cannot
    find what it verifies and returns success anyway.
    """
    errors = verify_generated.check(tmp_path)
    assert errors, "verification of an empty tree reported no findings"


# ---------------------------------------------------------------------------------------
# The parsers, whose bugs would show up as either false failures or silent gaps
# ---------------------------------------------------------------------------------------

_SAMPLE_SOURCE = """
syntax = "proto3";
package example.v1;
option go_package = "example/v1;examplev1";
import "other.proto";

enum TopLevel {
  TOP_LEVEL_UNSPECIFIED = 0;
  TOP_LEVEL_ONE = 1;
}

message Outer {
  option deprecated = true;
  reserved 9, 10;
  string name = 1;
  repeated Inner inner = 2;
  map<string, int32> counts = 3;
  optional uint64 seed = 4 [deprecated = true];
  oneof choice {
    string text = 5;
    Inner nested = 6;
  }
  message Inner { string value = 1; }
  enum Mode { MODE_UNSPECIFIED = 0; }
}

message Compact { uint64 sequence=1; string label=2; }

service Thing {
  rpc Do(Outer) returns (Compact);
  rpc Stream(Outer) returns (stream Compact) { option deprecated = true; }
}
"""


def test_proto_parser_reads_every_construct_the_repository_uses() -> None:
    surface = verify_generated.parse_proto_source(_SAMPLE_SOURCE, "example.proto")
    assert surface.package == "example.v1"
    assert surface.syntax == "proto3"
    assert set(surface.messages) == {"Outer", "Outer.Inner", "Compact"}
    assert verify_generated._render_members(surface.messages["Outer"]) == [
        "counts=3",
        "inner=2",
        "name=1",
        "nested=6",
        "seed=4",
        "text=5",
    ]
    assert verify_generated._render_members(surface.messages["Compact"]) == [
        "label=2",
        "sequence=1",
    ]
    assert set(surface.enums) == {"TopLevel", "Outer.Mode"}
    assert surface.services == {"Thing": ("Do", "Stream")}


def test_proto_parser_fails_closed_on_a_construct_it_does_not_know() -> None:
    """Skipping the unknown is how a parser quietly stops verifying part of the contract."""
    with pytest.raises(verify_generated.VerificationError, match="unsupported top-level"):
        verify_generated.parse_proto_source('syntax = "proto3";\nnonsense Foo {}\n', "bad.proto")


def test_a_field_named_reserved_is_a_field_and_not_a_statement() -> None:
    """`string reserved = 1;` appears in six contracts here; `reserved 1;` is a different thing."""
    surface = verify_generated.parse_proto_source(
        'syntax = "proto3";\nmessage M { string reserved = 1; }\n', "m.proto"
    )
    assert verify_generated._render_members(surface.messages["M"]) == ["reserved=1"]


def test_descriptor_decoding_agrees_with_the_committed_source() -> None:
    """The wire decoder, checked against the real artifact rather than a hand-built message."""
    binding = verify_generated.read_typescript_binding(
        (ROOT / SAMPLE_BINDING).read_text(encoding="utf-8"), SAMPLE_BINDING
    )
    assert binding.descriptor_source_file == SAMPLE_PROTO
    assert binding.descriptor_surface.package == "mindclade.common.v1"
    assert binding.descriptor_surface.syntax == "proto3"
    assert verify_generated._render_members(binding.descriptor_surface.messages["PageRequest"]) == [
        "page_size=1",
        "page_token=2",
    ]


def test_a_truncated_descriptor_is_an_error_rather_than_an_empty_surface() -> None:
    with pytest.raises(verify_generated.VerificationError):
        verify_generated.decode_file_descriptor(b"\x0a\xff")


@pytest.mark.parametrize(
    ("proto_name", "expected"),
    [("page_size", "pageSize"), ("seed", "seed"), ("next_page_token", "nextPageToken")],
)
def test_json_name_conversion(proto_name: str, expected: str) -> None:
    assert verify_generated._lower_camel(proto_name) == expected


def test_nested_declarations_flatten_the_way_protoc_gen_es_flattens_them() -> None:
    assert verify_generated._ts_identifier("BufferDescriptor.AccessMode") == (
        "BufferDescriptor_AccessMode"
    )
    assert verify_generated._upper_snake("AccessMode") == "ACCESS_MODE"


def test_tree_comparison_names_added_removed_and_changed_files() -> None:
    findings = verify_generated.compare_trees(
        {"a.ts": b"old\n", "gone.ts": b"x\n"}, {"a.ts": b"new\n", "extra.ts": b"y\n"}
    )
    rendered = "\n".join(findings)
    assert "extra.ts: generated but not committed" in rendered
    assert "gone.ts: committed but no generator produces it" in rendered
    assert "-old" in rendered and "+new" in rendered
