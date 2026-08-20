# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

from kernels.providers.tilelang.fused.schedules import ElementwiseSchedule, TriangleSchedule

BASELINE_ELEMENTWISE = ElementwiseSchedule()
BASELINE_TRIANGLE = TriangleSchedule()
