#!/usr/bin/env python3
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#
"""Generate the deterministic safetensors bundle used by the reference IPC worker."""

from __future__ import annotations

import argparse
import json
import struct
from pathlib import Path

from tools.release.build_model_bundle import build

MODEL_NAME = "reference-affine-v1"


def write_safetensors(path: Path) -> None:
    # The payload contains two scalar float32 tensors: y = (2 * x) + 0.5.
    header = {
        "bias": {"dtype": "F32", "shape": [], "data_offsets": [0, 4]},
        "scale": {"dtype": "F32", "shape": [], "data_offsets": [4, 8]},
        "__metadata__": {
            "format": "pt",
            "mindclade.reference": "reference-affine-v1",
        },
    }
    encoded = json.dumps(header, sort_keys=True, separators=(",", ":")).encode()
    encoded += b" " * (-len(encoded) % 8)
    payload = struct.pack("<ff", 0.5, 2.0)
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_bytes(struct.pack("<Q", len(encoded)) + encoded + payload)


def generate(out: Path) -> dict[str, object]:
    checkpoint = out.parent / f"{out.name}.checkpoint"
    write_safetensors(checkpoint / "model.safetensors")
    (checkpoint / "config.json").write_text(
        json.dumps(
            {
                "architecture": MODEL_NAME,
                "bias": 0.5,
                "dtype": "float32",
                "operation": "reference.affine.v1",
                "scale": 2.0,
            },
            indent=2,
            sort_keys=True,
        )
        + "\n",
        encoding="utf-8",
    )
    return build(checkpoint, out, name=MODEL_NAME, schema_version=1)


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--out", type=Path, required=True)
    arguments = parser.parse_args()
    manifest = generate(arguments.out)
    print(f"{MODEL_NAME} {manifest['digest']} ({manifest['size_bytes']} bytes)")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
