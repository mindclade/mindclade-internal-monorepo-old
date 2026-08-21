# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

import hashlib
from dataclasses import replace

from kernels.api import (
    PORTABLE_CPU,
    ImplementationIdentity,
    KernelRequest,
    Provider,
    TensorSpec,
)
from kernels.dispatch import KernelDispatcher
from kernels.manifest import QualificationManifest, QualificationRecord, RevocationRecord
from kernels.registry import KernelImplementation, KernelRegistry

_SOURCE = hashlib.sha256(b"source").hexdigest()
_SCHEDULE = hashlib.sha256(b"schedule").hexdigest()
_EVIDENCE = hashlib.sha256(b"evidence").hexdigest()


def _identity(provider: Provider, name: str) -> ImplementationIdentity:
    return ImplementationIdentity(provider, name, _SOURCE, name, "1", _SCHEDULE)


def _request() -> KernelRequest:
    spec = TensorSpec((2, 2), "float32")
    return KernelRequest("test.add", (spec, spec), (spec,), "cpu", "generic")


def _registry() -> tuple[KernelRegistry, KernelImplementation]:
    registry = KernelRegistry()
    optimized = KernelImplementation(
        "test.add",
        _identity(Provider.TILELANG, "optimized"),
        lambda a, b: a + b,
        lambda *_: None,
        10,
    )
    reference = KernelImplementation(
        "test.add", _identity(Provider.PYTORCH, "reference"), lambda a, b: a + b, lambda *_: None
    )
    registry.register(optimized)
    registry.register(reference)
    return registry, optimized


def test_request_identity_is_canonical_and_semantics_must_be_sorted() -> None:
    first = _request()
    second = _request()
    assert first.digest == second.digest
    assert len(first.digest) == 64


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


def test_exact_evidence_enables_and_revocation_disables_candidate() -> None:
    registry, optimized = _registry()
    request = _request()
    record = QualificationRecord(
        request_digest=request.digest,
        implementation_digest=optimized.identity.digest,
        evidence_digests=(_EVIDENCE,),
        environment_digest=PORTABLE_CPU.runtime_environment_digest,
        target="cpu",
        architecture="generic",
        toolchain="optimized-1",
        approved_by="reviewer@example.test",
        created_at="2026-08-20T12:00:00Z",
    )
    enabled = KernelDispatcher(registry, QualificationManifest((record,))).select(
        request, PORTABLE_CPU
    )
    assert not enabled.used_fallback
    assert enabled.qualification == record

    revocation = RevocationRecord(record.digest, "numerical regression", "2026-08-20T13:00:00Z")
    disabled = KernelDispatcher(registry, QualificationManifest((record,), (revocation,))).select(
        request, PORTABLE_CPU
    )
    assert disabled.used_fallback


def test_qualification_is_bound_to_the_runtime_environment() -> None:
    registry, optimized = _registry()
    request = _request()
    record = QualificationRecord(
        request_digest=request.digest,
        implementation_digest=optimized.identity.digest,
        evidence_digests=(_EVIDENCE,),
        environment_digest="0" * 64,
        target="cpu",
        architecture="generic",
        toolchain="optimized-1",
        approved_by="reviewer@example.test",
        created_at="2026-08-20T12:00:00Z",
    )

    decision = KernelDispatcher(registry, QualificationManifest((record,))).select(
        request, replace(PORTABLE_CPU, runtime_environment_digest="1" * 64)
    )

    assert decision.used_fallback
    assert decision.rejections[-1].reason == "qualification_environment"
