# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

import hashlib
import json
import sys
from pathlib import Path

import pytest

from kernels.manifest import QualificationManifest
from kernels.tilelang.autotune import Candidate, TuningBudget
from kernels.tilelang.compiler.ir import MAXIMUM_GENERATED_SOURCE_BYTES
from tools.qualification.kernels.autotune_tilelang import (
    MAXIMUM_SPEC_BYTES,
    MAXIMUM_WORKER_STREAM_BYTES,
    _parse_json_object,
    _read_bounded_text,
    candidates_from_spec,
    execute_worker,
)
from tools.qualification.kernels.autotune_tilelang import (
    _atomic_write as write_tuning_results,
)
from tools.qualification.kernels.inspect_tilelang_ir import (
    _read_bounded_source,
    inspect_source,
)
from tools.qualification.kernels.qualify_tilelang import _atomic_write
from tools.qualification.kernels.verify_tilelang_manifest import verify_manifest


def _digest(value: str) -> str:
    return hashlib.sha256(value.encode()).hexdigest()


def test_ir_inspection_is_content_addressed_and_fail_closed() -> None:
    result = inspect_source(
        "wgmma.mma_async; cp.async;",
        target="sm_90",
        compiler_version="0.1.13",
        required=("wgmma", "cp.async"),
        forbidden=("atomicAdd",),
    )
    assert result["token_contract_satisfied"] is True
    assert "verified" not in result
    identity_digest = result["identity_digest"]
    assert isinstance(identity_digest, str)
    assert len(identity_digest) == 64
    with pytest.raises(ValueError, match="generated source contract"):
        inspect_source(
            "ordinary mma",
            target="sm_90",
            compiler_version="0.1.13",
            required=("wgmma",),
            forbidden=(),
        )


def test_ir_inspection_excludes_comments_from_token_evidence() -> None:
    with pytest.raises(ValueError, match=r"missing=.*wgmma"):
        inspect_source(
            "// wgmma.mma_async\\\ncp.async\n/* wgmma */",
            target="sm_90",
            compiler_version="0.1.13",
            required=("wgmma",),
            forbidden=(),
        )

    result = inspect_source(
        "wgmma.mma_async; // atomicAdd\n/* atomicAdd */",
        target="sm_90",
        compiler_version="0.1.13",
        required=("wgmma",),
        forbidden=("atomicAdd",),
    )
    assert result["token_contract_satisfied"] is True


def test_generated_source_file_read_is_size_bounded(tmp_path: Path) -> None:
    source = tmp_path / "generated.cu"
    source.write_bytes(b"x" * (MAXIMUM_GENERATED_SOURCE_BYTES + 1))
    with pytest.raises(ValueError, match="inspection limit"):
        _read_bounded_source(source)


def test_tuning_spec_is_bounded_to_unique_content_addressed_candidates() -> None:
    candidates = candidates_from_spec(
        {
            "source_digest": _digest("source"),
            "environment_digest": _digest("environment"),
            "candidates": [
                {"block_m": 64, "stages": 2},
                {"block_m": 128, "stages": 3},
            ],
        }
    )
    assert len(candidates) == 2
    assert len({candidate.digest for candidate in candidates}) == 2
    with pytest.raises(ValueError, match="unique"):
        candidates_from_spec(
            {
                "source_digest": _digest("source"),
                "environment_digest": _digest("environment"),
                "candidates": [{"block_m": 64}, {"block_m": 64}],
            }
        )
    with pytest.raises(ValueError, match="active budget"):
        candidates_from_spec(
            {
                "source_digest": _digest("source"),
                "environment_digest": _digest("environment"),
                "candidates": [{"block_m": 64}, {"block_m": 128}],
            },
            maximum_candidates=1,
        )


@pytest.mark.parametrize(
    "mutation, error",
    [
        ({"source_digest": 1}, "source_digest"),
        ({"environment_digest": "A" * 64}, "environment_digest"),
        ({"unexpected": True}, "schema mismatch"),
        ({"candidates": [["not", "an", "object"]]}, "JSON object"),
        ({"candidates": [{"stages": float("nan")}]}, "finite"),
    ],
)
def test_tuning_spec_rejects_type_digest_schema_and_scalar_spoofs(
    mutation: dict[str, object], error: str
) -> None:
    payload: dict[str, object] = {
        "source_digest": _digest("source"),
        "environment_digest": _digest("environment"),
        "candidates": [{"block_m": 64}],
    }
    payload.update(mutation)
    with pytest.raises((TypeError, ValueError), match=error):
        candidates_from_spec(payload)


def test_tuning_spec_file_and_json_shape_are_bounded_and_strict(tmp_path: Path) -> None:
    spec = tmp_path / "spec.json"
    spec.write_bytes(b"x" * (MAXIMUM_SPEC_BYTES + 1))
    with pytest.raises(ValueError, match="byte limit"):
        _read_bounded_text(
            spec,
            maximum_bytes=MAXIMUM_SPEC_BYTES,
            description="tuning specification",
        )
    with pytest.raises(ValueError, match="duplicate JSON object key"):
        _parse_json_object('{"candidates":[],"candidates":[]}', description="spec")
    with pytest.raises(ValueError, match="non-finite JSON number"):
        _parse_json_object('{"value":NaN}', description="spec")

    target = tmp_path / "target.json"
    target.write_text("{}")
    symbolic = tmp_path / "symbolic.json"
    symbolic.symlink_to(target)
    with pytest.raises(ValueError, match="symbolic-link"):
        _read_bounded_text(
            symbolic,
            maximum_bytes=MAXIMUM_SPEC_BYTES,
            description="tuning specification",
        )


