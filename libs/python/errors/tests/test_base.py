# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

import pytest

from libs.python.errors import (
    MAXIMUM_FIELDS,
    MAXIMUM_MESSAGE_LENGTH,
    Code,
    DeadlineExceeded,
    FailedPrecondition,
    InvalidArgument,
    MindcladeError,
    backoff_retry,
    code_of,
    fields_of,
    is_retryable,
    operation_of,
    public_message_of,
    reason_of,
    retry_of,
    wrap,
)


def test_empty_message_falls_back_to_the_code_default() -> None:
    assert MindcladeError(Code.NOT_FOUND).message == "resource not found"
    assert MindcladeError(Code.NOT_FOUND, "   ").message == "resource not found"


def test_unknown_code_is_normalized_rather_than_stored_verbatim() -> None:
    assert MindcladeError("teapot").code is Code.UNKNOWN


def test_diagnostic_string_includes_operation_and_cause() -> None:
    fault = MindcladeError(
        Code.UNAVAILABLE, "store unreachable", operation="resolve", cause=OSError("refused")
    )
    assert str(fault) == "resolve: store unreachable: refused"


def test_public_message_never_leaks_the_cause() -> None:
    # The disclosure boundary. str() is for operators, public_message_of is for callers.
    fault = MindcladeError(Code.INTERNAL, "request failed", cause=OSError("/etc/secrets missing"))
    assert public_message_of(fault) == "request failed"
    assert "secrets" not in public_message_of(fault)


def test_public_message_of_a_foreign_exception_is_generic() -> None:
    # An arbitrary exception's text has not been vetted for disclosure, and this
    # accessor exists precisely so its result can be sent outward.
    assert public_message_of(RuntimeError("/etc/secrets missing")) == "operation failed"
    assert code_of(RuntimeError("boom")) is Code.UNKNOWN


def test_accessors_are_total_over_none_and_foreign_errors() -> None:
    for value in (None, RuntimeError("boom")):
        assert code_of(value) is Code.UNKNOWN
        assert reason_of(value) == ""
        assert operation_of(value) == ""
        assert fields_of(value) == {}
        assert not is_retryable(value)


def test_fields_are_copied_so_later_caller_mutation_cannot_reclassify() -> None:
    mutable = {"host": "a"}
    fault = MindcladeError(Code.UNAVAILABLE, fields=mutable)
    mutable["host"] = "b"
    assert fault.fields["host"] == "a"


def test_fields_are_not_writable_through_the_accessor() -> None:
    fault = MindcladeError(Code.UNAVAILABLE, fields={"host": "a"})
    with pytest.raises(TypeError):
        fault.fields["host"] = "b"  # type: ignore[index]


def test_field_bounds_raise_rather_than_truncate() -> None:
    with pytest.raises(ValueError, match="more than"):
        MindcladeError(Code.INTERNAL, fields={str(n): "v" for n in range(MAXIMUM_FIELDS + 1)})
    with pytest.raises(ValueError, match="too long"):
        MindcladeError(Code.INTERNAL, fields={"k" * 129: "v"})
    with pytest.raises(ValueError, match="value bound"):
        MindcladeError(Code.INTERNAL, fields={"k": "v" * 4097})


def test_fault_text_and_field_types_are_checked_at_runtime() -> None:
    with pytest.raises(TypeError, match="must be strings"):
        MindcladeError(Code.INTERNAL, message=1)  # type: ignore[arg-type]
    with pytest.raises(TypeError, match="keys and values"):
        MindcladeError(Code.INTERNAL, fields={"attempt": 1})  # type: ignore[dict-item]
    with pytest.raises(ValueError, match="message exceeds"):
        MindcladeError(Code.INTERNAL, message="x" * (MAXIMUM_MESSAGE_LENGTH + 1))


def test_retry_intent_round_trips_and_is_normalized() -> None:
    fault = MindcladeError(Code.UNAVAILABLE, retry=backoff_retry(3))
    assert is_retryable(fault)
    assert retry_of(fault).max_attempts == 3


def test_a_fault_without_retry_intent_is_not_retryable() -> None:
    # Fail closed. Inferring retryability from the code would retry permission_denied.
    assert not is_retryable(MindcladeError(Code.UNAVAILABLE))


def test_wrap_passes_none_through() -> None:
    assert wrap(None, Code.INTERNAL) is None


def test_wrap_sets_both_the_accessor_and_the_exception_chain() -> None:
    cause = OSError("refused")
    fault = wrap(cause, Code.UNAVAILABLE, "store unreachable")
    assert fault is not None
    assert fault.cause is cause
    assert fault.__cause__ is cause


def test_convenience_subclasses_carry_their_code_and_builtin_base() -> None:
    assert code_of(InvalidArgument("bad digest")) is Code.INVALID_ARGUMENT
    assert isinstance(InvalidArgument("bad digest"), ValueError)
    assert code_of(FailedPrecondition()) is Code.FAILED_PRECONDITION
    assert isinstance(FailedPrecondition(), ValueError)
    assert code_of(DeadlineExceeded()) is Code.DEADLINE_EXCEEDED


def test_deadline_exceeded_is_not_a_timeout_error() -> None:
    # TimeoutError descends from OSError, whose instance layout cannot be combined with
    # MindcladeError. This asserts the documented consequence rather than the intent.
    assert not isinstance(DeadlineExceeded(), TimeoutError)
    assert isinstance(DeadlineExceeded(), MindcladeError)


def test_reason_and_operation_are_stripped() -> None:
    fault = MindcladeError(Code.CONFLICT, "x", reason="  stale_version ", operation=" write ")
    assert fault.reason == "stale_version"
    assert fault.operation == "write"
