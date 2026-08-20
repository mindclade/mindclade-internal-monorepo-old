# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

from enum import StrEnum


class KernelMaturity(StrEnum):
    SOURCE = "source"
    LOCALLY_TESTED = "locally_tested"
    HARDWARE_QUALIFIED = "hardware_qualified"
    PROMOTED = "promoted"
    REVOKED = "revoked"
