# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""One canonical byte encoding, so one document has one digest.

ADR-0004 addresses every dataset, checkpoint, model bundle and evidence record by
the digest of its bytes. That only works if a document has exactly one byte
representation, and before this module it had two: ``libs/python/config`` encoded
with ``ensure_ascii=False`` to UTF-8, while ``preprocessing/cache/keys.py``,
``preprocessing/provenance/manifest.py``, ``data/curation/pipeline.py`` and
``serving/contracts/runtime_manifest.py`` encoded with ``ensure_ascii=True`` to
ASCII. For any document containing a non-ASCII byte — a species name, an author,
a units string — those produce different bytes and therefore different digests
for the same content.

UTF-8 wins, and not by preference. Go's ``encoding/json`` and Rust's
``serde_json`` both emit UTF-8, so the ASCII-escaping form was also the one that
disagreed with the other two languages. A digest Python computes has to equal the
one Go computes over the same document, or the cross-language artifact contract
is decorative.

This module is not RFC 8785/JCS. Strings, integers, booleans, nulls and
containers form the cross-language-compatible subset. Floats are deliberately
prohibited: Python, Go and Rust do not promise the same shortest-number spelling,
so independently encoding a float-bearing identity document could produce a
different digest in each language.

``allow_nan=False`` is set for a related reason: ``NaN`` and ``Infinity`` are not
JSON, Go and Rust both refuse them, and Python's default of emitting them
unquoted produces a document those parsers reject. Failing at encode time turns a
cross-language parse error into a local one.

This package produces **bytes**. Digesting them is
:class:`libs.python.identifiers.Digest`'s job, and keeping the two separate is
what stops this module from growing a second identity vocabulary::

    from libs.python.identifiers import Digest
    from libs.python.serialization import canonical_json_bytes

    digest = Digest.of(canonical_json_bytes(document))
