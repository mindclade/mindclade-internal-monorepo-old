# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

from kernels.ops.fused.reference import triangle_multiplication_reference

triangle_multiplication = triangle_multiplication_reference

__all__ = ["triangle_multiplication", "triangle_multiplication_reference"]
