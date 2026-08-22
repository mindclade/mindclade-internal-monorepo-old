# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Runtime capability detection mapped to the reviewed target catalog."""

from __future__ import annotations

import os
import platform
import re
from dataclasses import replace
from importlib import import_module
from typing import Any

from kernels.api.capabilities import DeviceCapabilities, RuntimeCompatibility
from kernels.api.errors import KernelErrorCode, KernelUnavailableError
from kernels.providers.tilelang.attention.attention import _tilelang
from kernels.tilelang.targets import resolve_target

_OCI_DIGEST = re.compile(r"^sha256:[0-9a-f]{64}$")


def _required_environment(name: str, *, oci_digest: bool = False) -> str:
    value = os.environ.get(name, "").strip()
    if not value or (oci_digest and _OCI_DIGEST.fullmatch(value) is None):
        requirement = "a sha256:<64 lowercase hexadecimal> digest" if oci_digest else "non-empty"
        raise KernelUnavailableError(
            KernelErrorCode.PROVIDER_UNAVAILABLE,
            f"{name} must be {requirement} for qualification-bound dispatch",
        )
    return value


def _driver_version(torch: Any) -> str:
    override = os.environ.get("MINDCLADE_ACCELERATOR_DRIVER_VERSION", "").strip()
    if override:
        return override
    getter = getattr(torch._C, "_cuda_getDriverVersion", None)
    if getter is None:
        return _required_environment("MINDCLADE_ACCELERATOR_DRIVER_VERSION")
    try:
        value = str(getter()).strip()
    except RuntimeError:
        return _required_environment("MINDCLADE_ACCELERATOR_DRIVER_VERSION")
    if not value or value == "0":
        return _required_environment("MINDCLADE_ACCELERATOR_DRIVER_VERSION")
    return value


def runtime_compatibility(device: Any | None = None) -> RuntimeCompatibility:
    """Capture all mutable runtime fields used by exact qualification dispatch."""

    try:
        import torch
    except ImportError as exc:  # pragma: no cover - accelerator image contract
        raise KernelUnavailableError(
            KernelErrorCode.PROVIDER_UNAVAILABLE,
            "PyTorch and apache-tvm-ffi device discovery are unavailable",
        ) from exc
    try:
        tvm_ffi = import_module("tvm_ffi")
    except ImportError as exc:
        raise KernelUnavailableError(
            KernelErrorCode.PROVIDER_UNAVAILABLE,
            "apache-tvm-ffi device discovery is unavailable",
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

    tilelang, _ = _tilelang()
    runtime = torch.version.hip or torch.version.cuda
    if not isinstance(runtime, str) or not runtime.strip():
        raise KernelUnavailableError(
            KernelErrorCode.PROVIDER_UNAVAILABLE,
            "accelerator runtime version is unavailable for qualification-bound dispatch",
        )
    return RuntimeCompatibility(
        target=target.kind,
        architecture=architecture,
        device_name=str(properties.name),
        device_memory_bytes=int(properties.total_memory),
        driver_version=_driver_version(torch),
        runtime_version=str(runtime),
        pytorch_version=str(torch.__version__),
        tilelang_version=str(tilelang.__version__),
        tvm_ffi_version=str(getattr(tvm_ffi, "__version__", "")),
        compiler_version=_required_environment("MINDCLADE_ACCELERATOR_COMPILER_VERSION"),
        os_release=platform.platform(),
        runtime_image_digest=_required_environment(
            "MINDCLADE_RUNTIME_IMAGE_DIGEST",
            oci_digest=True,
        ),
        partition_profile=os.environ.get(
            "MINDCLADE_ACCELERATOR_PARTITION_PROFILE",
            "none",
        ),
    )


def detect_capabilities(device: Any | None = None) -> DeviceCapabilities:
    compatibility = runtime_compatibility(device)
    target = resolve_target(compatibility.target, compatibility.architecture)
    if target is None:  # defensive: runtime_compatibility already resolved it
        raise KernelUnavailableError(
            KernelErrorCode.UNSUPPORTED_SIGNATURE,
            "the runtime target disappeared from the reviewed target catalog",
        )
    return replace(target.capabilities, runtime_environment_digest=compatibility.digest)
