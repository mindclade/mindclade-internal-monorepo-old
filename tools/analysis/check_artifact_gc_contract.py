#!/usr/bin/env python3
from __future__ import annotations

from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]


def check(root: Path) -> list[str]:
    required = {
        "control/artifacts/gc.go": (
            "ArtifactReachability",
            "ArtifactLease",
            "ArtifactPin",
            "RetentionHold",
            "ObjectPath",
            "ObservedObjectVersion",
            "ValidateGCReceipt",
        ),
        "libs/rust/artifact_cas/src/gc.rs": (
            "expected_version",
            "store.delete",
            "SweepOutcome",
            "SweepResult",
        ),
        "protocols/proto/mindclade/artifact/v1/artifact.proto": (
            "GarbageCollectionPlan",
            "GarbageCollectionReceipt",
            "object_path",
            "expected_object_version",
        ),
    }
    errors = []
    for rel, tokens in required.items():
        p = root / rel
        if not p.exists():
            errors.append(f"missing GC contract file: {rel}")
            continue
        text = p.read_text(errors="replace")
        for token in tokens:
            if token not in text:
                errors.append(f"{rel}: missing {token}")
    return errors


def main() -> int:
    errors = check(ROOT)
    [print(e) for e in errors]
    if errors:
        return 1
    print("artifact GC contract passed")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
