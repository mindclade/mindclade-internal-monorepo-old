#!/usr/bin/env python3
"""Run bounded fuzz targets for untrusted Rust parsers and protocol decoders."""

from __future__ import annotations

import argparse
import shutil
import subprocess
import sys
import tomllib
from pathlib import Path

ROOT = Path(__file__).resolve().parents[3]
FUZZ_CRATES = (
    "libs/rust/bio_formats",
    "libs/rust/ipc",
)


def target_names(manifest: Path) -> list[str]:
    payload = tomllib.loads(manifest.read_text())
    names = [entry["name"] for entry in payload.get("bin", []) if entry.get("name")]
    if not names:
        raise RuntimeError(f"no fuzz targets declared in {manifest}")
    return names


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--required", action="store_true")
    parser.add_argument("--seconds", type=int, default=30)
    args = parser.parse_args()
    if args.seconds <= 0 or args.seconds > 3600:
        raise SystemExit("--seconds must be in [1, 3600]")

    cargo = shutil.which("cargo")
    cargo_fuzz = shutil.which("cargo-fuzz")
    if not cargo or not cargo_fuzz:
        if args.required:
            print("cargo/cargo-fuzz unavailable", file=sys.stderr)
            return 1
        print("cargo-fuzz unavailable; skipped")
        return 0

    for relative in FUZZ_CRATES:
        crate_root = ROOT / relative
        manifest = crate_root / "fuzz" / "Cargo.toml"
        if not manifest.exists():
            print(f"required fuzz manifest missing: {manifest}", file=sys.stderr)
            return 1
        for target in target_names(manifest):
            subprocess.run(
                [
                    cargo,
                    "fuzz",
                    "run",
                    target,
                    "--",
                    f"-max_total_time={args.seconds}",
                    "-rss_limit_mb=2048",
                ],
                cwd=crate_root,
                check=True,
            )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
