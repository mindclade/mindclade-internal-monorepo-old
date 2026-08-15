#!/usr/bin/env python3
"""Enforce that canonical Rust target-state paths are implementation, not markers."""

from __future__ import annotations

import re
import tomllib
from pathlib import Path

COMPAT: set[str] = set()
FORBIDDEN = (
    "SCAFFOLD_",
    "todo!()",
    'todo!("',
    "unimplemented!()",
    'unimplemented!("',
    "production implementation pending",
    "target-state scaffold marker",
)
CANONICAL_SERVICES = (
    "services/runtime_gateway",
    "services/runtime_host",
    "services/artifact_proxy",
    "services/node_agent",
    "serving/runtime",
)


def _crate_dirs(root: Path) -> list[Path]:
    return sorted(
        p
        for p in (root / "libs/rust").iterdir()
        if p.is_dir() and (p / "Cargo.toml").exists() and p.name not in COMPAT
    )


def check(root: Path) -> list[str]:
    errors: list[str] = []
    authoritative = []
    for crate in _crate_dirs(root):
        sources = sorted((crate / "src").rglob("*.rs"))
        authoritative.extend(sources)
        if not sources:
            errors.append(f"{crate.relative_to(root)}: authoritative crate has no Rust source")
        tests = sorted((crate / "tests").glob("*.rs")) if (crate / "tests").exists() else []
        if not tests:
            errors.append(
                f"{crate.relative_to(root)}: authoritative crate has no package-local Rust test"
            )
        for source in sources:
            text = source.read_text()
            lowered = text.lower()
            for token in FORBIDDEN:
                if token.lower() in lowered:
                    errors.append(
                        f"{source.relative_to(root)}: forbidden implementation marker {token!r}"
                    )
            # Tiny modules are allowed only when they deliberately re-export or
            # provide a narrow concrete adapter/function. Bare constants are not.
            semantic = [
                line.strip()
                for line in text.splitlines()
                if line.strip() and not line.strip().startswith("//")
            ]
            if len(semantic) <= 2 and not any(
                key in text for key in ("pub use ", "pub fn ", "pub type ", "impl ", "mod ")
            ):
                errors.append(
                    f"{source.relative_to(root)}: target-state module is effectively empty"
                )

    for rel in CANONICAL_SERVICES:
        path = root / rel
        cargo = path / "Cargo.toml"
        if not cargo.exists():
            errors.append(f"{rel}: missing Cargo.toml")
            continue
        source_files = sorted((path / "src").glob("*.rs"))
        if not source_files:
            errors.append(f"{rel}: no implementation source")
        for source in source_files:
            text = source.read_text()
            for token in FORBIDDEN:
                if token.lower() in text.lower():
                    errors.append(
                        f"{source.relative_to(root)}: service still contains scaffold marker {token!r}"
                    )
        tests = sorted((path / "tests").glob("*.rs")) if (path / "tests").exists() else []
        if not tests:
            errors.append(f"{rel}: implemented Rust component must have tests")

    # Cargo manifests for canonical services must consume the foundation rather
    # than being empty composition placeholders.
    required_deps = {
        "services/runtime_gateway": {"mindclade_serving_runtime", "mindclade_worker_protocol"},
        "services/runtime_host": {"mindclade_serving_runtime", "mindclade_worker_runtime"},
        "services/artifact_proxy": {"mindclade_artifact_cas", "mindclade_worker_protocol"},
        "services/node_agent": {
            "mindclade_worker_runtime",
            "mindclade_checkpoint_io",
            "mindclade_data_stream",
        },
        "serving/runtime": {"mindclade_worker_protocol", "mindclade_runtime_core"},
    }
    for rel, expected in required_deps.items():
        data = tomllib.loads((root / rel / "Cargo.toml").read_text())
        actual = set(data.get("dependencies", {}))
        missing = expected - actual
        if missing:
            errors.append(f"{rel}: missing canonical Rust foundation consumers: {sorted(missing)}")

    # Canonical Rust code must not use compatibility crates.
    legacy = re.compile(
        r"mindclade_(clock|retry|resource_version|observability|artifact_manifest|byte_spec|python_bindings)"
    )
    for path in (
        list((root / "libs/rust").rglob("*.rs"))
        + list((root / "services").rglob("*.rs"))
        + list((root / "serving/runtime").rglob("*.rs"))
    ):
        if legacy.search(path.read_text()):
            errors.append(
                f"{path.relative_to(root)}: depends on deprecated Rust compatibility facade"
            )
    return errors


def main() -> int:
    root = Path(__file__).resolve().parents[2]
    errors = check(root)
    for error in errors:
        print(error)
    if errors:
        print(f"Rust implementation check failed: {len(errors)}")
        return 1
    print("Rust implementation check passed")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
