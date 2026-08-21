# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

import pytest

from kernels.tilelang.compiler.ir import KernelSourceArtifact
from kernels.tilelang.compiler.lowering import require_codegen_tokens
from kernels.tilelang.compiler.pipeline import PipelineSpec, validate_stage_order
from kernels.tilelang.compiler.wgmma import WGMMAInstruction
from kernels.tilelang.targets import CDNA3_GFX942, HOPPER


def test_pipeline_resource_model_and_annotations() -> None:
    pipeline = PipelineSpec(3, 32_768, 16_384)
    assert pipeline.shared_memory_bytes == 114_688
    assert pipeline.rejection_reason(HOPPER) is None
    assert pipeline.rejection_reason(CDNA3_GFX942) == "shared_memory_limit"
    validate_stage_order((0, 0, 1), (0, 1, 2))
    with pytest.raises(ValueError, match="permutation"):
        validate_stage_order((0, 1), (0, 0))


def test_codegen_identity_and_instruction_contract() -> None:
    artifact = KernelSourceArtifact("wgmma.mma_async; cp.async;", "sm_90", "0.1.13")
    require_codegen_tokens(artifact, required=("wgmma", "cp.async"), forbidden=("atomicAdd",))
    assert len(artifact.identity_digest) == 64
    with pytest.raises(ValueError, match="generated source contract"):
        require_codegen_tokens(artifact, required=("tcgen05",))

    instruction = WGMMAInstruction(64, 64, 16, "bfloat16")
    instruction.validate_target(HOPPER)
    with pytest.raises(ValueError, match="WGMMA"):
        instruction.validate_target(CDNA3_GFX942)
