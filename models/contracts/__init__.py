# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Model contract package with a shared target-card core."""

from .target_card import ActivationState, MetricDirection, MetricGate, ModelFamily, ModelTargetCard

__all__ = [
    "ActivationState",
    "MetricDirection",
    "MetricGate",
    "ModelFamily",
    "ModelTargetCard",
]
