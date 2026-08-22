# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Payload-free MLflow trace projection with bounded explicit lifecycles."""

from __future__ import annotations

import re
from dataclasses import dataclass
from threading import Lock
from typing import Any, Protocol

MAXIMUM_ACTIVE_TRACES = 128
MAXIMUM_SPANS_PER_TRACE = 1024
MAXIMUM_ATTRIBUTES = 64
MAXIMUM_ATTRIBUTE_VALUE_LENGTH = 512

_NAME = re.compile(r"^[a-z][a-z0-9_.-]{0,127}$")
_WORKSPACE = re.compile(r"^(?!.*--)[a-z0-9](?:[-a-z0-9]{0,61}[a-z0-9])$")
_FORBIDDEN_ATTRIBUTE_FRAGMENTS = {
    "authorization",
    "body",
    "content",
    "cookie",
    "credential",
    "email",
    "input",
    "message",
    "output",
    "password",
    "payload",
    "prompt",
    "secret",
    "token",
}


class Span(Protocol):
    trace_id: str
    span_id: str


class TraceClient(Protocol):
    def start_trace(
        self,
        name: str,
        *,
        span_type: str = "UNKNOWN",
        inputs: Any | None = None,
        attributes: dict[str, str] | None = None,
        tags: dict[str, str] | None = None,
        experiment_id: str | None = None,
        start_time_ns: int | None = None,
    ) -> Span: ...

    def start_span(
        self,
        name: str,
        trace_id: str,
        parent_id: str,
        *,
        span_type: str = "UNKNOWN",
        inputs: Any | None = None,
        attributes: dict[str, Any] | None = None,
        start_time_ns: int | None = None,
    ) -> Span: ...

    def end_span(
        self,
        trace_id: str,
        span_id: str,
        *,
        outputs: Any | None = None,
        attributes: dict[str, Any] | None = None,
        status: str = "OK",
        end_time_ns: int | None = None,
    ) -> object: ...

    def end_trace(
        self,
        trace_id: str,
        *,
        outputs: Any | None = None,
        attributes: dict[str, Any] | None = None,
        status: str = "OK",
        end_time_ns: int | None = None,
    ) -> object: ...


@dataclass(frozen=True, slots=True)
class TraceIdentity:
    request_id: str
    workspace: str
    operation: str
    source_revision: str
    runtime_image_digest: str
    model_digest: str | None = None
    dataset_digest: str | None = None
    evidence_digest: str | None = None
    classification: str = "internal"

    def validate(self) -> None:
        _text(self.request_id, "trace request id", 256)
        if not _WORKSPACE.fullmatch(self.workspace) or self.workspace in {
            "api",
            "default",
            "static-files",
            "workspaces",
        }:
            raise ValueError("trace workspace is invalid or reserved")
        _name(self.operation, "trace operation")
        if (
            not isinstance(self.source_revision, str)
            or len(self.source_revision) not in {40, 64}
            or any(character not in "0123456789abcdef" for character in self.source_revision)
        ):
            raise ValueError("trace source revision is invalid")
        _digest(self.runtime_image_digest, "trace runtime image digest")
        for label, value in (
            ("trace model digest", self.model_digest),
            ("trace dataset digest", self.dataset_digest),
            ("trace evidence digest", self.evidence_digest),
        ):
            if value is not None:
                _digest(value, label)
        if self.classification not in {"public", "internal", "confidential", "restricted"}:
            raise ValueError("trace classification is invalid")

    def tags(self) -> dict[str, str]:
        self.validate()
        tags = {
            "mindclade.authority": "mirror",
            "mindclade.classification": self.classification,
            "mindclade.operation": self.operation,
            "mindclade.request_id": self.request_id,
            "mindclade.runtime_image_digest": self.runtime_image_digest,
            "mindclade.source_revision": self.source_revision,
            "mindclade.trace_schema": "1",
            "mindclade.workspace": self.workspace,
        }
        for key, value in (
            ("mindclade.model_digest", self.model_digest),
            ("mindclade.dataset_digest", self.dataset_digest),
            ("mindclade.evidence_digest", self.evidence_digest),
        ):
            if value is not None:
                tags[key] = value
        return tags


