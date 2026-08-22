# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Implemented communication primitives for bounded DDP training."""

from __future__ import annotations

from .loss_reduction import DDPReducer

__all__ = ["DDPReducer"]
