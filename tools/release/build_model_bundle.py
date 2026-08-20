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
import shutil
import struct
import sys
from pathlib import Path

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
        if header_len == 0 or header_len > 100_000_000 or header_len > path.stat().st_size:
            raise ValueError(
                f"{path.name}: declares a {header_len}-byte header, which is not consistent "
                f"with a {path.stat().st_size}-byte file. A pickle renamed to .safetensors "
                f"fails here."
            )

        try:
            header = json.loads(handle.read(header_len))
        except (UnicodeDecodeError, json.JSONDecodeError) as exc:
            raise ValueError(f"{path.name}: safetensors header is not valid JSON: {exc}") from exc

    if not isinstance(header, dict):
        raise ValueError(f"{path.name}: safetensors header is not a JSON object.")

    tensors = {k: v for k, v in header.items() if k != "__metadata__"}
    if not tensors:
        raise ValueError(
            f"{path.name}: declares no tensors. An empty weights file builds, pushes, mounts "
            f"and serves nothing — a failure that surfaces as bad outputs rather than an error."
        )

    for tensor_name, spec in tensors.items():
        if not isinstance(spec, dict) or "dtype" not in spec or "shape" not in spec:
            raise ValueError(
                f"{path.name}: tensor {tensor_name!r} has no dtype/shape; the header is not a "
                f"safetensors header."
            )


def collect(checkpoint: Path) -> list[Path]:
    """Every file in the checkpoint, sorted, with the format rules applied."""
    files = sorted(p for p in checkpoint.rglob("*") if p.is_file())
    if not files:
        raise ValueError(f"{checkpoint}: empty. There is nothing to publish.")

    rejected = []
    for path in files:
        suffix = path.suffix.lower()
        if suffix in PICKLE_EXTENSIONS:
            rejected.append(
                f"  {path.relative_to(checkpoint)}: {suffix} is a pickle format. Loading it is "
                f"arbitrary code execution, and this bundle crosses an admission boundary where "
                f"a signature is taken to mean the contents are safe. Convert it with "
                f"safetensors.torch.save_file."
            )
        elif suffix not in ALLOWED_EXTENSIONS:
            rejected.append(
                f"  {path.relative_to(checkpoint)}: {suffix or '(no extension)'} is not an "
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


def build(checkpoint: Path, out: Path, name: str, schema_version: int) -> dict:
    files = collect(checkpoint)
    out.mkdir(parents=True, exist_ok=True)

    members = []
    for path in files:
        digest, size = sha256_file(path)
        relative = path.relative_to(checkpoint).as_posix()
        destination = out / relative
        destination.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(path, destination)
        members.append(
            {
                "path": relative,
                # Field names are ArtifactRef's, from
                # protocols/proto/mindclade/artifact/v1/artifact.proto. The platform manifest
                # is authoritative for identity (ADR-0004/ADR-0022); the OCI annotations this
                # feeds are a projection carried for admission and portability.
                "digest": digest,
                "size_bytes": size,
                "media_type": (
                    SAFETENSORS_MEDIA_TYPE
                    if path.suffix.lower() == ".safetensors"
                    else "application/json"
                ),
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
    (out / "manifest.json").write_text(json.dumps(manifest, indent=2, sort_keys=True) + "\n")
    return manifest


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
