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
from typing import Any, Final

from libs.python.errors import InvalidArgument

# The delimiters the line-oriented encoding reserves. A field containing either
# could impersonate a structural boundary, which would make two different
# documents encode to identical bytes and seal to the same digest.
LINE_SEPARATOR: Final = "\n"
FIELD_SEPARATOR: Final = "|"
_RESERVED: Final = frozenset({LINE_SEPARATOR, FIELD_SEPARATOR})


def canonical_json_bytes(value: Mapping[str, Any]) -> bytes:
    """Encode ``value`` as canonical JSON bytes.

    Keys sorted, no insignificant whitespace, UTF-8, and no non-finite floats.
    These four settings are the encoding; changing any of them changes every
    digest in the platform, so they are written once here rather than at each
    call site.
    """
    try:
        return json.dumps(
            value,
            sort_keys=True,
            separators=(",", ":"),
            ensure_ascii=False,
            allow_nan=False,
        ).encode("utf-8")
    except ValueError as error:
        # json raises bare ValueError for out-of-range floats and circular
        # references alike. Re-raised as a fault so a caller sees a code.
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
    for line in lines:
        if LINE_SEPARATOR in line:
            raise InvalidArgument(
                "canonical line must not contain an embedded newline",
                reason="canonical_line_newline",
            )
    return (LINE_SEPARATOR.join(lines) + LINE_SEPARATOR).encode("utf-8")
