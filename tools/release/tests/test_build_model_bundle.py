# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#
"""Tests for tools/release/build_model_bundle.py.

The refusal cases carry the weight here. This tool's job is to be the place a pickle cannot
get past, so a test suite that only proved the happy path would be testing the least important
half.
"""

from __future__ import annotations

import json
import pickle
import struct
import sys
from pathlib import Path

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from build_model_bundle import build, collect, validate_safetensors


def write_safetensors(
    path: Path, tensors: dict | None = None, payload: bytes = b"\x00" * 16
) -> Path:
    """Write a minimal valid safetensors file, without importing safetensors."""
    header = (
        tensors
        if tensors is not None
        else {"weight": {"dtype": "F32", "shape": [2, 2], "data_offsets": [0, 16]}}
    )
    blob = json.dumps(header).encode()
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_bytes(struct.pack("<Q", len(blob)) + blob + payload)
    return path


@pytest.fixture
def checkpoint(tmp_path: Path) -> Path:
    d = tmp_path / "checkpoint"
    write_safetensors(d / "model.safetensors")
    (d / "config.json").write_text('{"hidden_size": 4}')
    return d


# ---------------------------------------------------------------------------------------
# The happy path
# ---------------------------------------------------------------------------------------
def test_builds_a_manifest_with_artifactref_fields(checkpoint: Path, tmp_path: Path):
    manifest = build(checkpoint, tmp_path / "out", name="tiny", schema_version=1)

    assert manifest["name"] == "tiny"
    assert manifest["digest"].startswith("sha256:")
    assert manifest["logical_kind"] == "model.bundle"
    assert len(manifest["members"]) == 2

    # Field names must match protocols/proto/mindclade/artifact/v1/artifact.proto. A manifest
    # that renamed them would still validate as JSON and would silently not be an ArtifactRef.
    for member in manifest["members"]:
        assert set(member) >= {
            "digest",
            "size_bytes",
            "media_type",
            "logical_kind",
            "schema_version",
        }
        assert member["digest"].startswith("sha256:")
        assert member["size_bytes"] > 0

    weights = next(m for m in manifest["members"] if m["path"] == "model.safetensors")
    assert weights["logical_kind"] == "model.weights"
    assert weights["media_type"].endswith("+safetensors")


def test_stages_every_file_and_writes_the_manifest(checkpoint: Path, tmp_path: Path):
    out = tmp_path / "out"
    build(checkpoint, out, name="tiny", schema_version=1)
    assert (out / "model.safetensors").is_file()
    assert (out / "config.json").is_file()
    assert json.loads((out / "manifest.json").read_text())["name"] == "tiny"


def test_records_specific_text_media_types(checkpoint: Path, tmp_path: Path):
    (checkpoint / "README.md").write_text("model card\n")
    (checkpoint / "NOTICE.txt").write_text("notice\n")
    manifest = build(checkpoint, tmp_path / "out", name="tiny", schema_version=1)
    media_types = {member["path"]: member["media_type"] for member in manifest["members"]}
    assert media_types["README.md"] == "text/markdown; charset=utf-8"
    assert media_types["NOTICE.txt"] == "text/plain; charset=utf-8"


def test_bundle_digest_is_content_addressed_not_time_addressed(checkpoint: Path, tmp_path: Path):
    """Repacking the same weights must produce the same bundle digest.

    This is the property that makes "promote the same model to production" checkable. A digest
    taken over a tarball would fail this, because tar records mtimes.
    """
    first = build(checkpoint, tmp_path / "a", name="tiny", schema_version=1)
    second = build(checkpoint, tmp_path / "b", name="tiny", schema_version=1)
    assert first["digest"] == second["digest"]


def test_changing_a_weight_changes_the_bundle_digest(checkpoint: Path, tmp_path: Path):
    before = build(checkpoint, tmp_path / "a", name="tiny", schema_version=1)["digest"]
    write_safetensors(checkpoint / "model.safetensors", payload=b"\x01" * 16)
    after = build(checkpoint, tmp_path / "b", name="tiny", schema_version=1)["digest"]
    assert before != after


# ---------------------------------------------------------------------------------------
# The refusals — the reason this tool exists
# ---------------------------------------------------------------------------------------
@pytest.mark.parametrize("suffix", [".pt", ".pth", ".bin", ".ckpt", ".pkl", ".pickle"])
def test_refuses_every_pickle_extension(checkpoint: Path, suffix: str):
    (checkpoint / f"weights{suffix}").write_bytes(pickle.dumps({"w": [1, 2, 3]}))
    with pytest.raises(ValueError, match="arbitrary code execution"):
        collect(checkpoint)


def test_refuses_a_pickle_renamed_to_safetensors(checkpoint: Path):
    """The extension check is not enough on its own, so the header is parsed too."""
    (checkpoint / "sneaky.safetensors").write_bytes(pickle.dumps({"w": [1, 2, 3]}))
    with pytest.raises(ValueError):
        collect(checkpoint)


def test_refuses_an_unknown_extension(checkpoint: Path):
    (checkpoint / "run.sh").write_text("#!/bin/sh\necho hi\n")
    with pytest.raises(ValueError, match="not an allowed bundle member"):
        collect(checkpoint)