def _candidate() -> Candidate:
    return Candidate(
        (("block_m", 64),),
        _digest("source"),
        _digest("environment"),
    )


def _worker_for_payload(payload: str, *, stderr_bytes: int = 0) -> tuple[str, ...]:
    program = f"import sys;sys.stdout.write({payload!r});sys.stderr.write('x'*{stderr_bytes})"
    return (sys.executable, "-c", program)


def _valid_worker_response(*, parity_passed: object = True) -> dict[str, object]:
    return {
        "generated_source_digest": _digest("generated"),
        "parity_passed": parity_passed,
        "samples_ms": [1.0, 1.1, 0.9, 1.0, 1.0],
    }


def test_worker_response_accepts_only_the_exact_typed_schema() -> None:
    response = _valid_worker_response()
    assert execute_worker(
        _candidate(),
        TuningBudget(benchmark_samples=5),
        worker=_worker_for_payload(json.dumps(response)),
    ) == (True, _digest("generated"), (1.0, 1.1, 0.9, 1.0, 1.0))

    adversarial_responses = [
        _valid_worker_response(parity_passed="false"),
        {**_valid_worker_response(), "generated_source_digest": "A" * 64},
        {**_valid_worker_response(), "samples_ms": [1.0, 1.0, 1.0, 1.0]},
        {**_valid_worker_response(), "samples_ms": [1.0, 1.0, 1.0, 1.0, 0.0]},
        {**_valid_worker_response(), "samples_ms": [1.0, 1.0, 1.0, 1.0, True]},
        {**_valid_worker_response(), "samples_ms": [1.0, 1.0, 1.0, 1.0, 30_001.0]},
        {**_valid_worker_response(), "unexpected": True},
    ]
    for adversarial in adversarial_responses:
        with pytest.raises((TypeError, ValueError)):
            execute_worker(
                _candidate(),
                TuningBudget(benchmark_samples=5),
                worker=_worker_for_payload(json.dumps(adversarial)),
            )

    failed_with_samples = _valid_worker_response(parity_passed=False)
    with pytest.raises(ValueError, match="failing tuning workers"):
        execute_worker(
            _candidate(),
            TuningBudget(benchmark_samples=5),
            worker=_worker_for_payload(json.dumps(failed_with_samples)),
        )


def test_worker_response_rejects_nonfinite_json_and_oversized_streams() -> None:
    nonfinite = json.dumps(_valid_worker_response()).replace("1.1", "NaN", 1)
    with pytest.raises(ValueError, match="non-finite JSON number"):
        execute_worker(
            _candidate(),
            TuningBudget(benchmark_samples=5),
            worker=_worker_for_payload(nonfinite),
        )

    with pytest.raises(ValueError, match="worker stdout"):
        execute_worker(
            _candidate(),
            TuningBudget(benchmark_samples=5),
            worker=(
                sys.executable,
                "-c",
                f"import sys;sys.stdout.write('x'*{MAXIMUM_WORKER_STREAM_BYTES + 1})",
            ),
        )

    with pytest.raises(ValueError, match="worker stderr"):
        execute_worker(
            _candidate(),
            TuningBudget(benchmark_samples=5),
            worker=_worker_for_payload(
                json.dumps(_valid_worker_response()),
                stderr_bytes=MAXIMUM_WORKER_STREAM_BYTES + 1,
            ),
        )


def test_manifest_verifier_rejects_empty_production_state() -> None:
    manifest = QualificationManifest()
    with pytest.raises(ValueError, match="empty"):
        verify_manifest(manifest, expected_manifest_digest=manifest.digest)
    result = verify_manifest(
        manifest,
        expected_manifest_digest=manifest.digest,
        allow_empty=True,
    )
    assert result["structurally_valid"] is True
    assert result["trusted_identity_matched"] is True
    with pytest.raises(ValueError, match="trusted content identity"):
        verify_manifest(
            manifest,
            expected_manifest_digest=_digest("older-manifest"),
            allow_empty=True,
        )
    with pytest.raises(ValueError, match="expected_manifest_digest"):
        verify_manifest(manifest, allow_empty=True)


def test_candidate_manifest_publication_is_atomic_and_non_overwriting(tmp_path: Path) -> None:
    output = tmp_path / "candidate.json"
    _atomic_write(output, "first")
    assert output.read_text() == "first"
    with pytest.raises(ValueError, match="overwrite"):
        _atomic_write(output, "second")
    assert output.read_text() == "first"
    assert not tuple(tmp_path.glob(".candidate.json.*"))


def test_tuning_result_publication_is_atomic_and_non_overwriting(tmp_path: Path) -> None:
    output = tmp_path / "results.json"
    write_tuning_results(output, "first")
    assert output.read_text() == "first"
    with pytest.raises(ValueError, match="overwrite"):
        write_tuning_results(output, "second")
    assert output.read_text() == "first"
    assert not tuple(tmp_path.glob(".results.json.*"))
