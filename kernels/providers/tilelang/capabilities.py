# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Runtime capability detection mapped to the reviewed target catalog."""

from __future__ import annotations

from dataclasses import replace
from typing import Any

from kernels.api.capabilities import DeviceCapabilities, digest_runtime_environment
from kernels.api.errors import KernelErrorCode, KernelUnavailableError
from kernels.tilelang.targets import resolve_target


def detect_capabilities(device: Any | None = None) -> DeviceCapabilities:
    try:
        import torch
    except ImportError as exc:  # pragma: no cover - root environment pins torch
        raise KernelUnavailableError(
            KernelErrorCode.PROVIDER_UNAVAILABLE, "PyTorch device discovery is unavailable"
        ) from exc

    selected = torch.device("cuda" if device is None else device)
    if selected.type != "cuda" or not torch.cuda.is_available():
        raise KernelUnavailableError(
            KernelErrorCode.PROVIDER_UNAVAILABLE,
            "TileLang accelerator dispatch requires a CUDA or ROCm device",
        )
    properties = torch.cuda.get_device_properties(selected)
    if torch.version.hip:
        architecture = getattr(properties, "gcnArchName", "").split(":", maxsplit=1)[0]
        target = resolve_target("hip", architecture)
    else:
        architecture = f"sm_{properties.major}{properties.minor}"
        target = resolve_target("cuda", architecture)
    if target is None:
        raise KernelUnavailableError(
            KernelErrorCode.UNSUPPORTED_SIGNATURE,
            "the detected accelerator architecture is not in the reviewed target catalog",
            details={"architecture": architecture},
        )
    runtime = torch.version.hip or torch.version.cuda or "unknown"
    runtime_identity = digest_runtime_environment(
        {
            "architecture": architecture,
            "device_name": properties.name,
            "device_total_memory": properties.total_memory,
            "pytorch": torch.__version__,
            "runtime": runtime,
            "target": target.kind,
        }
    )
    return replace(target.capabilities, runtime_environment_digest=runtime_identity)
