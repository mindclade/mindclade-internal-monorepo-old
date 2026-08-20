# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

from kernels.ops.fused.reference import swiglu_reference

swiglu = swiglu_reference

__all__ = ["swiglu", "swiglu_reference"]
