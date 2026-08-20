# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

import math

import pytest
from hypothesis import given
from hypothesis import strategies as st

from libs.python.errors import InvalidArgument
from libs.python.worker_runtime import StageEnvelope, StageKind, StageResult
from libs.python.worker_runtime.contracts import MAXIMUM_NAME_LENGTH, MAXIMUM_UINT32, MAXIMUM_UINT64

DIGEST = "sha256:" + "1" * 64
STAGE_ID = "stage_01890f2c7b7a70008000000000000000"
SAFE_TEXT = st.text(
    alphabet=st.characters(min_codepoint=0x21, max_codepoint=0x7E),
    min_size=1,
    max_size=MAXIMUM_NAME_LENGTH,
)


@given(
    operation=SAFE_TEXT,
    attempt=st.sampled_from([1, MAXIMUM_UINT32]),
    fencing_token=st.sampled_from([1, MAXIMUM_UINT64]),
    deadline=st.sampled_from([1, MAXIMUM_UINT64]),
)
def test_worker_contract_accepts_boundary_sized_valid_values(
    operation: str, attempt: int, fencing_token: int, deadline: int
) -> None:
    envelope = StageEnvelope(
        stage_id=STAGE_ID,
        kind=StageKind.PREPROCESS,
        operation=operation,
        inputs=(),
        output_namespace="tenant/a",
        resolved_config_digest=DIGEST,
        reference_snapshot_digest=None,
        attempt=attempt,
        fencing_token=fencing_token,
        deadline_unix_millis=deadline,
    )
    envelope.validate()


@given(
    malformed=st.one_of(
        st.text(min_size=MAXIMUM_NAME_LENGTH + 1, max_size=MAXIMUM_NAME_LENGTH + 32),
        st.sampled_from(["line\nbreak", "\x00"]),
    )
)
def test_worker_contract_rejects_malformed_text(malformed: str) -> None:
    with pytest.raises(InvalidArgument):
        StageEnvelope(
            stage_id=STAGE_ID,
            kind=StageKind.PREPROCESS,
            operation=malformed,
            inputs=(),
            output_namespace="tenant/a",
            resolved_config_digest=DIGEST,
            reference_snapshot_digest=None,
            attempt=1,
            fencing_token=1,
            deadline_unix_millis=1,
        )


@given(metric=st.sampled_from([True, math.nan, math.inf, -math.inf, 10**10_000]))
def test_worker_result_rejects_malformed_metrics(metric: float) -> None:
    with pytest.raises(InvalidArgument):
        StageResult((), {"loss": metric})
