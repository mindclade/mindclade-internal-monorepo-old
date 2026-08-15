#!/usr/bin/env python3
from __future__ import annotations

import argparse
import re
import tomllib
from pathlib import Path

FORBIDDEN = [
    re.compile(r"\bpip\s+install\b"),
    re.compile(r"\bnpm\s+install\b"),
    re.compile(r"\bcargo\s+fetch\b"),
    re.compile(r"curl\b[^\n|]*\|\s*(?:bash|sh)\b"),
    re.compile(r"(?<![\w.-])/usr/bin/(?!env\b)"),
]
SCAN = {".bzl", ".bazel", ".sh", ".py", ".yml", ".yaml", ".nix"}


def rust_version_contract(root: Path) -> list[str]:
    errors = []
    cargo = tomllib.loads((root / "Cargo.toml").read_text())
    expected = cargo.get("workspace", {}).get("package", {}).get("rust-version")
    nix = (root / "tools/build/nix/versions.nix").read_text(errors="replace")
    flake = (root / "flake.nix").read_text(errors="replace")
    qualify = (
        (root / "tools/qualification/rust/common.py").read_text(errors="replace")
        if (root / "tools/qualification/rust/common.py").exists()
        else ""
    )
    if not expected:
        errors.append("Cargo workspace rust-version is missing")
        return errors
    if f'rust = "{expected}"' not in nix:
        errors.append("Nix Rust version does not match Cargo rust-version")
    if "rust-overlay" not in flake or "toolchains/rust.nix" not in flake:
        errors.append("flake.nix must construct the pinned Rust toolchain through rust-overlay")
    if expected not in qualify:
        errors.append("Rust qualification expected-version does not match Cargo rust-version")
    return errors


def check(root: Path):
    errors = []
    if (root / "WORKSPACE").exists() or (root / "WORKSPACE.bazel").exists():
        errors.append("legacy WORKSPACE is forbidden; Bzlmod owns Bazel dependencies")
    errors.extend(rust_version_contract(root))
    for p in root.rglob("*"):
        if (
            not p.is_file()
            or p.resolve() == Path(__file__).resolve()
            or any(x in p.parts for x in (".git", "node_modules", "__pycache__"))
        ):
            continue
        if p.name == "Dockerfile" or p.name.startswith("Dockerfile."):
            text = p.read_text(errors="replace")
            if "MINDCLADE_DEV_ONLY=1" not in text:
                errors.append(
                    f"{p.relative_to(root)}: production Dockerfiles are forbidden; Bazel OCI owns images"
                )
        if p.suffix in SCAN or p.name in {"BUILD", "BUILD.bazel"}:
            text = p.read_text(errors="replace")
            for rx in FORBIDDEN:
                if rx.search(text):
                    errors.append(
                        f"{p.relative_to(root)}: forbidden host/package-manager pattern: {rx.pattern}"
                    )
    return sorted(set(errors))


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--repo", type=Path, default=Path(__file__).resolve().parents[2])
    a = ap.parse_args()
    e = check(a.repo.resolve())
    [print(x) for x in e]
    print(
        "Bazel/Nix ownership check passed"
        if not e
        else f"Bazel/Nix ownership check failed: {len(e)}"
    )
    return 1 if e else 0


if __name__ == "__main__":
    raise SystemExit(main())