@dataclass(frozen=True, slots=True)
class TraceHandle:
    trace_id: str
    root_span_id: str


@dataclass(frozen=True, slots=True)
class SpanHandle:
    trace_id: str
    span_id: str


class MLflowTraceExporter:
    """Mirrors structural traces while never exporting request or response bodies."""

    def __init__(self, client: TraceClient, experiment_id: str, *, required: bool = False) -> None:
        if not callable(getattr(client, "start_trace", None)):
            raise ValueError("MLflow trace client is invalid")
        _text(experiment_id, "MLflow trace experiment id", 256)
        if not isinstance(required, bool):
            raise ValueError("MLflow trace required mode must be boolean")
        self._client = client
        self._experiment_id = experiment_id
        self._required = required
        self._active: dict[str, set[str]] = {}
        self._failures = 0
        self._lock = Lock()

    @property
    def failures(self) -> int:
        with self._lock:
            return self._failures

    @property
    def active_trace_count(self) -> int:
        with self._lock:
            return len(self._active)

    def start(
        self,
        name: str,
        identity: TraceIdentity,
        *,
        span_type: str = "CHAIN",
        attributes: dict[str, str] | None = None,
        start_time_ns: int | None = None,
    ) -> TraceHandle | None:
        _name(name, "trace name")
        identity.validate()
        normalized = _attributes(attributes or {})
        timestamp = _timestamp(start_time_ns)
        with self._lock:
            if len(self._active) >= MAXIMUM_ACTIVE_TRACES:
                raise RuntimeError("MLflow active trace bound reached")
            try:
                span = self._client.start_trace(
                    name,
                    span_type=span_type,
                    inputs=None,
                    attributes=normalized,
                    tags=identity.tags(),
                    experiment_id=self._experiment_id,
                    start_time_ns=timestamp,
                )
                trace_id = _text(span.trace_id, "MLflow trace id", 256)
                span_id = _text(span.span_id, "MLflow root span id", 256)
                if trace_id in self._active:
                    raise RuntimeError("MLflow returned a duplicate active trace id")
                self._active[trace_id] = {span_id}
                return TraceHandle(trace_id, span_id)
            except Exception as error:
                self._failed(error)
                return None

    def start_span(
        self,
        trace: TraceHandle,
        name: str,
        *,
        parent: SpanHandle | None = None,
        span_type: str = "UNKNOWN",
        attributes: dict[str, str] | None = None,
        start_time_ns: int | None = None,
    ) -> SpanHandle | None:
        _name(name, "span name")
        normalized = _attributes(attributes or {})
        timestamp = _timestamp(start_time_ns)
        with self._lock:
            spans = self._trace_spans(trace)
            parent_id = parent.span_id if parent is not None else trace.root_span_id
            if parent is not None and (parent.trace_id != trace.trace_id or parent_id not in spans):
                raise ValueError("MLflow span parent is not active in this trace")
            if len(spans) >= MAXIMUM_SPANS_PER_TRACE:
                raise RuntimeError("MLflow span bound reached")
            try:
                span = self._client.start_span(
                    name,
                    trace.trace_id,
                    parent_id,
                    span_type=span_type,
                    inputs=None,
                    attributes=normalized,
                    start_time_ns=timestamp,
                )
                if span.trace_id != trace.trace_id:
                    raise RuntimeError("MLflow returned a span for a different trace")
                span_id = _text(span.span_id, "MLflow span id", 256)
                if span_id in spans:
                    raise RuntimeError("MLflow returned a duplicate span id")
                spans.add(span_id)
                return SpanHandle(trace.trace_id, span_id)
            except Exception as error:
                self._failed(error)
                return None

    def end_span(
        self,
        trace: TraceHandle,
        span: SpanHandle,
        *,
        status: str = "OK",
        attributes: dict[str, str] | None = None,
        end_time_ns: int | None = None,
    ) -> bool:
        normalized = _attributes(attributes or {})
        _status(status)
        timestamp = _timestamp(end_time_ns)
        with self._lock:
            spans = self._trace_spans(trace)
            if span.trace_id != trace.trace_id or span.span_id == trace.root_span_id:
                raise ValueError("cannot end a foreign or root span with end_span")
            if span.span_id not in spans:
                raise ValueError("MLflow span is not active")
            try:
                self._client.end_span(
                    trace.trace_id,
                    span.span_id,
                    outputs=None,
                    attributes=normalized,
                    status=status,
                    end_time_ns=timestamp,
                )
                spans.remove(span.span_id)
                return True
            except Exception as error:
                return self._failed_bool(error)

    def end(
        self,
        trace: TraceHandle,
        *,
        status: str = "OK",
        attributes: dict[str, str] | None = None,
        end_time_ns: int | None = None,
    ) -> bool:
        normalized = _attributes(attributes or {})
        _status(status)
        timestamp = _timestamp(end_time_ns)
        with self._lock:
            spans = self._trace_spans(trace)
            if spans != {trace.root_span_id}:
                raise RuntimeError("MLflow trace has active child spans")
            try:
                self._client.end_trace(
                    trace.trace_id,
                    outputs=None,
                    attributes=normalized,
                    status=status,
                    end_time_ns=timestamp,
                )
                del self._active[trace.trace_id]
                return True
            except Exception as error:
                return self._failed_bool(error)

    def _trace_spans(self, trace: TraceHandle) -> set[str]:
        spans = self._active.get(trace.trace_id)
        if spans is None or trace.root_span_id not in spans:
            raise ValueError("MLflow trace is not active")
        return spans

    def _failed(self, error: Exception) -> None:
        self._failures += 1
        if self._required:
            raise error
        return None

    def _failed_bool(self, error: Exception) -> bool:
        self._failures += 1
        if self._required:
            raise error
        return False


