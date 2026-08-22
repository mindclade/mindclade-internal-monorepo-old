# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Stable provider-neutral kernel contracts."""

from kernels.api.capabilities import (
    PORTABLE_CPU,
    DeviceCapabilities,
    RuntimeCompatibility,
    digest_runtime_environment,
)
from kernels.api.custom_ops import CustomOpDefinition
from kernels.api.errors import (
    KernelCompilationError,
    KernelError,
    KernelErrorCode,
    KernelLaunchError,
    KernelUnavailableError,
    KernelValidationError,
)
from kernels.api.fake_tensor import output_like
from kernels.api.specs import (
    ExecutionMode,
    ImplementationIdentity,
    KernelRequest,
    Provider,
    TensorLayout,
    TensorSpec,
)

__all__ = [
    "PORTABLE_CPU",
    "CustomOpDefinition",
    "DeviceCapabilities",
    "ExecutionMode",
    "ImplementationIdentity",
    "KernelCompilationError",
    "KernelError",
    "KernelErrorCode",
    "KernelLaunchError",
    "KernelRequest",
    "KernelUnavailableError",
    "KernelValidationError",
    "Provider",
    "RuntimeCompatibility",
    "TensorLayout",
    "TensorSpec",
    "digest_runtime_environment",
    "output_like",
]