def test_refuses_a_bundle_with_no_weights(tmp_path: Path):
    d = tmp_path / "metadata-only"
    d.mkdir()
    (d / "config.json").write_text("{}")
    with pytest.raises(ValueError, match=r"no \.safetensors file"):
        collect(d)


def test_refuses_an_empty_checkpoint(tmp_path: Path):
    d = tmp_path / "empty"
    d.mkdir()
    with pytest.raises(ValueError, match="nothing to publish"):
        collect(d)


def test_refuses_safetensors_declaring_no_tensors(tmp_path: Path):
    path = write_safetensors(tmp_path / "ckpt" / "empty.safetensors", tensors={"__metadata__": {}})
    with pytest.raises(ValueError, match="declares no tensors"):
        validate_safetensors(path)


def test_refuses_an_absurd_header_length_without_reading_it(tmp_path: Path):
    """The bound has to be checked before the read, or the check is the denial of service."""
    path = tmp_path / "huge.safetensors"
    path.write_bytes(struct.pack("<Q", 2**60) + b"{}")
    with pytest.raises(ValueError, match="not consistent"):
        validate_safetensors(path)


def test_refuses_a_truncated_file(tmp_path: Path):
    path = tmp_path / "short.safetensors"
    path.write_bytes(b"\x00\x01\x02")
    with pytest.raises(ValueError, match="8-byte safetensors header"):
        validate_safetensors(path)


def test_refuses_payload_offsets_beyond_file(tmp_path: Path):
    path = write_safetensors(
        tmp_path / "bad.safetensors",
        tensors={"weight": {"dtype": "F32", "shape": [2], "data_offsets": [0, 8]}},
        payload=b"\x00" * 4,
    )
    with pytest.raises(ValueError, match="offsets exceed"):
        validate_safetensors(path)


def test_refuses_dtype_shape_size_mismatch(tmp_path: Path):
    path = write_safetensors(
        tmp_path / "bad.safetensors",
        tensors={"weight": {"dtype": "F32", "shape": [1], "data_offsets": [0, 8]}},
        payload=b"\x00" * 8,
    )
    with pytest.raises(ValueError, match="dtype/shape require"):
        validate_safetensors(path)


def test_refuses_payload_gaps_and_trailing_bytes(tmp_path: Path):
    gap = write_safetensors(
        tmp_path / "gap.safetensors",
        tensors={"weight": {"dtype": "F32", "shape": [1], "data_offsets": [4, 8]}},
        payload=b"\x00" * 8,
    )
    with pytest.raises(ValueError, match="payload gap"):
        validate_safetensors(gap)

    trailing = write_safetensors(tmp_path / "trailing.safetensors", payload=b"\x00" * 20)
    with pytest.raises(ValueError, match="offsets cover"):
        validate_safetensors(trailing)


def test_refuses_duplicate_header_keys(tmp_path: Path):
    header = (
        b'{"weight":{"dtype":"F32","shape":[1],"data_offsets":[0,4]},'
        b'"weight":{"dtype":"F32","shape":[1],"data_offsets":[0,4]}}'
    )
    path = tmp_path / "duplicate.safetensors"
    path.write_bytes(struct.pack("<Q", len(header)) + header + b"\x00" * 4)
    with pytest.raises(ValueError, match="duplicate JSON key"):
        validate_safetensors(path)


def test_refuses_symlinked_members(checkpoint: Path, tmp_path: Path):
    outside = tmp_path / "outside.json"
    outside.write_text('{"secret": true}')
    (checkpoint / "linked.json").symlink_to(outside)
    with pytest.raises(ValueError, match="symbolic links"):
        collect(checkpoint)


def test_refuses_reserved_input_manifest(checkpoint: Path):
    (checkpoint / "manifest.json").write_text("{}")
    with pytest.raises(ValueError, match="reserved"):
        collect(checkpoint)


@pytest.mark.parametrize("schema_version", [0, 2, -1])
def test_refuses_unsupported_schema_versions(checkpoint: Path, tmp_path: Path, schema_version: int):
    with pytest.raises(ValueError, match="schema version"):
        build(checkpoint, tmp_path / "out", name="tiny", schema_version=schema_version)


@pytest.mark.parametrize("name", ["", "Tiny", "../tiny", "tiny//v1", "tiny/"])
def test_refuses_noncanonical_model_names(checkpoint: Path, tmp_path: Path, name: str):
    with pytest.raises(ValueError, match="model name"):
        build(checkpoint, tmp_path / "out", name=name, schema_version=1)


def test_refuses_overlapping_input_and_output(checkpoint: Path):
    with pytest.raises(ValueError, match="must not overlap"):
        build(checkpoint, checkpoint / "out", name="tiny", schema_version=1)


def test_refuses_nonempty_output(checkpoint: Path, tmp_path: Path):
    out = tmp_path / "out"
    out.mkdir()
    (out / "stale.txt").write_text("stale")
    with pytest.raises(ValueError, match="must not already contain"):
        build(checkpoint, out, name="tiny", schema_version=1)


def test_accepts_an_existing_empty_output_and_commits_atomically(checkpoint: Path, tmp_path: Path):
    out = tmp_path / "out"
    out.mkdir()
    build(checkpoint, out, name="tiny", schema_version=1)
    assert (out / "manifest.json").is_file()
    assert not list(tmp_path.glob(".out.staging-*"))