def _attributes(values: dict[str, str]) -> dict[str, str]:
    if not isinstance(values, dict) or len(values) > MAXIMUM_ATTRIBUTES:
        raise ValueError("MLflow trace attributes are outside bounds")
    normalized: dict[str, str] = {}
    for key, value in values.items():
        _name(key, "trace attribute key")
        if not key.startswith("mindclade."):
            raise ValueError("trace attributes must use the mindclade namespace")
        segments = set(key.split("."))
        if segments & _FORBIDDEN_ATTRIBUTE_FRAGMENTS:
            raise ValueError("trace attribute key could carry sensitive payload data")
        normalized[key] = _text(value, "trace attribute value", MAXIMUM_ATTRIBUTE_VALUE_LENGTH)
    return normalized


def _name(value: object, label: str) -> str:
    if not isinstance(value, str) or not _NAME.fullmatch(value):
        raise ValueError(f"{label} is invalid")
    return value


def _text(value: object, label: str, maximum: int) -> str:
    if not isinstance(value, str) or not value or len(value) > maximum:
        raise ValueError(f"{label} is invalid")
    if any(ord(character) < 0x20 for character in value):
        raise ValueError(f"{label} contains control characters")
    return value


def _digest(value: object, label: str) -> str:
    if (
        not isinstance(value, str)
        or len(value) != 71
        or not value.startswith("sha256:")
        or any(character not in "0123456789abcdef" for character in value[7:])
    ):
        raise ValueError(f"{label} is invalid")
    return value


def _timestamp(value: int | None) -> int | None:
    if value is not None and (
        isinstance(value, bool) or not isinstance(value, int) or not 0 <= value < 2**63
    ):
        raise ValueError("trace timestamp is outside bounds")
    return value


def _status(value: str) -> str:
    if value not in {"OK", "ERROR", "UNSET"}:
        raise ValueError("MLflow span status is invalid")
    return value
