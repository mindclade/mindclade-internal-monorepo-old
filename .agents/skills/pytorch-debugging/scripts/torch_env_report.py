#!/usr/bin/env python3
"""Print a read-only JSON report useful for PyTorch bug reports."""

from __future__ import annotations

import json
import os
import platform
import sys
from collections.abc import Callable
from typing import Any


def attempt(fn: Callable[[], Any]) -> Any:
    try:
        return fn()
    except Exception as exc:  # diagnostics should not abort on one backend
        return {"error": f"{type(exc).__name__}: {exc}"}


def main() -> int:
    try:
        import torch
    except ImportError as exc:
        print(json.dumps({"python": sys.version, "torch_import_error": str(exc)}, indent=2))
        return 1

    report: dict[str, Any] = {
        "python": {
            "version": sys.version,
            "executable": sys.executable,
            "implementation": platform.python_implementation(),
        },
        "platform": {
            "system": platform.system(),
            "release": platform.release(),
            "version": platform.version(),
            "machine": platform.machine(),
        },
        "torch": {
            "version": torch.__version__,
            "git_version": getattr(torch.version, "git_version", None),
            "cuda_build": getattr(torch.version, "cuda", None),
            "hip_build": getattr(torch.version, "hip", None),
            "debug_build": getattr(torch.version, "debug", None),
            "num_threads": attempt(torch.get_num_threads),
            "num_interop_threads": attempt(torch.get_num_interop_threads),
        },
        "environment": {
            key: os.environ.get(key)
            for key in (
                "CUDA_VISIBLE_DEVICES",
                "HIP_VISIBLE_DEVICES",
                "PYTORCH_ENABLE_MPS_FALLBACK",
                "TORCH_LOGS",
                "TORCH_SHOW_CPP_STACKTRACES",
                "OMP_NUM_THREADS",
                "MKL_NUM_THREADS",
            )
            if key in os.environ
        },
    }

    cuda_available = attempt(torch.cuda.is_available)
    report["cuda"] = {"available": cuda_available}
    if cuda_available is True:
        count = attempt(torch.cuda.device_count)
        report["cuda"]["device_count"] = count
        if isinstance(count, int):
            report["cuda"]["devices"] = [
                {
                    "index": index,
                    "name": attempt(lambda index=index: torch.cuda.get_device_name(index)),
                    "capability": attempt(lambda index=index: torch.cuda.get_device_capability(index)),
                }
                for index in range(count)
            ]
        report["cuda"]["cudnn_version"] = attempt(torch.backends.cudnn.version)

    mps = getattr(getattr(torch, "backends", None), "mps", None)
    if mps is not None:
        report["mps"] = {
            "built": attempt(mps.is_built),
            "available": attempt(mps.is_available),
        }

    xpu = getattr(torch, "xpu", None)
    if xpu is not None and hasattr(xpu, "is_available"):
        report["xpu"] = {"available": attempt(xpu.is_available)}

    distributed = getattr(torch, "distributed", None)
    if distributed is not None:
        report["distributed"] = {
            "available": attempt(distributed.is_available),
            "gloo": attempt(distributed.is_gloo_available) if hasattr(distributed, "is_gloo_available") else None,
            "nccl": attempt(distributed.is_nccl_available) if hasattr(distributed, "is_nccl_available") else None,
            "mpi": attempt(distributed.is_mpi_available) if hasattr(distributed, "is_mpi_available") else None,
        }

    print(json.dumps(report, indent=2, sort_keys=True))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
