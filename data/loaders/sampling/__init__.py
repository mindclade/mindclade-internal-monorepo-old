# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Deterministic sampling policies."""

from .curriculum import eligible_indices
from .deterministic import stable_order
from .mixture import mixture_schedule
from .random import sample_indices
from .temperature import temperature_weights
from .weighted import weighted_indices

__all__ = [
    "eligible_indices",
    "mixture_schedule",
    "sample_indices",
    "stable_order",
    "temperature_weights",
    "weighted_indices",
]

"""Mindclade scaffold package for data/loaders/sampling."""
