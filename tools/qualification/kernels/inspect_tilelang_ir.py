# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

"""Lexically inspect bounded generated source against an architecture token contract.

This tool emits content-addressed heuristic evidence. It does not prove that an
instruction survives compilation, is selected by dispatch, or executes at runtime.
"""

from __future__ import annotations

import argparse
import json
from pathlib import Path

from kernels.tilelang.compiler.ir import (
    MAXIMUM_GENERATED_SOURCE_BYTES,
    KernelSourceArtifact,
)
from kernels.tilelang.compiler.lowering import require_codegen_tokens


def inspect_source(
    source: str,
    *,
    target: str,
    compiler_version: str,
    required: tuple[str, ...],
    forbidden: tuple[str, ...],
) -> dict[str, object]:
    if not required:
        raise ValueError("generated-source inspection requires at least one expected token")
    artifact = KernelSourceArtifact(source, target, compiler_version)
    require_codegen_tokens(artifact, required=required, forbidden=forbidden)
    return {
        "compiler_version": compiler_version,
        "forbidden_tokens": list(forbidden),
        "identity_digest": artifact.identity_digest,
        "required_tokens": list(required),
        "source_digest": artifact.source_digest,
        "target": target,
        "token_contract_satisfied": True,
    }


def _read_bounded_source(path: Path) -> str:
    with path.open("rb") as handle:
        payload = handle.read(MAXIMUM_GENERATED_SOURCE_BYTES + 1)
    if len(payload) > MAXIMUM_GENERATED_SOURCE_BYTES:
        raise ValueError(
            f"generated source exceeds the {MAXIMUM_GENERATED_SOURCE_BYTES}-byte inspection limit"
        )
    try:
        return payload.decode("utf-8")
    except UnicodeDecodeError as exc:
        raise ValueError("generated source must be valid UTF-8") from exc


def _parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--source", required=True, type=Path)
    parser.add_argument("--target", required=True)
    parser.add_argument("--compiler-version", required=True)
    parser.add_argument("--required", action="append", default=[])
    parser.add_argument("--forbidden", action="append", default=[])
    return parser


def main() -> int:
    args = _parser().parse_args()
    result = inspect_source(
        _read_bounded_source(args.source),
        target=args.target,
        compiler_version=args.compiler_version,
        required=tuple(args.required),
        forbidden=tuple(args.forbidden),
    )
    print(json.dumps(result, sort_keys=True, separators=(",", ":")))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
