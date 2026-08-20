# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Bounded, conservative redaction for observability attributes."""

from __future__ import annotations

import math
import re
from collections.abc import Mapping, Sequence
from dataclasses import dataclass
from types import MappingProxyType
from typing import Final
from urllib.parse import urlsplit, urlunsplit

from libs.python.errors import InvalidArgument, ResourceExhausted

REDACTED: Final = "[REDACTED]"
REDACTED_URL: Final = "[REDACTED_URL]"
MAXIMUM_REDACTION_DEPTH: Final = 16
MAXIMUM_REDACTION_NODES: Final = 4_096
MAXIMUM_STRING_LENGTH: Final = 4_096
MAXIMUM_MAPPING_FIELDS: Final = 128
_SENSITIVE_KEY = re.compile(
    r"(?:^|[_-])(?:api[_-]?key|auth(?:orization)?|cookie|credential|password|secret|"
    r"session|signed[_-]?url|token)(?:$|[_-])",
    re.IGNORECASE,
)
_URL_SCHEMES: Final = frozenset({"http", "https"})


@dataclass(slots=True)
class _Budget:
    nodes: int = 0

    def consume(self) -> None:
        self.nodes += 1
        if self.nodes > MAXIMUM_REDACTION_NODES:
            raise ResourceExhausted(
                f"observability attributes exceed {MAXIMUM_REDACTION_NODES} nodes",
                reason="observability_redaction_nodes",
            )


def is_sensitive_key(key: str) -> bool:
    """Return whether ``key`` names credential-like data."""
    return bool(_SENSITIVE_KEY.search(key))


def _safe_text(value: str) -> str:
    shortened = value[:MAXIMUM_STRING_LENGTH]
    try:
        parsed = urlsplit(shortened)
    except ValueError:
        return REDACTED_URL if shortened.lower().startswith(("http://", "https://")) else shortened
    if parsed.scheme.lower() not in _URL_SCHEMES or not parsed.netloc:
        return shortened
    hostname = parsed.hostname or ""
    if ":" in hostname:
        hostname = f"[{hostname}]"
    try:
        port_number = parsed.port
    except ValueError:
        return REDACTED_URL
    port = f":{port_number}" if port_number is not None else ""
    return urlunsplit((parsed.scheme.lower(), f"{hostname}{port}", parsed.path, "", ""))


def _redact(value: object, *, budget: _Budget, active: set[int], depth: int) -> object:
    budget.consume()
    if depth > MAXIMUM_REDACTION_DEPTH:
        raise ResourceExhausted(
            f"observability attributes exceed depth {MAXIMUM_REDACTION_DEPTH}",
            reason="observability_redaction_depth",
        )
    if isinstance(value, float):
        return value if math.isfinite(value) else "<non-finite>"
    if value is None or isinstance(value, bool | int):
        return value
    if isinstance(value, str):
        return _safe_text(value)
    if isinstance(value, bytes | bytearray | memoryview):
        return f"<{type(value).__name__}:{len(value)}>"

    identity = id(value)
    if identity in active:
        return "<cycle>"
    if isinstance(value, Mapping):
        if len(value) > MAXIMUM_MAPPING_FIELDS:
            raise ResourceExhausted(
                f"observability mapping exceeds {MAXIMUM_MAPPING_FIELDS} fields",
                reason="observability_redaction_fields",
            )
        active.add(identity)
        try:
            result: dict[str, object] = {}
            for key, item in value.items():
                if not isinstance(key, str) or not key or len(key) > 128:
                    raise InvalidArgument(
                        "observability attribute keys must be non-empty strings of at most 128 characters",
                        reason="observability_attribute_key",
                    )
                result[key] = (
                    REDACTED
                    if is_sensitive_key(key)
                    else _redact(item, budget=budget, active=active, depth=depth + 1)
                )
            return result
        finally:
            active.remove(identity)
    if isinstance(value, Sequence):
        active.add(identity)
        try:
            return [_redact(item, budget=budget, active=active, depth=depth + 1) for item in value]
        finally:
            active.remove(identity)
    return f"<{type(value).__name__}>"


def redact(value: object) -> object:
    """Return a JSON-compatible, bounded, credential-redacted snapshot."""
    return _redact(value, budget=_Budget(), active=set(), depth=0)


def redact_fields(fields: Mapping[str, object]) -> Mapping[str, object]:
    """Redact and freeze a top-level attribute mapping."""
    if not isinstance(fields, Mapping):
        raise InvalidArgument(
            "observability fields must be a mapping",
            reason="observability_fields_type",
        )
    redacted = redact(fields)
    if not isinstance(redacted, dict):  # pragma: no cover - guarded by the input check
        raise AssertionError("redaction did not preserve a mapping")
    return MappingProxyType(redacted)
