#!/usr/bin/env python3
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Reject accidental saturating/wrapping arithmetic in production Rust hot paths."""

from __future__ import annotations

import re
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
APPROVED = {
    "libs/rust/content_digest/src/sha256.rs": {
        "wrapping_add": "SHA-256 compression and specified modulo-2^64 length encoding",
        "wrapping_mul": "SHA-256 specified modulo-2^64 bit length",
    },
    "libs/rust/runtime_core/src/deadline.rs": {
        "saturating_duration_since": "monotonic remaining-duration calculation",
    },
    "libs/rust/record_io/src/reader.rs": {
        "saturating_sub": "remaining bounded byte count",
    },
    "libs/rust/record_io/src/codec.rs": {
        "saturating_sub": "remaining decoder bytes; offset is independently range-checked",
    },
    "libs/rust/atomic_fs/src/store.rs": {
        "saturating_add": "bounded-reader sentinel; u64::MAX is already the largest possible limit",
    },
    "libs/rust/runtime_core/src/retry.rs": {
        "wrapping_add": "SplitMix64 deterministic jitter mixer",
        "wrapping_mul": "SplitMix64 deterministic jitter mixer",
    },
    "libs/rust/data_stream/src/shuffle.rs": {
        "wrapping_add": "SplitMix64 deterministic shuffle mixer",
        "wrapping_mul": "SplitMix64 deterministic shuffle mixer",
    },
    "libs/rust/identifiers/src/resource_id.rs": {
        "wrapping_mul": "non-cryptographic UUIDv7 entropy mixer",
    },
    "libs/rust/object_store/src/metrics.rs": {
        "saturating_add": "telemetry-only counters, never authorization/accounting",
    },
}
PATTERN = re.compile(r"\.(saturating_[a-z_]+|wrapping_[a-z_]+)\(")


def check() -> list[str]:
    errors: list[str] = []
    roots = [ROOT / "libs/rust", ROOT / "services", ROOT / "serving/runtime"]
    for base in roots:
        for path in base.rglob("*.rs"):
            if "tests" in path.parts:
                continue
            rel = path.relative_to(ROOT).as_posix()
            approved = APPROVED.get(rel, {})
            for line_number, line in enumerate(path.read_text().splitlines(), 1):
                for method in PATTERN.findall(line):
                    if method not in approved:
                        errors.append(
                            f"{rel}:{line_number}: {method} requires checked arithmetic or explicit approval"
                        )
    return errors


def main() -> int:
    errors = check()
    for error in errors:
        print(error)
    if errors:
        print(f"Rust arithmetic policy failed: {len(errors)}")
        return 1
    print("Rust arithmetic policy passed")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