"""

from __future__ import annotations

import json
from collections.abc import Mapping, Sequence
from dataclasses import dataclass
from typing import Any, Final

from libs.python.errors import InvalidArgument, ResourceExhausted

# The delimiters the line-oriented encoding reserves. A field containing either
# could impersonate a structural boundary, which would make two different
# documents encode to identical bytes and seal to the same digest.
LINE_SEPARATOR: Final = "\n"
FIELD_SEPARATOR: Final = "|"
_RESERVED: Final = frozenset({LINE_SEPARATOR, FIELD_SEPARATOR})
MAXIMUM_CANONICAL_JSON_DEPTH: Final = 128
MAXIMUM_CANONICAL_JSON_NODES: Final = 100_000
MAXIMUM_CANONICAL_JSON_BYTES: Final = 8 << 20


def validate_json_nesting(
    value: bytes,
    *,
    maximum_depth: int = MAXIMUM_CANONICAL_JSON_DEPTH,
) -> None:
    """Reject excessive raw JSON nesting before invoking the runtime parser.

    Python's JSON decoder recursion behavior is an implementation detail and changed in
    Python 3.14. Scanning the structural bytes first keeps the platform's depth boundary
    deterministic across interpreter versions without mistaking brackets inside strings
    for containers. Syntax validity remains the JSON decoder's responsibility.
    """
    if not isinstance(value, bytes):
        raise InvalidArgument(
            "JSON input must be bytes",
            reason="canonical_json_input_type",
        )
    if (
        isinstance(maximum_depth, bool)
        or not isinstance(maximum_depth, int)
        or not 1 <= maximum_depth <= MAXIMUM_CANONICAL_JSON_DEPTH
    ):
        raise InvalidArgument(
            f"maximum_depth must be in [1, {MAXIMUM_CANONICAL_JSON_DEPTH}]",
            reason="canonical_json_depth_limit",
        )

    depth = 0
    in_string = False
    escaped = False
    for byte in value:
        if in_string:
            if escaped:
                escaped = False
            elif byte == ord("\\"):
                escaped = True
            elif byte == ord('"'):
                in_string = False
            continue
        if byte == ord('"'):
            in_string = True
        elif byte in (ord("{"), ord("[")):
            depth += 1
            if depth > maximum_depth:
                raise InvalidArgument(
                    "canonical JSON exceeds the maximum nesting depth",
                    reason="canonical_json_depth",
                )
        elif byte in (ord("}"), ord("]")):
            depth -= 1


@dataclass(slots=True)
class _JsonBudget:
    maximum_nodes: int
    maximum_encoded_bytes: int
    nodes: int = 0
    text_bytes: int = 0

    def consume(self) -> None:
        self.nodes += 1
        if self.nodes > self.maximum_nodes:
            raise ResourceExhausted(
                f"canonical JSON exceeds the {self.maximum_nodes}-node budget",
                reason="canonical_json_nodes",
            )

    def consume_text(self, value: str) -> None:
        remaining = self.maximum_encoded_bytes - self.text_bytes
        # Every Unicode scalar consumes at least one UTF-8 byte. This length
        # check prevents allocating a temporary encoding for an obviously
        # oversized value.
        if len(value) > remaining:
            raise ResourceExhausted(
                f"canonical JSON exceeds the {self.maximum_encoded_bytes}-byte encoded budget",
                reason="canonical_json_bytes",
            )
        try:
            encoded_size = len(value.encode("utf-8"))
        except UnicodeEncodeError as error:
            raise InvalidArgument(
                "canonical JSON strings must be valid Unicode scalar values",
                reason="canonical_json_string",
                cause=error,
            ) from error
        self.text_bytes += encoded_size
        if self.text_bytes > self.maximum_encoded_bytes:
            raise ResourceExhausted(
                f"canonical JSON exceeds the {self.maximum_encoded_bytes}-byte encoded budget",
                reason="canonical_json_bytes",
            )


def _json_compatible(
    value: object,
    *,
    active: set[int],
    budget: _JsonBudget,
    depth: int = 0,
) -> object:
    """Return built-in JSON containers after validating the document tree.

    ``json.dumps`` accepts surprising inputs at this boundary: non-string mapping
    keys are coerced to strings, generic mappings fail with a bare ``TypeError``,
    and unsupported values fail differently from non-finite floats.  Normalizing
    first gives callers one controlled failure mode and lets immutable mapping
    implementations participate without sacrificing canonical bytes.
    """
    budget.consume()
    if depth > MAXIMUM_CANONICAL_JSON_DEPTH:
        raise InvalidArgument(
            "canonical JSON exceeds the maximum nesting depth",
            reason="canonical_json_depth",
        )
    if isinstance(value, str):
        budget.consume_text(value)
        return value
    if value is None or isinstance(value, bool | int):
        return value
    if isinstance(value, float):
        raise InvalidArgument(
            "canonical identity documents do not permit floating-point numbers",
            reason="canonical_json_number",
        )

    identity = id(value)
    if identity in active:
        raise InvalidArgument(
            "canonical JSON does not permit circular references",
            reason="canonical_json_cycle",
        )

    if isinstance(value, Mapping):
        active.add(identity)
        try:
            normalized: dict[str, object] = {}
            for key, item in value.items():
                budget.consume()
                if not isinstance(key, str):
                    raise InvalidArgument(
                        "canonical JSON object keys must be strings",
                        reason="canonical_json_key",
                    )
                budget.consume_text(key)
                normalized[key] = _json_compatible(
                    item,
                    active=active,
                    budget=budget,
                    depth=depth + 1,
                )
            return normalized
        finally:
            active.remove(identity)

    if isinstance(value, Sequence) and not isinstance(value, str | bytes | bytearray):
        active.add(identity)
        try:
            return [
                _json_compatible(
                    item,
                    active=active,
                    budget=budget,
                    depth=depth + 1,
                )
                for item in value
            ]
        finally:
            active.remove(identity)

    raise InvalidArgument(
        f"canonical JSON does not support values of type {type(value).__name__}",
        reason="canonical_json_type",
    )


def canonical_json_bytes(
    value: Mapping[str, Any],
    *,
    maximum_nodes: int = MAXIMUM_CANONICAL_JSON_NODES,
    maximum_encoded_bytes: int = MAXIMUM_CANONICAL_JSON_BYTES,
) -> bytes:
    """Encode ``value`` as canonical JSON bytes.

    Keys sorted, no insignificant whitespace, UTF-8, and no floats. The output
    and total document-node counts are bounded while encoding. These settings are the encoding; changing any of them changes every
    digest in the platform, so they are written once here rather than at each
    call site.
    """
    if not isinstance(value, Mapping):
        raise InvalidArgument(
            "canonical JSON documents must be mappings",
            reason="canonical_json_document_type",
        )
    if (
        isinstance(maximum_nodes, bool)
        or not isinstance(maximum_nodes, int)
        or not 1 <= maximum_nodes <= MAXIMUM_CANONICAL_JSON_NODES
    ):
        raise InvalidArgument(
            f"maximum_nodes must be in [1, {MAXIMUM_CANONICAL_JSON_NODES}]",
            reason="canonical_json_node_limit",
        )
    if (
        isinstance(maximum_encoded_bytes, bool)
        or not isinstance(maximum_encoded_bytes, int)
        or not 1 <= maximum_encoded_bytes <= MAXIMUM_CANONICAL_JSON_BYTES
    ):
        raise InvalidArgument(
            f"maximum_encoded_bytes must be in [1, {MAXIMUM_CANONICAL_JSON_BYTES}]",
            reason="canonical_json_byte_limit",
        )
    try:
        normalized = _json_compatible(
            value,
            active=set(),
            budget=_JsonBudget(maximum_nodes, maximum_encoded_bytes),
        )
        encoder = json.JSONEncoder(
            sort_keys=True,
            separators=(",", ":"),
            ensure_ascii=False,
            allow_nan=False,
        )
        encoded_chunks: list[bytes] = []
        encoded_size = 0
        for chunk in encoder.iterencode(normalized):
            encoded = chunk.encode("utf-8")
            encoded_size += len(encoded)
            if encoded_size > maximum_encoded_bytes:
                raise ResourceExhausted(
                    f"canonical JSON exceeds the {maximum_encoded_bytes}-byte encoded budget",
                    reason="canonical_json_bytes",
                )
            encoded_chunks.append(encoded)
        return b"".join(encoded_chunks)
    except (InvalidArgument, ResourceExhausted):
        raise
    except (TypeError, ValueError) as error:  # pragma: no cover - normalization owns these
        raise InvalidArgument(
            "value is not canonically encodable as JSON",
            reason="canonical_json_encode",
            cause=error,
        ) from error


def canonical_field(value: str) -> str:
    """Validate one field of a line-oriented canonical document.

    Rejects the two reserved delimiters. Without this the encoding is not
    injective: a value containing a vertical bar could impersonate a structural
    line and two different documents would seal to the same digest.
    """
    if not isinstance(value, str):
        raise InvalidArgument(
            "canonical field must be text",
            reason="canonical_field_type",
        )
    if not value:
        raise InvalidArgument("canonical field must not be empty", reason="canonical_field_empty")
    if _RESERVED & set(value):
        raise InvalidArgument(
            "canonical field must not contain a reserved delimiter",
            reason="canonical_field_delimiter",
        )
    return value


def canonical_lines(lines: Sequence[str]) -> bytes:
    """Encode a line-oriented canonical document.

    Newline-separated with a trailing newline, UTF-8. Used where a document's
    canonical form is a fixed sequence of lines rather than a JSON object,
    matching the Go writer for ``inference-model-descriptor/v1``.

    Callers are responsible for ordering repeated entries deterministically;
    this function will not sort for them, because only the caller knows which key
    the other language sorts on.
    """
    if not isinstance(lines, Sequence) or isinstance(lines, str | bytes | bytearray):
        raise InvalidArgument(
            "canonical lines must be a sequence of text values",
            reason="canonical_lines_type",
        )
    for line in lines:
        if not isinstance(line, str):
            raise InvalidArgument(
                "canonical line must be text",
                reason="canonical_line_type",
            )
        if LINE_SEPARATOR in line:
            raise InvalidArgument(
                "canonical line must not contain an embedded newline",
                reason="canonical_line_newline",
            )
    return (LINE_SEPARATOR.join(lines) + LINE_SEPARATOR).encode("utf-8")
