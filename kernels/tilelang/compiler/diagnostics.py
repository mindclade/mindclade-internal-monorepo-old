# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

"""Bounded diagnostics that keep private source out of public error strings."""

from __future__ import annotations

import hashlib
from dataclasses import dataclass


@dataclass(frozen=True, slots=True)
class CompilationDiagnostic:
    phase: str
    error_type: str
    message_digest: str

    @classmethod
    def from_exception(cls, phase: str, error: BaseException) -> CompilationDiagnostic:
        return cls(
            phase=phase,
            error_type=type(error).__name__,
            message_digest=hashlib.sha256(str(error).encode()).hexdigest(),
        )
