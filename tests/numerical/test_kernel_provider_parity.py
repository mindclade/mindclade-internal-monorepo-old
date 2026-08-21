# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Cross-family CPU-safe numerical smoke tests; accelerator evidence is separate."""

import torch

from kernels.defaults import default_registry
from kernels.manifest import QualificationManifest
from kernels.ops.attention import attention_reference
from kernels.ops.diffusion import modulated_residual_reference
from kernels.ops.fp8 import scaled_gemm_reference
from kernels.ops.fused import swiglu_reference


def test_reference_outputs_are_finite_for_adversarial_magnitude_buckets() -> None:
    for magnitude in (1e-4, 1.0, 1e2):
        q = torch.randn(1, 1, 5, 8) * magnitude
        output = attention_reference(q, q, q)
        assert torch.isfinite(output).all()

        a = torch.randn(3, 8) * magnitude
        b = torch.randn(8, 4) / max(magnitude, 1e-8)
        gemm = scaled_gemm_reference(a, b, torch.ones(1), torch.ones(1), output_dtype=torch.float32)
        assert torch.isfinite(gemm).all()


def test_default_registry_is_fail_closed_without_promoted_manifest() -> None:
    registry = default_registry()
    assert registry.reference("attention.sdpa").invoke is attention_reference
    assert registry.reference("fused.swiglu").invoke is swiglu_reference
    assert registry.reference("diffusion.modulated_residual").invoke is modulated_residual_reference
    assert QualificationManifest().records == ()
