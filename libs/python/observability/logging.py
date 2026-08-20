# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Provider-neutral structured log events."""

from __future__ import annotations

import re
from collections.abc import Mapping
from dataclasses import dataclass, field
from enum import StrEnum
from typing import Final

from libs.python.errors import InvalidArgument, ResourceExhausted

from .redaction import redact_fields

MAXIMUM_LOG_FIELDS: Final = 64
MAXIMUM_LOG_MESSAGE_LENGTH: Final = 2_048
_EVENT_NAME = re.compile(r"^[a-z][a-z0-9_.-]{0,127}$")


class LogLevel(StrEnum):
    DEBUG = "debug"
    INFO = "info"
    WARNING = "warning"
    ERROR = "error"
    CRITICAL = "critical"


@dataclass(frozen=True, slots=True)
class LogEvent:
    """A finite, redacted log record; emission remains the application's job."""

    event: str
    message: str
    level: LogLevel = LogLevel.INFO
    fields: Mapping[str, object] = field(default_factory=dict)
    timestamp_unix_millis: int | None = None

    def __post_init__(self) -> None:
        if not isinstance(self.event, str) or not _EVENT_NAME.fullmatch(self.event):
            raise InvalidArgument(
                "log event names must be canonical lowercase identifiers",
                reason="observability_event_name",
            )
        if not isinstance(self.message, str) or len(self.message) > MAXIMUM_LOG_MESSAGE_LENGTH:
            raise InvalidArgument(
                f"log messages must be strings of at most {MAXIMUM_LOG_MESSAGE_LENGTH} characters",
                reason="observability_log_message",
            )
        if not isinstance(self.level, LogLevel):
            raise InvalidArgument("log level is invalid", reason="observability_log_level")
        if not isinstance(self.fields, Mapping):
            raise InvalidArgument("log fields must be a mapping", reason="observability_log_fields")
        if len(self.fields) > MAXIMUM_LOG_FIELDS:
            raise ResourceExhausted(
                f"log event exceeds {MAXIMUM_LOG_FIELDS} fields",
                reason="observability_log_fields",
            )
        if self.timestamp_unix_millis is not None and (
            isinstance(self.timestamp_unix_millis, bool)
            or not isinstance(self.timestamp_unix_millis, int)
            or self.timestamp_unix_millis < 0
        ):
            raise InvalidArgument(
                "log timestamp must be a non-negative integer",
                reason="observability_timestamp",
            )
        object.__setattr__(self, "fields", redact_fields(self.fields))

    def to_document(self) -> dict[str, object]:
        document: dict[str, object] = {
            "event": self.event,
            "fields": dict(self.fields),
            "level": self.level.value,
            "message": self.message,
        }
        if self.timestamp_unix_millis is not None:
            document["timestamp_unix_millis"] = self.timestamp_unix_millis
        return document
