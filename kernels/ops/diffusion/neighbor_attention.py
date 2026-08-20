# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

from kernels.ops.diffusion.reference import neighbor_attention_reference

neighbor_attention = neighbor_attention_reference

__all__ = ["neighbor_attention", "neighbor_attention_reference"]
