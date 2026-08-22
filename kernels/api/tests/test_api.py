# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

import hashlib
from dataclasses import replace
from typing import Any

import pytest
import torch
from torch._subclasses.fake_tensor import FakeTensorMode

import kernels
from kernels.api import (
    PORTABLE_CPU,
    CustomOpDefinition,
    ExecutionMode,
    ImplementationIdentity,
    KernelRequest,
    Provider,
    RuntimeCompatibility,
    TensorSpec,
    output_like,
)
from kernels.dispatch import DispatchEvent, KernelDispatcher
from kernels.manifest import QualificationManifest, QualificationRecord, RevocationRecord
from kernels.registry import KernelImplementation, KernelRegistry

_SOURCE = hashlib.sha256(b"source").hexdigest()
_SCHEDULE = hashlib.sha256(b"schedule").hexdigest()
_EVIDENCE = hashlib.sha256(b"evidence").hexdigest()
_ARTIFACT = hashlib.sha256(b"artifact").hexdigest()


def test_root_package_preserves_public_api_without_eager_provider_imports() -> None:
    assert kernels.KernelDispatcher is KernelDispatcher
    assert kernels.KernelRegistry is KernelRegistry
    assert kernels.QualificationManifest is QualificationManifest
    assert callable(kernels.default_registry)
    with pytest.raises(AttributeError):
        kernels.__getattr__("unknown_public_symbol")


def _identity(provider: Provider, name: str) -> ImplementationIdentity:
    return ImplementationIdentity(provider, name, _SOURCE, name, "1", _SCHEDULE)


def _request(mode: ExecutionMode = ExecutionMode.INFERENCE) -> KernelRequest:
    spec = TensorSpec((2, 2), "float32")
    gradients = (0, 1) if mode == ExecutionMode.TRAINING else ()
    return KernelRequest(
        "test.add",
        (spec, spec),
        (spec,),
        "cpu",
        "generic",
        execution_mode=mode,
        gradient_inputs=gradients,
    )


def _registry() -> tuple[KernelRegistry, KernelImplementation]:
    registry = KernelRegistry()
    optimized = KernelImplementation(
        "test.add",
        _identity(Provider.TILELANG, "optimized"),
        lambda a, b: a + b,
        lambda *_: None,
        10,
        artifact_digest=_ARTIFACT,
    )
    reference = KernelImplementation(
        "test.add",
        _identity(Provider.PYTORCH, "reference"),
        lambda a, b: a + b,
        lambda *_: None,
        execution_modes=frozenset({ExecutionMode.INFERENCE, ExecutionMode.TRAINING}),
        differentiable_inputs=frozenset({0, 1}),
    )
    registry.register(optimized)
    registry.register(reference)
    return registry, optimized


def _qualification_pair(
    optimized: KernelImplementation,
    *,
    environment_digest: str = PORTABLE_CPU.runtime_environment_digest,
) -> tuple[QualificationRecord, QualificationRecord]:
    inference = _request()
    training = _request(ExecutionMode.TRAINING)

    def record(
        request: KernelRequest,
        paired: KernelRequest,
        mode: ExecutionMode,
    ) -> QualificationRecord:
        return QualificationRecord(
            request_digest=request.digest,
            paired_request_digest=paired.digest,
            execution_mode=mode,
            implementation_digest=optimized.identity.digest,
            evidence_digests=(_EVIDENCE,),
            environment_digest=environment_digest,
            toolchain_digest=optimized.toolchain_digest,
            artifact_digest=optimized.qualified_artifact_digest,
            target="cpu",
            architecture="generic",
            toolchain="optimized-1",
            approved_by="reviewer@example.test",
            created_at="2026-08-20T12:00:00Z",
        )

    return (
        record(inference, training, ExecutionMode.INFERENCE),
        record(training, inference, ExecutionMode.TRAINING),
    )


def test_request_identity_is_canonical_and_execution_contract_is_validated() -> None:
    first = _request()
    second = _request()
    assert first.digest == second.digest
    assert len(first.digest) == 64
    with pytest.raises(ValueError, match="inference"):
        replace(first, gradient_inputs=(0,))
    with pytest.raises(ValueError, match="identify differentiable"):
        replace(first, execution_mode=ExecutionMode.TRAINING)


def test_unqualified_and_disabled_candidates_fall_back_with_reasons() -> None:
    registry, _ = _registry()
    decision = KernelDispatcher(registry, QualificationManifest()).select(_request(), PORTABLE_CPU)
    assert decision.used_fallback
    assert decision.rejections[0].reason == "unqualified"

    disabled = KernelDispatcher(
        registry,
        QualificationManifest(),
        environment={"MINDCLADE_DISABLE_TILELANG": "true"},
    ).select(_request(), PORTABLE_CPU)
    assert disabled.used_fallback
    assert disabled.rejections[0].reason == "kill_switch"


