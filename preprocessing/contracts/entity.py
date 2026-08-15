# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Canonical scientific entity identity used as preprocessing cache input."""

from __future__ import annotations

import hashlib
from dataclasses import dataclass


class EntityType:
    PROTEIN = "protein"
    RNA = "rna"
    DNA = "dna"
    LIGAND = "ligand"


@dataclass(frozen=True)
class Entity:
    entity_type: str
    canonical: str

    def __post_init__(self):
        if self.entity_type not in {
            EntityType.PROTEIN,
            EntityType.RNA,
            EntityType.DNA,
            EntityType.LIGAND,
        }:
            raise ValueError("unsupported entity type")
        if not self.canonical.strip():
            raise ValueError("canonical entity representation is empty")

    @property
    def digest(self) -> str:
        payload = f"{self.entity_type}\0{self.canonical}".encode()
        return "sha256:" + hashlib.sha256(payload).hexdigest()
