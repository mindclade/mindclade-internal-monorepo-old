# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Version-pinned, normalized dataset mixture contract."""

from __future__ import annotations

import hashlib
import json
import math
from dataclasses import dataclass


@dataclass(frozen=True, slots=True, order=True)
class MixtureComponent:
    dataset_id: str
    version: str
    manifest_digest: str
    split: str
    weight: float

    def __post_init__(self) -> None:
        if not self.dataset_id or len(self.dataset_id) > 63:
            raise ValueError("mixture dataset id is invalid")
        if not self.version or len(self.version) > 64:
            raise ValueError("mixture dataset version is invalid")
        if (
            len(self.manifest_digest) != 71
            or not self.manifest_digest.startswith("sha256:")
            or any(character not in "0123456789abcdef" for character in self.manifest_digest[7:])
        ):
            raise ValueError("mixture manifest digest is invalid")
        if self.split not in {"train", "validation", "test", "holdout", "serving"}:
            raise ValueError("mixture split is invalid")
        if isinstance(self.weight, bool) or not math.isfinite(self.weight) or self.weight <= 0:
            raise ValueError("mixture weight must be finite and positive")


@dataclass(frozen=True, slots=True)
class DatasetMixture:
    name: str
    components: tuple[MixtureComponent, ...]
    seed: int

    def __post_init__(self) -> None:
        components = tuple(sorted(self.components))
        if not self.name or len(self.name) > 128:
            raise ValueError("mixture name is invalid")
        if not 1 <= len(components) <= 4096:
            raise ValueError("mixture requires 1..4096 components")
        identities = [(item.dataset_id, item.version, item.split) for item in components]
        if len(set(identities)) != len(identities):
            raise ValueError("mixture components must be unique")
        if isinstance(self.seed, bool) or not isinstance(self.seed, int) or self.seed < 0:
            raise ValueError("mixture seed must be a non-negative integer")
        total = sum(item.weight for item in components)
        if not math.isclose(total, 1.0, rel_tol=0.0, abs_tol=1e-12):
            raise ValueError("mixture weights must sum to one")
        object.__setattr__(self, "components", components)

    @property
    def digest(self) -> str:
        value = {
            "name": self.name,
            "seed": self.seed,
            "components": [
                {
                    "dataset_id": item.dataset_id,
                    "version": item.version,
                    "manifest_digest": item.manifest_digest,
                    "split": item.split,
                    "weight": item.weight,
                }
                for item in self.components
            ],
        }
        canonical = json.dumps(value, sort_keys=True, separators=(",", ":")).encode()
        return "sha256:" + hashlib.sha256(canonical).hexdigest()
