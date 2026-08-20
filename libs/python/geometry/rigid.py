# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Immutable three-dimensional rigid transforms."""

from __future__ import annotations

from dataclasses import dataclass

import numpy as np

from libs.python.errors import InvalidArgument

from .invariants import FloatArray, as_points, as_rotation_matrix, as_vector3, readonly_copy


@dataclass(frozen=True, slots=True, eq=False)
class RigidTransform:
    """A right-handed transform applying ``rotation @ point + translation``."""

    rotation: FloatArray
    translation: FloatArray

    def __post_init__(self) -> None:
        object.__setattr__(self, "rotation", as_rotation_matrix(self.rotation))
        object.__setattr__(self, "translation", as_vector3(self.translation, name="translation"))

    @classmethod
    def identity(cls) -> RigidTransform:
        return cls(np.eye(3, dtype=np.float64), np.zeros(3, dtype=np.float64))

    def apply(self, points: object) -> FloatArray:
        values = as_points(points)
        transformed = values @ self.rotation.T + self.translation
        return readonly_copy(transformed)

    def inverse(self) -> RigidTransform:
        rotation = self.rotation.T
        translation = -(rotation @ self.translation)
        return RigidTransform(rotation, translation)

    def compose(self, other: object) -> RigidTransform:
        """Return ``self(other(points))``."""
        if not isinstance(other, RigidTransform):
            raise InvalidArgument(
                "rigid composition requires a RigidTransform",
                reason="geometry_transform_type",
            )
        return RigidTransform(
            self.rotation @ other.rotation,
            self.rotation @ other.translation + self.translation,
        )

    def almost_equals(self, other: object, *, atol: float = 1e-7) -> bool:
        return isinstance(other, RigidTransform) and bool(
            np.allclose(self.rotation, other.rotation, rtol=0.0, atol=atol)
            and np.allclose(self.translation, other.translation, rtol=0.0, atol=atol)
        )
