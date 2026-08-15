"""Shared helpers for the repository-owned Rust qualification lane."""

from __future__ import annotations

import os
import shutil
import subprocess
from collections.abc import Iterable
from pathlib import Path

ROOT = Path(__file__).resolve().parents[3]
EXPECTED_RUSTC = "1.97.1"


def require_tool(name: str) -> str:
    path = shutil.which(name)
    if not path:
        raise RuntimeError(f"required Rust qualification tool is unavailable: {name}")
    return path


def run(command: Iterable[str], *, env: dict[str, str] | None = None) -> None:
    merged = os.environ.copy()
    if env:
        merged.update(env)
    subprocess.run(list(command), cwd=ROOT, env=merged, check=True)


def verify_toolchain() -> None:
    rustc = require_tool("rustc")
    output = subprocess.check_output([rustc, "--version"], text=True).strip()
    if f"rustc {EXPECTED_RUSTC}" not in output:
        raise RuntimeError(f"expected rustc {EXPECTED_RUSTC}, got {output!r}")
    require_tool("cargo")
    require_tool("rustfmt")
