# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Model contract package with target-card and scientific-intake boundaries."""

from .scientific_intake import (
    ApprovalAttestation,
    ApprovalRole,
    IntakePurpose,
    ScientificModelIntake,
)
from .target_card import (
    MODEL_TARGET_CARD_V1,
    MODEL_TARGET_CARD_V2,
    ActivationState,
    MetricDirection,
    MetricGate,
    ModelFamily,
    ModelTargetCard,
)

__all__ = [
    "MODEL_TARGET_CARD_V1",
    "MODEL_TARGET_CARD_V2",
    "ActivationState",
    "ApprovalAttestation",
    "ApprovalRole",
    "IntakePurpose",
    "MetricDirection",
    "MetricGate",
    "ModelFamily",
    "ModelTargetCard",
    "ScientificModelIntake",
]
