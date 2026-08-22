# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Pinned TileLang and TVM-FFI provider contract."""

from __future__ import annotations

import re

TILELANG_VERSION = "0.1.13"
TVM_FFI_MINIMUM = "0.1.11"
TVM_FFI_MAXIMUM = "0.1.13"
TVM_FFI_RANGE = f">={TVM_FFI_MINIMUM},<{TVM_FFI_MAXIMUM}"
PROVIDER_SCHEMA_VERSION = 2

_RELEASE = re.compile(r"^(\d+)\.(\d+)\.(\d+)")


def _release(version: str) -> tuple[int, int, int]:
    match = _RELEASE.match(version)
    if match is None:
        raise ValueError(f"invalid release version {version!r}")
    return tuple(int(component) for component in match.groups())  # type: ignore[return-value]


def validate_runtime_versions(tilelang_version: str, tvm_ffi_version: str) -> None:
    """Reject a runtime outside the source-reviewed compiler compatibility window."""

    if tilelang_version != TILELANG_VERSION:
        raise ValueError(
            f"TileLang must be exactly {TILELANG_VERSION}; observed {tilelang_version or 'missing'}"
        )
    observed = _release(tvm_ffi_version)
    if not (_release(TVM_FFI_MINIMUM) <= observed < _release(TVM_FFI_MAXIMUM)):
        raise ValueError(
            f"apache-tvm-ffi must satisfy {TVM_FFI_RANGE}; observed {tvm_ffi_version or 'missing'}"
        )
