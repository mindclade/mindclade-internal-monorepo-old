# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Location-free multimodal input descriptors."""

from dataclasses import dataclass


@dataclass(frozen=True, slots=True)
class ModalityInput:
    modality: str
    artifact_digest: str
    units: int

    def __post_init__(self) -> None:
        if not self.modality or len(self.modality) > 128:
            raise ValueError("modality name is invalid")
        if not self.artifact_digest.startswith("sha256:") or len(self.artifact_digest) != 71:
            raise ValueError("modality artifact digest is invalid")
        if isinstance(self.units, bool) or not 1 <= self.units <= 2**31:
            raise ValueError("modality units are outside bounds")


def validate_modalities(values: tuple[ModalityInput, ...], *, maximum_modalities: int = 32) -> None:
    if not values or len(values) > maximum_modalities:
        raise ValueError("modality count is outside bounds")
    if len({value.modality for value in values}) != len(values):
        raise ValueError("modalities must be unique")
