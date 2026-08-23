#!/usr/bin/env python3
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Run bounded fuzz targets for untrusted Rust parsers and protocol decoders."""

from __future__ import annotations

import argparse
import shutil
import subprocess
import sys
import tempfile
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


def harness_tests(cargo: str, manifest: Path) -> None:
    """Run a fuzz crate's own unit tests before fuzzing with it.

    A fuzz crate is its own cargo workspace, so nothing in the root workspace
    builds or tests it. Where the crate carries harness logic -- deriving parse
    limits from fuzzer bytes, for instance -- a regression there leaves every
    target running and reporting no findings while exercising almost nothing.
    That failure is invisible in fuzz output, so the tests run here or nowhere.
    """
    if not tomllib.loads(manifest.read_text()).get("lib", {}).get("test"):
        return
    subprocess.run(
        [cargo, "test", "--manifest-path", str(manifest), "--lib"],
        check=True,
    )


def seeded_corpus(crate_root: Path, target: str, scratch: Path) -> list[str]:
    """Copy committed seeds into a writable corpus and return the libFuzzer args.

    libFuzzer *writes* newly discovered inputs into whatever corpus directory it
    is given. Pointing it straight at the committed `corpus/` would let a fuzzer
    deposit generated files into a tree whose whole policy is that every byte is
    hand-written and synthetic, so the seeds are copied into scratch first and
    the committed directory is only ever read.
    """
    seeds = crate_root / "corpus" / target
    if not seeds.is_dir():
        return []
    working = scratch / target
    shutil.copytree(seeds, working)
    return [str(working)]


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
        harness_tests(cargo, manifest)
        with tempfile.TemporaryDirectory(prefix="mindclade-fuzz-corpus-") as scratch:
            for target in target_names(manifest):
                subprocess.run(
                    [
                        cargo,
                        "fuzz",
                        "run",
                        target,
                        *seeded_corpus(crate_root, target, Path(scratch)),
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
