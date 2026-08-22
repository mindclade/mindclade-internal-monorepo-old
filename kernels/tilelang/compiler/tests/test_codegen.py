# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

import pytest

from kernels.tilelang.compiler.ir import (
    MAXIMUM_GENERATED_SOURCE_BYTES,
    KernelSourceArtifact,
)
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


def test_codegen_token_contract_cannot_be_satisfied_by_comments_or_identifiers() -> None:
    comment_spoof = KernelSourceArtifact(
        "// wgmma.mma_async\\\ncp.async\n/* wgmma */\nmy_wgmma_helper();",
        "sm_90",
        "0.1.13",
    )
    with pytest.raises(ValueError, match=r"missing=.*wgmma"):
        require_codegen_tokens(comment_spoof, required=("wgmma",))

    spliced_comment_delimiter = KernelSourceArtifact(
        "/\\\n/ wgmma.mma_async\nordinary();",
        "sm_90",
        "0.1.13",
    )
    with pytest.raises(ValueError, match=r"missing=.*wgmma"):
        require_codegen_tokens(spliced_comment_delimiter, required=("wgmma",))

    forbidden_in_comments = KernelSourceArtifact(
        "wgmma.mma_async; // atomicAdd\n/* atomicAdd */",
        "sm_90",
        "0.1.13",
    )
    require_codegen_tokens(
        forbidden_in_comments,
        required=("wgmma",),
        forbidden=("atomicAdd",),
    )

    comment_marker_in_literal = KernelSourceArtifact(
        'const char *url = "https://example.invalid"; wgmma; atomicAdd(value);',
        "sm_90",
        "0.1.13",
    )
    with pytest.raises(ValueError, match=r"forbidden_present=.*atomicAdd"):
        require_codegen_tokens(
            comment_marker_in_literal,
            required=("wgmma",),
            forbidden=("atomicAdd",),
        )

    raw_literal = KernelSourceArtifact(
        'auto text = R"tag("https://example.invalid")tag"; atomicAdd(value); wgmma;',
        "sm_90",
        "0.1.13",
    )
    with pytest.raises(ValueError, match=r"forbidden_present=.*atomicAdd"):
        require_codegen_tokens(
            raw_literal,
            required=("wgmma",),
            forbidden=("atomicAdd",),
        )


def test_codegen_artifact_and_token_contracts_are_bounded() -> None:
    with pytest.raises(ValueError, match="inspection limit"):
        KernelSourceArtifact(
            "x" * (MAXIMUM_GENERATED_SOURCE_BYTES + 1),
            "sm_90",
            "0.1.13",
        )

    artifact = KernelSourceArtifact("wgmma;", "sm_90", "0.1.13")
    with pytest.raises(ValueError, match="unique"):
        require_codegen_tokens(artifact, required=("wgmma", "wgmma"))
    with pytest.raises(ValueError, match="both required and forbidden"):
        require_codegen_tokens(
            artifact,
            required=("wgmma",),
            forbidden=("wgmma",),
        )
