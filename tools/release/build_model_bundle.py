#!/usr/bin/env python3
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#
"""Stage a checkpoint into the canonical model-bundle layout.

This is the step BEFORE Bazel packages the bundle as an OCI artifact. The split is
deliberate: Bazel owns the packaging (pkg_tar -> oci_image -> oci_push, see
tools/build/bazel/rules/release.bzl), and this owns the part that has to read the files —
rejecting formats that must not cross an admission boundary, and producing the manifest that
identifies what is inside.

WHY THIS REFUSES PICKLE, AT LENGTH, BECAUSE IT IS THE WHOLE POINT.

`torch.load` unpickles, and unpickling is arbitrary code execution: a `.pt` file is a program,
not data. Everything downstream of this tool treats the bundle as inert content — it is signed,
attested by a biosecurity attestor, admitted by a Gatekeeper constraint, and mounted read-only
into a serving pod. Every one of those controls is about WHICH bytes are allowed to run, and
none of them helps if loading the approved bytes executes whatever the file says to execute.

An attacker who can influence a checkpoint does not need to defeat the signature: they need
the signed artifact to contain a pickle. So the format is constrained here, at the point where
a human-produced checkpoint becomes a platform artifact, rather than trusted later.

safetensors is the format that cannot do this. Its header is JSON describing dtypes, shapes
and byte offsets; loading is a parse and a memory map. There is no opcode stream.

NO TORCH IMPORT. The safetensors header is validated by parsing it directly — 8 bytes of
little-endian header length, then that many bytes of JSON. Depending on torch here would put
the largest dependency in the tree, and a CUDA closure, into the artifact-staging step of a
release; it would also mean the tool that exists to avoid deserializing untrusted tensors
would itself pull in the library that deserializes them.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import re
import shutil
import stat
import struct
import sys
import tempfile
from pathlib import Path
from typing import Final

# The media type recorded for each weight file, and the one the OCI layer advertises.
# Not application/octet-stream: the point of a media type is that a consumer can refuse what it
# does not understand, and "some bytes" is not something anything can refuse.
SAFETENSORS_MEDIA_TYPE = "application/vnd.mindclade.model.weights.v1+safetensors"
MANIFEST_MEDIA_TYPE = "application/vnd.mindclade.model.manifest.v1+json"

# Extensions that carry a pickle, directly or by convention. Refused rather than warned about.
#
# `.bin` is on the list because that is what a HuggingFace `pytorch_model.bin` is — a pickle
# with an extension that suggests otherwise, which is exactly the case a reviewer skims past.
PICKLE_EXTENSIONS = frozenset({".pt", ".pth", ".bin", ".ckpt", ".pkl", ".pickle", ".npy", ".npz"})

# What may appear in a bundle. Weights and the metadata that describes them, nothing else.
ALLOWED_EXTENSIONS = frozenset({".safetensors", ".json", ".txt", ".md"})

CHUNK = 1 << 20
MAXIMUM_MEMBERS: Final = 4096
MAXIMUM_MEMBER_PATH_BYTES: Final = 4096
MAXIMUM_HEADER_BYTES: Final = 100_000_000
SUPPORTED_SCHEMA_VERSION: Final = 1

_MODEL_NAME = re.compile(r"[a-z0-9][a-z0-9._/-]{0,254}[a-z0-9]$|[a-z0-9]$")
_DTYPE_BYTES: Final = {
    "BOOL": 1,
    "U8": 1,
    "I8": 1,
    "F8_E4M3": 1,
    "F8_E5M2": 1,
    "F8_E8M0": 1,
    "U16": 2,
    "I16": 2,
    "F16": 2,
    "BF16": 2,
    "U32": 4,
    "I32": 4,
    "F32": 4,
    "U64": 8,
    "I64": 8,
    "F64": 8,
}


def _unique_json_object(pairs: list[tuple[str, object]]) -> dict[str, object]:
    result: dict[str, object] = {}
    for key, value in pairs:
        if key in result:
            raise ValueError(f"duplicate JSON key {key!r}")
        result[key] = value
    return result


def sha256_file(path: Path) -> tuple[str, int]:
    """Return (sha256:<hex>, size_bytes), streamed."""
    digest = hashlib.sha256()
    size = 0
    with path.open("rb") as handle:
        while chunk := handle.read(CHUNK):
            digest.update(chunk)
            size += len(chunk)
    return f"sha256:{digest.hexdigest()}", size


def validate_safetensors(path: Path) -> None:
    """Parse the safetensors header, raising ValueError if it is not one.

    This is a structural check, not a signature check — it establishes that the file is the
    format it claims to be, so that a pickle renamed to `.safetensors` does not walk past the
    extension test above. It reads only the header, never the tensor data.
    """
    with path.open("rb") as handle:
        prefix = handle.read(8)
        if len(prefix) < 8:
            raise ValueError(
                f"{path.name}: shorter than the 8-byte safetensors header length prefix; "
                f"this is not a safetensors file."
            )
        (header_len,) = struct.unpack("<Q", prefix)

        # A pickle renamed to .safetensors reaches here. Its first 8 bytes read as an enormous
        # or nonsensical length, so the bound is what catches it — and the bound has to come
        # before the read, or the check itself is the denial of service.
        file_size = os.fstat(handle.fileno()).st_size
        if (
            header_len == 0
            or header_len > MAXIMUM_HEADER_BYTES
            or header_len > file_size - len(prefix)
        ):
            raise ValueError(
                f"{path.name}: declares a {header_len}-byte header, which is not consistent "
                f"with a {file_size}-byte file. A pickle renamed to .safetensors "
                f"fails here."
            )

        try:
            header = json.loads(
                handle.read(header_len),
                object_pairs_hook=_unique_json_object,
                parse_constant=lambda value: (_ for _ in ()).throw(
                    ValueError(f"non-finite JSON number {value}")
                ),
            )
        except (UnicodeDecodeError, json.JSONDecodeError, ValueError) as exc:
            raise ValueError(f"{path.name}: safetensors header is not valid JSON: {exc}") from exc

    if not isinstance(header, dict):
        raise ValueError(f"{path.name}: safetensors header is not a JSON object.")

    metadata = header.get("__metadata__")
    if metadata is not None and (
        not isinstance(metadata, dict)
        or any(
            not isinstance(key, str) or not isinstance(value, str)
            for key, value in metadata.items()
        )
    ):
        raise ValueError(f"{path.name}: safetensors metadata must map strings to strings.")

    tensors = {k: v for k, v in header.items() if k != "__metadata__"}
    if not tensors:
        raise ValueError(
            f"{path.name}: declares no tensors. An empty weights file builds, pushes, mounts "
            f"and serves nothing — a failure that surfaces as bad outputs rather than an error."
        )

    payload_size = file_size - 8 - header_len
    intervals: list[tuple[int, int, str]] = []
    for tensor_name, spec in tensors.items():
        if not isinstance(tensor_name, str) or not tensor_name or len(tensor_name) > 1024:
            raise ValueError(f"{path.name}: tensor names must be non-empty and bounded.")
        if not isinstance(spec, dict) or set(spec) != {"dtype", "shape", "data_offsets"}:
            raise ValueError(
                f"{path.name}: tensor {tensor_name!r} must declare only dtype, shape and "
                "data_offsets."
            )
        dtype = spec["dtype"]
        shape = spec["shape"]
        offsets = spec["data_offsets"]
        if not isinstance(dtype, str) or dtype not in _DTYPE_BYTES:
            raise ValueError(
                f"{path.name}: tensor {tensor_name!r} has unsupported dtype {dtype!r}."
            )
        if (
            not isinstance(shape, list)
            or len(shape) > 32
            or any(isinstance(dim, bool) or not isinstance(dim, int) or dim < 0 for dim in shape)
        ):
            raise ValueError(f"{path.name}: tensor {tensor_name!r} has an invalid shape.")
        if (
            not isinstance(offsets, list)
            or len(offsets) != 2
            or any(isinstance(offset, bool) or not isinstance(offset, int) for offset in offsets)
        ):
            raise ValueError(f"{path.name}: tensor {tensor_name!r} has invalid data offsets.")
        start, end = offsets
        if start < 0 or end < start or end > payload_size:
            raise ValueError(
                f"{path.name}: tensor {tensor_name!r} offsets exceed the {payload_size}-byte payload."
            )
        elements = 1
        for dimension in shape:
            elements *= dimension
        expected_bytes = elements * _DTYPE_BYTES[dtype]
        if end - start != expected_bytes:
            raise ValueError(
                f"{path.name}: tensor {tensor_name!r} declares {end - start} bytes but "
                f"dtype/shape require {expected_bytes}."
            )
        intervals.append((start, end, tensor_name))

    cursor = 0
    for start, end, tensor_name in sorted(intervals):
        if start != cursor:
            relation = "overlap" if start < cursor else "gap"
            raise ValueError(
                f"{path.name}: tensor {tensor_name!r} introduces a payload {relation} at byte {start}."
            )
        cursor = end
    if cursor != payload_size:
        raise ValueError(
            f"{path.name}: tensor offsets cover {cursor} of {payload_size} payload bytes."
        )


def collect(checkpoint: Path) -> list[Path]:
    """Every file in the checkpoint, sorted, with the format rules applied."""
    if not checkpoint.is_dir() or checkpoint.is_symlink():
        raise ValueError(f"{checkpoint}: checkpoint must be a real directory, not a symlink.")

    files: list[Path] = []
    for path in checkpoint.rglob("*"):
        if path.is_symlink():
            raise ValueError(
                f"{path.relative_to(checkpoint)}: symbolic links are not allowed in model bundles."
            )
        if path.is_file():
            files.append(path)
            if len(files) > MAXIMUM_MEMBERS:
                raise ValueError(
                    f"{checkpoint}: contains more than {MAXIMUM_MEMBERS} files; refusing an "
                    "unbounded bundle."
                )
    files.sort()
    if not files:
        raise ValueError(f"{checkpoint}: empty. There is nothing to publish.")

    rejected = []
    for path in files:
        relative = path.relative_to(checkpoint)
        relative_text = relative.as_posix()
        suffix = path.suffix.lower()
        if len(relative_text.encode("utf-8")) > MAXIMUM_MEMBER_PATH_BYTES:
            rejected.append(f"  {relative_text[:256]}: member path exceeds the byte limit.")
        elif relative_text == "manifest.json":
            rejected.append(
                "  manifest.json: reserved for the canonical bundle manifest written by this tool."
            )
        elif suffix in PICKLE_EXTENSIONS:
            rejected.append(
                f"  {relative}: {suffix} is a pickle format. Loading it is "
                f"arbitrary code execution, and this bundle crosses an admission boundary where "
                f"a signature is taken to mean the contents are safe. Convert it with "
                f"safetensors.torch.save_file."
            )
        elif suffix not in ALLOWED_EXTENSIONS:
            rejected.append(
                f"  {relative}: {suffix or '(no extension)'} is not an "
                f"allowed bundle member. Allowed: {', '.join(sorted(ALLOWED_EXTENSIONS))}."
            )

    if rejected:
        raise ValueError("refusing to build the bundle:\n" + "\n".join(rejected))

    for path in files:
        if path.suffix.lower() == ".safetensors":
            validate_safetensors(path)

    if not any(p.suffix.lower() == ".safetensors" for p in files):
        raise ValueError(
            f"{checkpoint}: contains no .safetensors file. A bundle of metadata with no weights "
            f"would publish and mount successfully, which is the worst way for this to fail."
        )
    return files


def _copy_regular_file(source: Path, destination: Path) -> None:
    """Copy one already-admitted file without following a last-moment symlink swap."""
    flags = os.O_RDONLY | getattr(os, "O_NOFOLLOW", 0)
    try:
        descriptor = os.open(source, flags)
    except OSError as exc:
        raise ValueError(f"{source.name}: could not open admitted bundle member: {exc}") from exc
    try:
        source_stat = os.fstat(descriptor)
        if not stat.S_ISREG(source_stat.st_mode):
            raise ValueError(f"{source.name}: bundle members must remain regular files.")
        destination.parent.mkdir(parents=True, exist_ok=True)
        with os.fdopen(descriptor, "rb", closefd=False) as reader, destination.open("xb") as writer:
            shutil.copyfileobj(reader, writer, length=CHUNK)
        destination.chmod(0o644)
    finally:
        os.close(descriptor)


def _media_type(path: Path) -> str:
    return {
        ".safetensors": SAFETENSORS_MEDIA_TYPE,
        ".json": "application/json",
        ".txt": "text/plain; charset=utf-8",
        ".md": "text/markdown; charset=utf-8",
    }[path.suffix.lower()]


def _fsync_directory(path: Path) -> None:
    descriptor = os.open(path, os.O_RDONLY)
    try:
        os.fsync(descriptor)
    finally:
        os.close(descriptor)


def build(checkpoint: Path, out: Path, name: str, schema_version: int) -> dict:
    if schema_version != SUPPORTED_SCHEMA_VERSION:
        raise ValueError(
            f"schema version must be {SUPPORTED_SCHEMA_VERSION}; got {schema_version}."
        )
    if (
        not isinstance(name, str)
        or not _MODEL_NAME.fullmatch(name)
        or name.startswith("/")
        or name.endswith("/")
        or "//" in name
        or any(segment in {".", ".."} for segment in name.split("/"))
    ):
        raise ValueError("model name must be a bounded canonical lowercase name.")

    checkpoint = checkpoint.resolve()
    out = out.resolve()
    if checkpoint == out or checkpoint in out.parents or out in checkpoint.parents:
        raise ValueError("checkpoint and output directories must not overlap.")
    files = collect(checkpoint)
    if out.exists() and (not out.is_dir() or any(out.iterdir())):
        raise ValueError(f"{out}: output directory must not already contain files.")
    out.parent.mkdir(parents=True, exist_ok=True)
    staging = Path(tempfile.mkdtemp(prefix=f".{out.name}.staging-", dir=out.parent))
    try:
        members = []
        for path in files:
            relative = path.relative_to(checkpoint).as_posix()
            destination = staging / relative
            _copy_regular_file(path, destination)
            if destination.suffix.lower() == ".safetensors":
                # Validate the copied bytes too. This closes the source-mutation window between
                # admission and copying while keeping the release tool independent of Torch.
                validate_safetensors(destination)
            digest, size = sha256_file(destination)
            members.append(
                {
                    "path": relative,
                    # Field names are ArtifactRef's, from
                    # protocols/proto/mindclade/artifact/v1/artifact.proto. The platform manifest
                    # is authoritative for identity (ADR-0004/ADR-0022); the OCI annotations this
                    # feeds are a projection carried for admission and portability.
                    "digest": digest,
                    "size_bytes": size,
                    "media_type": _media_type(path),
                    "logical_kind": "model.weights"
                    if path.suffix.lower() == ".safetensors"
                    else "model.metadata",
                    "schema_version": schema_version,
                }
            )

        # The bundle's own identity: a digest over the sorted member refs, not over a tarball.
        #
        # A tar digest depends on mtimes, ordering and padding, so the same weights repacked twice
        # produce different bytes and look like a different model. Hashing the identity records
        # makes the bundle digest a function of the CONTENT, which is what "the same weights" has
        # to mean for a promotion between registries to be checkable.
        canonical = json.dumps(members, sort_keys=True, separators=(",", ":")).encode()
        bundle_digest = "sha256:" + hashlib.sha256(canonical).hexdigest()

        manifest = {
            "schema_version": schema_version,
            "media_type": MANIFEST_MEDIA_TYPE,
            "logical_kind": "model.bundle",
            "name": name,
            "digest": bundle_digest,
            "size_bytes": sum(m["size_bytes"] for m in members),
            "members": members,
        }
        manifest_path = staging / "manifest.json"
        with manifest_path.open("x", encoding="utf-8") as handle:
            handle.write(json.dumps(manifest, indent=2, sort_keys=True) + "\n")
            handle.flush()
            os.fsync(handle.fileno())
        _fsync_directory(staging)

        if out.exists():
            out.rmdir()  # It was proven empty above; fail if another writer raced us.
        os.replace(staging, out)
        _fsync_directory(out.parent)
        return manifest
    finally:
        if staging.exists():
            shutil.rmtree(staging)


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    ap.add_argument(
        "--checkpoint", type=Path, required=True, help="directory holding the checkpoint"
    )
    ap.add_argument("--out", type=Path, required=True, help="directory to stage the bundle into")
    ap.add_argument("--name", required=True, help="logical model name recorded in the manifest")
    ap.add_argument("--schema-version", type=int, default=1)
    args = ap.parse_args()

    try:
        manifest = build(args.checkpoint, args.out, args.name, args.schema_version)
    except ValueError as exc:
        print(f"build_model_bundle: {exc}", file=sys.stderr)
        return 1

    print(
        f"{manifest['name']} {manifest['digest']} ({len(manifest['members'])} files, {manifest['size_bytes']} bytes)"
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