def test_process_environment_kill_switch_is_active_by_default(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    registry, optimized = _registry()
    monkeypatch.setenv("MINDCLADE_DISABLE_TILELANG", "1")
    decision = KernelDispatcher(
        registry,
        QualificationManifest(_qualification_pair(optimized)),
    ).select(_request(), PORTABLE_CPU)
    assert decision.used_fallback
    assert decision.rejections[0].reason == "kill_switch"


def test_exact_paired_evidence_enables_and_revocation_disables_candidate() -> None:
    registry, optimized = _registry()
    request = _request()
    records = _qualification_pair(optimized)
    enabled = KernelDispatcher(registry, QualificationManifest(records)).select(
        request, PORTABLE_CPU
    )
    assert not enabled.used_fallback
    assert enabled.qualification == records[0]

    revocation = RevocationRecord(
        records[0].digest,
        "numerical regression",
        "2026-08-20T13:00:00Z",
    )
    disabled = KernelDispatcher(
        registry,
        QualificationManifest(records, (revocation,)),
    ).select(request, PORTABLE_CPU)
    assert disabled.used_fallback


def test_training_never_selects_an_inference_only_candidate() -> None:
    registry, optimized = _registry()
    decision = KernelDispatcher(
        registry,
        QualificationManifest(_qualification_pair(optimized)),
    ).select(_request(ExecutionMode.TRAINING), PORTABLE_CPU)
    assert decision.used_fallback
    assert decision.rejections[-1].reason == "execution_mode"


def test_qualification_is_bound_to_the_runtime_environment() -> None:
    registry, optimized = _registry()
    records = _qualification_pair(optimized, environment_digest="0" * 64)
    decision = KernelDispatcher(registry, QualificationManifest(records)).select(
        _request(), replace(PORTABLE_CPU, runtime_environment_digest="1" * 64)
    )
    assert decision.used_fallback
    assert decision.rejections[-1].reason == "qualification_environment"


def test_dispatch_rejects_source_candidates_without_a_bound_compiled_artifact() -> None:
    registry, optimized = _registry()
    records = _qualification_pair(optimized)
    unbound = replace(optimized, artifact_digest=None)
    replacement = KernelRegistry()
    replacement.register(unbound)
    replacement.register(registry.reference("test.add"))

    decision = KernelDispatcher(replacement, QualificationManifest(records)).select(
        _request(),
        PORTABLE_CPU,
    )

    assert decision.used_fallback
    assert decision.rejections[-1].reason == "artifact_unbound"


def test_operation_kill_switch_emits_one_structured_fallback_event() -> None:
    registry, optimized = _registry()
    events: list[DispatchEvent] = []
    decision = KernelDispatcher(
        registry,
        QualificationManifest(_qualification_pair(optimized)),
        environment={"MINDCLADE_DISABLE_TILELANG_OPERATIONS": "other,test.add"},
        event_sink=events.append,
    ).select(_request(), PORTABLE_CPU)
    assert decision.used_fallback
    assert len(events) == 1
    assert events[0].operation == "test.add"
    assert events[0].selected_provider == Provider.PYTORCH
    assert events[0].rejection_reasons == ("optimized:kill_switch",)


def test_manifest_v2_round_trip_requires_reciprocal_pairs() -> None:
    _, optimized = _registry()
    records = _qualification_pair(optimized)
    manifest = QualificationManifest(records)
    assert QualificationManifest.from_json(manifest.to_json()) == manifest
    with pytest.raises(ValueError, match="paired"):
        QualificationManifest((records[0],))


def test_revoking_either_half_disables_the_complete_qualification_pair() -> None:
    _, optimized = _registry()
    records = _qualification_pair(optimized)
    revocation = RevocationRecord(
        records[1].digest,
        "training-path regression",
        "2026-08-20T13:00:00Z",
    )
    manifest = QualificationManifest(records, (revocation,))
    assert (
        manifest.qualification(
            records[0].request_digest,
            records[0].implementation_digest,
        )
        is None
    )
    assert (
        manifest.qualification(
            records[1].request_digest,
            records[1].implementation_digest,
        )
        is None
    )


def test_manifest_identity_is_order_independent_and_parser_is_strict() -> None:
    _, optimized = _registry()
    records = _qualification_pair(optimized)
    first = QualificationManifest(records)
    second = QualificationManifest(tuple(reversed(records)))
    assert first.digest == second.digest
    assert first.to_json() == second.to_json()
    with pytest.raises(ValueError, match="unknown"):
        QualificationManifest.from_json(first.to_json()[:-1] + ',"unexpected":true}')


def test_runtime_compatibility_and_graph_safe_custom_op_contract() -> None:
    compatibility = RuntimeCompatibility(
        target="cuda",
        architecture="sm_90",
        device_name="H100",
        device_memory_bytes=80 * 1024**3,
        driver_version="580.65",
        runtime_version="13.0",
        pytorch_version="2.9.1",
        tilelang_version="0.1.13",
        tvm_ffi_version="0.1.12",
        compiler_version="13.0",
        os_release="linux",
        runtime_image_digest=f"sha256:{_ARTIFACT}",
    )
    assert compatibility.digest == compatibility.digest
    assert len(compatibility.digest) == 64
    with pytest.raises(ValueError, match="runtime_image_digest"):
        replace(compatibility, runtime_image_digest="sha256:mutable-tag")

    definition = CustomOpDefinition("mindclade_test", "square_contract", ("cpu",))

    def square(value: torch.Tensor) -> torch.Tensor:
        return value.square()

    def fake(value: torch.Tensor) -> torch.Tensor:
        return output_like(value, tuple(value.shape))

    def setup_context(ctx: Any, inputs: tuple[torch.Tensor], output: torch.Tensor) -> None:
        del output
        ctx.save_for_backward(inputs[0])

    def backward(context: Any, gradient: torch.Tensor) -> torch.Tensor:
        value: torch.Tensor = context.saved_tensors[0]
        return 2 * value * gradient

    operation = definition.register(
        square,
        fake=fake,
        backward=backward,
        setup_context=setup_context,
    )
    value = torch.tensor([2.0], requires_grad=True)
    operation(value).sum().backward()
    torch.testing.assert_close(value.grad, torch.tensor([4.0]))

    with FakeTensorMode():
        fake_value = torch.empty(3)
        assert tuple(operation(fake_value).shape) == (3,)
