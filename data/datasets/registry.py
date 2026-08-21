# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Provider-neutral validation of the Go-owned publication state machine."""

from __future__ import annotations

from enum import StrEnum


class PublicationState(StrEnum):
    DRAFT = "draft"
    VALIDATING = "validating"
    QUALIFIED = "qualified"
    PUBLISHED = "published"
    REJECTED = "rejected"
    DEPRECATED = "deprecated"
    RETIRED = "retired"


_TRANSITIONS = {
    PublicationState.DRAFT: {PublicationState.VALIDATING},
    PublicationState.VALIDATING: {PublicationState.QUALIFIED, PublicationState.REJECTED},
    PublicationState.QUALIFIED: {PublicationState.PUBLISHED},
    PublicationState.PUBLISHED: {PublicationState.DEPRECATED},
    PublicationState.DEPRECATED: {PublicationState.RETIRED},
    PublicationState.REJECTED: set(),
    PublicationState.RETIRED: set(),
}


def validate_transition(current: PublicationState, target: PublicationState) -> None:
    if not isinstance(current, PublicationState) or not isinstance(target, PublicationState):
        raise TypeError("publication states must be PublicationState values")
    if target not in _TRANSITIONS[current]:
        raise ValueError(f"invalid dataset publication transition: {current} -> {target}")
