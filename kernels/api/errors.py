# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Stable, structured errors for kernel validation and dispatch."""

from __future__ import annotations

from enum import StrEnum
from typing import Any


class KernelErrorCode(StrEnum):
    INVALID_ARGUMENT = "invalid_argument"
    UNSUPPORTED_SIGNATURE = "unsupported_signature"
    PROVIDER_UNAVAILABLE = "provider_unavailable"
    UNQUALIFIED = "unqualified"
    REVOKED = "revoked"
    COMPILATION_FAILED = "compilation_failed"
    LAUNCH_FAILED = "launch_failed"


class KernelError(RuntimeError):
    """Base exception whose public message never contains compiler internals."""

    def __init__(
        self,
        code: KernelErrorCode,
        message: str,
        *,
        details: dict[str, Any] | None = None,
    ) -> None:
        super().__init__(message)
        self.code = code
        self.details = dict(details or {})


class KernelValidationError(KernelError):
    def __init__(self, message: str, *, details: dict[str, Any] | None = None) -> None:
        super().__init__(KernelErrorCode.INVALID_ARGUMENT, message, details=details)


class KernelUnavailableError(KernelError):
    """Expected inability to use an implementation; callers may explicitly fall back."""


class KernelCompilationError(KernelError):
    def __init__(self, message: str = "kernel compilation failed") -> None:
        super().__init__(KernelErrorCode.COMPILATION_FAILED, message)


class KernelLaunchError(KernelError):
    def __init__(self, message: str = "kernel launch failed") -> None:
        super().__init__(KernelErrorCode.LAUNCH_FAILED, message)
