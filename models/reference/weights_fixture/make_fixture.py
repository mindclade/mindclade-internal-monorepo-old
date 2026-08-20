#!/usr/bin/env python3
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#
"""Generate a synthetic weights checkpoint and stage it as a model bundle.

WHY A FIXTURE AND NOT A MODEL.

There are no trained weights in this repository, and there will not be for some time:
`models/`, `training/`, `kernels/` and `evaluation/` hold no implementation modules. The
distribution, admission and rollout machinery is independent of the model, and is worth
building first because it has to exist BEFORE the first real checkpoint appears — otherwise
the first thing that happens to a real checkpoint is that somebody invents a way to ship it.

So this produces a deterministic, obviously-synthetic bundle that exercises the whole path:
Bazel packaging, OCI push, digest pinning, attestation, image-volume mount. When a real model
arrives, `mindclade_model_bundle` takes its files instead and nothing else changes.

DETERMINISTIC ON PURPOSE. The tensor payload is a fixed byte pattern rather than random data,
so the bundle digest is stable across builds. A fixture whose digest moved on every build
would make the "same weights promote to the same digest" property untestable, which is one of
the things this path exists to guarantee.

NO torch, NO safetensors, NO numpy. The file is written by packing the header directly — the
format is an 8-byte little-endian header length, then that many bytes of JSON, then the tensor
data. Depending on torch to produce four kilobytes of test data would put a multi-gigabyte CUDA
closure into a build that needs none of it.
"""

from __future__ import annotations

import argparse
import json
import struct
from pathlib import Path

# A normal import, not a sys.path insertion: //tools/release:build_model_bundle_lib sets
# `imports` so its directory is an import root, and this package depends on it.
from build_model_bundle import build

# 4 tensors, 1024 float32 values each. Large enough that the artifact is a real multi-layer
# pull rather than a rounding error, small enough that nobody waits for it.
TENSORS = ["encoder.weight", "encoder.bias", "decoder.weight", "decoder.bias"]
VALUES = 1024
ITEM_SIZE = 4


def write_safetensors(path: Path) -> None:
    header: dict[str, object] = {}
    offset = 0
    for name in TENSORS:
        length = VALUES * ITEM_SIZE
        header[name] = {
            "dtype": "F32",
            "shape": [VALUES],
            "data_offsets": [offset, offset + length],
        }
        offset += length

    # __metadata__ is where safetensors permits free-form strings. Saying so in the file itself
    # means an operator who finds this mounted in a pod does not have to guess.
    header["__metadata__"] = {
        "format": "pt",
        "mindclade.fixture": "true",
        "mindclade.note": "Synthetic weights. Not a trained model. Do not evaluate.",
    }

    blob = json.dumps(header, sort_keys=True).encode()

    # A fixed repeating pattern, not zeros: an all-zero payload compresses to nothing and would
    # hide a layer-size problem in the OCI push.
    payload = bytes(range(256)) * (offset // 256) + bytes(range(offset % 256))

    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_bytes(struct.pack("<Q", len(blob)) + blob + payload)


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument(
        "--out", type=Path, required=True, help="directory to write the staged bundle into"
    )
    args = ap.parse_args()

    staging = args.out.parent / (args.out.name + ".checkpoint")
    write_safetensors(staging / "model.safetensors")
    (staging / "config.json").write_text(
        json.dumps(
            {
                "architecture": "mindclade-fixture",
                "hidden_size": VALUES,
                "note": "Synthetic. Shapes are arbitrary and carry no meaning.",
            },
            indent=2,
            sort_keys=True,
        )
        + "\n"
    )

    # Staged through the real tool rather than written directly, so the fixture is also a live
    # check that build_model_bundle accepts what it is supposed to accept. If the format rules
    # tighten, this build breaks — which is the correct place to find out.
    manifest = build(staging, args.out, name="mindclade-fixture", schema_version=1)
    print(f"fixture {manifest['digest']} ({manifest['size_bytes']} bytes)")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
