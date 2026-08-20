# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Structured, immutable faults for Python libraries and workers.

A fault carries a stable :class:`~libs.python.errors.codes.Code`, a client-safe
message, an optional machine-readable ``reason`` and ``operation``, bounded
structured ``fields``, explicit retry intent, and an optional wrapped ``cause``.
It deliberately knows nothing about HTTP, gRPC, Connect, logging, telemetry,
persistence, or PyTorch — the same non-responsibilities ``libs/go/faults/doc.go``
declares for the Go side.

**Do not serialize a fault's string form to an external caller.** ``str(fault)``
is a diagnostic and includes the wrapped cause, which is how internal detail
leaks into a client response. Transports serialize :func:`code_of`,
:func:`public_message_of`, :func:`reason_of` and a reviewed subset of
:func:`fields_of` instead.

``InvalidArgument`` and ``FailedPrecondition`` also inherit ``ValueError``, so a
package can adopt this taxonomy without breaking a consumer that still catches the
built-in. ``DeadlineExceeded`` deliberately does not inherit ``TimeoutError``:
``TimeoutError`` descends from ``OSError``, whose instance layout is incompatible
with a plain ``Exception`` subclass, and Python rejects the combination at
class-creation time. Nothing in this tree catches ``TimeoutError`` — the sole raise
site was ``worker_runtime/executor.py``, which now raises this instead — so the
narrower base costs nothing.
"""

from __future__ import annotations

from collections.abc import Mapping
from types import MappingProxyType

from .codes import Code, default_message, normalize_code
from .retry import RetryPolicy

# Bounds on the diagnostic map. Fields ride along with a fault across a process
# boundary and into logs, so an unbounded map is an unbounded log line and an
# unbounded message frame. The numbers match the envelope limits in
# libs/python/worker_runtime/contracts.py rather than inventing a second scale.
MAXIMUM_FIELDS = 128
MAXIMUM_FIELD_KEY_LENGTH = 128
MAXIMUM_FIELD_VALUE_LENGTH = 4096

_EMPTY_FIELDS: Mapping[str, str] = MappingProxyType({})


class MindcladeError(Exception):
    """An immutable structured fault.

    Attributes are exposed through read-only properties and ``fields`` is returned
    as a mapping proxy, so a fault's classification cannot be edited after the
    code that understood the failure has published it.
    """

    __slots__ = ("_cause", "_code", "_fields", "_message", "_operation", "_reason", "_retry")

    def __init__(
        self,
        code: Code | str = Code.UNKNOWN,
        message: str = "",
        *,
        reason: str = "",
        operation: str = "",
        fields: Mapping[str, str] | None = None,
        retry: RetryPolicy | None = None,
        cause: BaseException | None = None,
    ) -> None:
        normalized_code = normalize_code(code)
        normalized_message = message.strip() or default_message(normalized_code)

        self._code = normalized_code
        self._message = normalized_message
        self._reason = reason.strip()
        self._operation = operation.strip()
        self._fields = _clone_fields(fields)
        self._retry = (retry or RetryPolicy()).normalized()
        self._cause = cause

        super().__init__(normalized_message)
        if cause is not None:
            self.__cause__ = cause

    @property
    def code(self) -> Code:
        return self._code

    @property
    def message(self) -> str:
        """The client-safe message. Never includes the cause."""
        return self._message

    @property
    def reason(self) -> str:
        return self._reason

    @property
    def operation(self) -> str:
        return self._operation

    @property
    def fields(self) -> Mapping[str, str]:
        return self._fields

    @property
    def retry(self) -> RetryPolicy:
        return self._retry

    @property
    def cause(self) -> BaseException | None:
        return self._cause

    def __str__(self) -> str:
        """A diagnostic string. May include the cause; do not send it to a client."""
        rendered = self._message
        if self._operation:
            rendered = f"{self._operation}: {rendered}"
        if self._cause is not None:
            rendered = f"{rendered}: {self._cause}"
        return rendered

    def __repr__(self) -> str:
        return (
            f"{type(self).__name__}(code={self._code.value!r}, message={self._message!r}, "
            f"reason={self._reason!r}, operation={self._operation!r})"
        )


class InvalidArgument(MindcladeError, ValueError):
    """A caller supplied something the contract does not accept."""

    def __init__(self, message: str = "", **kwargs: object) -> None:
        kwargs.pop("code", None)
        super().__init__(Code.INVALID_ARGUMENT, message, **kwargs)  # type: ignore[arg-type]


class FailedPrecondition(MindcladeError, ValueError):
    """The system is not in a state where the operation can be attempted."""

    def __init__(self, message: str = "", **kwargs: object) -> None:
        kwargs.pop("code", None)
        super().__init__(Code.FAILED_PRECONDITION, message, **kwargs)  # type: ignore[arg-type]


class ResourceExhausted(MindcladeError):
    """A bounded resource — quota, counter space, buffer — has no room left."""

    def __init__(self, message: str = "", **kwargs: object) -> None:
        kwargs.pop("code", None)
        super().__init__(Code.RESOURCE_EXHAUSTED, message, **kwargs)  # type: ignore[arg-type]


class DeadlineExceeded(MindcladeError):
    """The deadline for the operation passed before it could be completed.

    Not a ``TimeoutError``: that type descends from ``OSError`` and carries extra
    C-level slots, so mixing it with ``MindcladeError`` raises "multiple bases have
    instance lay-out conflict" when the class is created. Catch ``MindcladeError``
    or this type by name.
    """

    def __init__(self, message: str = "", **kwargs: object) -> None:
        kwargs.pop("code", None)
        super().__init__(Code.DEADLINE_EXCEEDED, message, **kwargs)  # type: ignore[arg-type]


def _clone_fields(fields: Mapping[str, str] | None) -> Mapping[str, str]:
    """Copy and bound-check the diagnostic map.

    Copied rather than referenced: the caller's dict is frequently a local that
    keeps being mutated after the fault is raised, which would otherwise change
    what a handler three frames up sees.
    """
    if not fields:
        return _EMPTY_FIELDS
    if len(fields) > MAXIMUM_FIELDS:
        raise ValueError(f"errors: fault carries more than {MAXIMUM_FIELDS} fields")
    cloned: dict[str, str] = {}
    for key, value in fields.items():
        if not key or len(key) > MAXIMUM_FIELD_KEY_LENGTH:
            raise ValueError(f"errors: fault field key {key!r} is empty or too long")
        if len(value) > MAXIMUM_FIELD_VALUE_LENGTH:
            raise ValueError(f"errors: fault field {key!r} exceeds the value bound")
        cloned[key] = value
    return MappingProxyType(cloned)


def wrap(
    cause: BaseException | None,
    code: Code | str = Code.UNKNOWN,
    message: str = "",
    **kwargs: object,
) -> MindcladeError | None:
    """Wrap ``cause`` in a fault, returning ``None`` for a ``None`` cause.

    The ``None`` passthrough mirrors Go's ``faults.Wrap`` so a caller can wrap
    unconditionally at the end of a fallible path without first testing.
    """
    if cause is None:
        return None
    return MindcladeError(code, message, cause=cause, **kwargs)  # type: ignore[arg-type]


def code_of(error: BaseException | None) -> Code:
    """The code of ``error``, or ``UNKNOWN`` for anything not a fault."""
    if isinstance(error, MindcladeError):
        return error.code
    return Code.UNKNOWN


def public_message_of(error: BaseException | None) -> str:
    """The client-safe message for ``error``, never including a wrapped cause.

    A non-fault exception yields the default message for ``UNKNOWN`` rather than
    its own ``str()``: an arbitrary exception's text is not vetted for disclosure,
    and this function's whole purpose is that its result can be sent outward.
    """
    if isinstance(error, MindcladeError):
        return error.message
    return default_message(Code.UNKNOWN)


def reason_of(error: BaseException | None) -> str:
    if isinstance(error, MindcladeError):
        return error.reason
    return ""


def operation_of(error: BaseException | None) -> str:
    if isinstance(error, MindcladeError):
        return error.operation
    return ""


def fields_of(error: BaseException | None) -> Mapping[str, str]:
    if isinstance(error, MindcladeError):
        return error.fields
    return _EMPTY_FIELDS


def retry_of(error: BaseException | None) -> RetryPolicy:
    if isinstance(error, MindcladeError):
        return error.retry
    return RetryPolicy()


def is_retryable(error: BaseException | None) -> bool:
    """Whether ``error`` explicitly permits another attempt.

    Fail-closed: an error carrying no retry intent is not retryable. Guessing
    from the code would make a caller retry a ``permission_denied`` forever.
    """
    return retry_of(error).retryable()
