# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""W3C-compatible trace identifiers without provider or global state."""

from __future__ import annotations

import re
import secrets
from collections.abc import Mapping
from dataclasses import dataclass, field

from libs.python.errors import InvalidArgument

from .redaction import redact_fields

_TRACE_ID = re.compile(r"^[0-9a-f]{32}$")
_SPAN_ID = re.compile(r"^[0-9a-f]{16}$")
_ZERO_TRACE_ID = "0" * 32
_ZERO_SPAN_ID = "0" * 16


@dataclass(frozen=True, slots=True)
class TraceContext:
    trace_id: str
    span_id: str
    sampled: bool = False

    def __post_init__(self) -> None:
        if (
            not isinstance(self.trace_id, str)
            or not _TRACE_ID.fullmatch(self.trace_id)
            or self.trace_id == _ZERO_TRACE_ID
        ):
            raise InvalidArgument(
                "trace_id must be 32 non-zero lowercase hex digits", reason="trace_id"
            )
        if (
            not isinstance(self.span_id, str)
            or not _SPAN_ID.fullmatch(self.span_id)
            or self.span_id == _ZERO_SPAN_ID
        ):
            raise InvalidArgument(
                "span_id must be 16 non-zero lowercase hex digits", reason="span_id"
            )
        if not isinstance(self.sampled, bool):
            raise InvalidArgument("sampled must be a boolean", reason="trace_sampled")

    @classmethod
    def root(cls, *, sampled: bool = False) -> TraceContext:
        return cls(secrets.token_hex(16), secrets.token_hex(8), sampled)

    def child(self) -> TraceContext:
        return TraceContext(self.trace_id, secrets.token_hex(8), self.sampled)

    def traceparent(self) -> str:
        flags = "01" if self.sampled else "00"
        return f"00-{self.trace_id}-{self.span_id}-{flags}"

    @classmethod
    def from_traceparent(cls, value: object) -> TraceContext:
        if not isinstance(value, str):
            raise InvalidArgument("traceparent must be text", reason="traceparent")
        fields = value.split("-")
        if len(fields) != 4 or fields[0] != "00" or fields[3] not in {"00", "01"}:
            raise InvalidArgument("traceparent is malformed or unsupported", reason="traceparent")
        return cls(fields[1], fields[2], fields[3] == "01")


@dataclass(frozen=True, slots=True)
class SpanEvent:
    context: TraceContext
    name: str
    attributes: Mapping[str, object] = field(default_factory=dict)

    def __post_init__(self) -> None:
        if not isinstance(self.context, TraceContext):
            raise InvalidArgument("span context is invalid", reason="span_context")
        if not isinstance(self.name, str) or not self.name or len(self.name) > 128:
            raise InvalidArgument("span names must contain 1-128 characters", reason="span_name")
        object.__setattr__(self, "attributes", redact_fields(self.attributes))

    def to_document(self) -> dict[str, object]:
        return {
            "attributes": dict(self.attributes),
            "name": self.name,
            "span_id": self.context.span_id,
            "trace_id": self.context.trace_id,
        }
