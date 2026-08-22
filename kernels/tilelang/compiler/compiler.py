# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Narrow TileLang compile and generated-source capture boundary."""

from __future__ import annotations

from dataclasses import dataclass
from typing import Any

from kernels.api.errors import KernelCompilationError
from kernels.tilelang.compiler.diagnostics import CompilationDiagnostic
from kernels.tilelang.compiler.ir import KernelSourceArtifact


@dataclass(frozen=True, slots=True)
class CompilationResult:
    kernel: Any
    source: KernelSourceArtifact


@dataclass(frozen=True, slots=True)
class CompilationFailure:
    diagnostic: CompilationDiagnostic


def compile_and_capture(
    kernel: Any,
    *,
    target: str,
    compiler_version: str,
    compile_constants: dict[str, object],
) -> CompilationResult:
    try:
        compiled = kernel.compile(**compile_constants)
        source = compiled.get_kernel_source()
    except Exception as exc:
        diagnostic = CompilationDiagnostic.from_exception("compile", exc)
        error = KernelCompilationError()
        error.details["diagnostic"] = diagnostic
        raise error from exc
    return CompilationResult(
        compiled,
        KernelSourceArtifact(source=source, target=target, compiler_version=compiler_version),
    )
