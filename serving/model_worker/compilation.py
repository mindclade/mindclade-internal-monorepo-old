# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Immutable compilation cache identity."""

from dataclasses import dataclass


@dataclass(frozen=True, order=True, slots=True)
class CompilationKey:
    model_digest: str
    runtime_digest: str
    hardware_class: str
    precision: str
    shape_bucket: str

    def __post_init__(self) -> None:
        for digest in (self.model_digest, self.runtime_digest):
            if not digest.startswith("sha256:") or len(digest) != 71:
                raise ValueError("compilation digest is invalid")
        for value in (self.hardware_class, self.precision, self.shape_bucket):
            if not value or len(value) > 128:
                raise ValueError("compilation compatibility field is invalid")
