#!/usr/bin/env python3
"""Print a compact, read-only PyTorch project and environment report as JSON."""

from __future__ import annotations

import argparse
import importlib.util
import json
import platform
import sys
from pathlib import Path
from typing import Any

CANDIDATES = (
    "AGENTS.md",
    "README.md",
    "pyproject.toml",
    "setup.py",
    "setup.cfg",
    "uv.lock",
    "poetry.lock",
    "pdm.lock",
    "Pipfile.lock",
    "requirements.txt",
    "environment.yml",
    "pytest.ini",
    "tox.ini",
    "noxfile.py",
    ".pre-commit-config.yaml",
)


def safe_call(fn: Any, default: Any = None) -> Any:
    try:
        return fn()
    except Exception as exc:  # diagnostics must continue
        return {"error": f"{type(exc).__name__}: {exc}", "default": default}


def torch_report() -> dict[str, Any]:
    if importlib.util.find_spec("torch") is None:
        return {"installed": False}

    import torch

    report: dict[str, Any] = {
        "installed": True,
        "version": torch.__version__,
        "debug_build": safe_call(torch.version.debug, None) if callable(getattr(torch.version, "debug", None)) else getattr(torch.version, "debug", None),
        "cuda_build": getattr(torch.version, "cuda", None),
        "hip_build": getattr(torch.version, "hip", None),
        "cuda_available": safe_call(torch.cuda.is_available, False),
    }

    mps = getattr(getattr(torch, "backends", None), "mps", None)
    if mps is not None:
        report["mps_available"] = safe_call(mps.is_available, False)
        report["mps_built"] = safe_call(mps.is_built, False)

    xpu = getattr(torch, "xpu", None)
    if xpu is not None and hasattr(xpu, "is_available"):
        report["xpu_available"] = safe_call(xpu.is_available, False)

    if report.get("cuda_available") is True:
        report["cuda_device_count"] = safe_call(torch.cuda.device_count, 0)
        report["cuda_devices"] = [
            safe_call(lambda i=i: torch.cuda.get_device_name(i), "unknown")
            for i in range(int(report.get("cuda_device_count", 0)))
        ]
    return report


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--root", default=".", help="Project root to inspect")
    args = parser.parse_args()

    root = Path(args.root).expanduser().resolve()
    if not root.is_dir():
        parser.error(f"not a directory: {root}")

    report = {
        "root": str(root),
        "python": {
            "version": sys.version.split()[0],
            "implementation": platform.python_implementation(),
            "executable": sys.executable,
        },
        "platform": {
            "system": platform.system(),
            "release": platform.release(),
            "machine": platform.machine(),
        },
        "files": [name for name in CANDIDATES if (root / name).exists()],
        "torch": torch_report(),
    }
    print(json.dumps(report, indent=2, sort_keys=True))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
