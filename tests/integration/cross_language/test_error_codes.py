# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Release-blocking conformance for the fault taxonomy across proto, Go, and Rust.

`protocols/proto/mindclade/common/v1/errors.proto` is the wire authority. Go
(`libs/go/faults`) and Rust (`libs/rust/faults`) each keep a transport-neutral
enum and map at their boundaries, so nothing in a compiler forces the three to
agree. The previous version of this file asserted only that two source files
exist and that the proto text contains the substrings "message" and "Error"; it
compared nothing, and under it the three definitions had already drifted into a
taxonomy that could not round-trip:

  * the proto declared 10 codes, Go 17, and Rust 16;
  * Go emitted "canceled"/"not_implemented" while Rust emitted
    "cancelled"/"unimplemented", and Rust's `FromStr` hard-errored on anything
    it did not recognize, so a Go-emitted code did not parse on the Rust side;
  * Rust had no `Unknown`, which is exactly the code Go emits when
    classification fails;
  * `RetryHint::Never` collapsed to an absent `retry_after_millis`, making
    "never retry" indistinguishable from "no retry information", and Go's
    `with_backoff` had no cross-language representation at all.

These tests read the three definitions mechanically and fail if any one of them
drifts from the other two. They are source-text assertions on purpose: the Go
and Rust enums are not generated from the proto, so a compiled test in either
language can only ever check one side of the contract.
"""

from __future__ import annotations

import re
from pathlib import Path

ROOT = Path(__file__).resolve().parents[3]

PROTO = ROOT / "protocols/proto/mindclade/common/v1/errors.proto"
GO_CODE = ROOT / "libs/go/faults/code.go"
GO_RETRY = ROOT / "libs/go/faults/retry.go"
RUST_CODE = ROOT / "libs/rust/faults/src/code.rs"
RUST_WIRE = ROOT / "libs/rust/faults/src/wire.rs"

# Tags are frozen because `ErrorCode` and `RetryKind` are wire contract: a value
# may be appended, but renumbering or removing one silently reinterprets every
# message already in flight or already persisted.
FROZEN_ERROR_CODE_TAGS = {
    "ERROR_CODE_UNSPECIFIED": 0,
    "ERROR_CODE_INVALID_ARGUMENT": 1,
    "ERROR_CODE_UNAUTHENTICATED": 2,
    "ERROR_CODE_PERMISSION_DENIED": 3,
    "ERROR_CODE_NOT_FOUND": 4,
    "ERROR_CODE_CONFLICT": 5,
    "ERROR_CODE_RESOURCE_EXHAUSTED": 6,
    "ERROR_CODE_DEADLINE_EXCEEDED": 7,
    "ERROR_CODE_UNAVAILABLE": 8,
    "ERROR_CODE_INTERNAL": 9,
}

FROZEN_RETRY_KIND_TAGS = {
    "RETRY_KIND_UNSPECIFIED": 0,
    "RETRY_KIND_NEVER": 1,
    "RETRY_KIND_IMMEDIATE": 2,
    "RETRY_KIND_WITH_BACKOFF": 3,
    "RETRY_KIND_AFTER": 4,
}

# Spellings that one language emitted before the taxonomy was reconciled. Both
# parsers have to keep accepting them: values carrying them are already in logs,
# telemetry attributes, and `Mindclade-Error-Code` headers in flight.
LEGACY_SPELLINGS = {"cancelled", "unimplemented"}


def _strip_comments(text: str) -> str:
    return re.sub(r"//.*", "", text)


def _proto_enum(name: str) -> dict[str, int]:
    """Returns {VALUE_NAME: tag} for a top-level enum in errors.proto."""
    match = re.search(rf"\benum\s+{name}\s*\{{(.*?)\n\}}", PROTO.read_text(), re.S)
    assert match, f"errors.proto declares no `enum {name}`"
    body = _strip_comments(match.group(1))
    return {value: int(tag) for value, tag in re.findall(r"(\w+)\s*=\s*(\d+)\s*;", body)}


def _proto_wire_names(enum: str, prefix: str) -> dict[str, int]:
    """Maps proto value names to their lowercase wire spelling, minus UNSPECIFIED."""
    names: dict[str, int] = {}
    for value, tag in _proto_enum(enum).items():
        assert value.startswith(prefix), f"{value} is missing the `{prefix}` prefix"
        names[value.removeprefix(prefix).lower()] = tag
    assert names.pop("unspecified", None) == 0, f"{enum} must reserve 0 for UNSPECIFIED"
    return names


def _go_string_consts(path: Path, type_name: str) -> dict[str, str]:
    """Returns {identifier suffix: wire string} for a `Name<Suffix> Name = "..."` block."""
    pattern = rf'^\s*{type_name}(\w+)\s+{type_name}\s*=\s*"([a-z_]*)"'
    found = re.findall(pattern, path.read_text(), re.M)
    assert found, f"{path} declares no {type_name} constants"
    return dict(found)


def _rust_block(text: str, needle: str) -> str:
    """Returns the brace-delimited body that follows needle."""
    start = text.find(needle)
    assert start >= 0, f"missing {needle!r}"
    opening = text.index("{", start)
    depth = 0
    for index in range(opening, len(text)):
        if text[index] == "{":
            depth += 1
        elif text[index] == "}":
            depth -= 1
            if depth == 0:
                return text[opening + 1 : index]
    raise AssertionError(f"unbalanced braces after {needle!r}")


def _rust_as_str(path: Path, impl_needle: str) -> dict[str, str]:
    """Returns {variant: wire string} from the as_str match inside an impl block."""
    block = _rust_block(path.read_text(), impl_needle)
    body = _rust_block(block, "fn as_str(self) -> &'static str")
    found = re.findall(r'Self::(\w+)\s*=>\s*"([a-z_]*)"', body)
    assert found, f"{path}: {impl_needle} has no as_str arms"
    return dict(found)


def _rust_from_str_arms() -> dict[str, str]:
    """Returns {accepted wire string: variant} from Code's FromStr match.

    Handles or-patterns, which is how a canonical spelling and its legacy alias
    share one arm without tripping clippy's match_same_arms.
    """
    body = _rust_block(RUST_CODE.read_text(), "fn from_str(")
    arms = re.findall(r'((?:"[a-z_]*"\s*\|\s*)*"[a-z_]*")\s*=>\s*Self::(\w+)', body)
    assert arms, "libs/rust/faults/src/code.rs: FromStr has no arms"
    return {
        literal: variant
        for patterns, variant in arms
        for literal in re.findall(r'"([a-z_]*)"', patterns)
    }


def test_proto_go_and_rust_declare_one_error_code_set() -> None:
    proto = set(_proto_wire_names("ErrorCode", "ERROR_CODE_"))
    go = set(_go_string_consts(GO_CODE, "Code").values())
    rust = set(_rust_as_str(RUST_CODE, "impl Code").values())

    assert proto == go, (
        "proto and Go disagree on the code set; "
        f"proto-only={sorted(proto - go)} go-only={sorted(go - proto)}"
    )
    assert proto == rust, (
        "proto and Rust disagree on the code set; "
        f"proto-only={sorted(proto - rust)} rust-only={sorted(rust - proto)}"
    )


def test_error_code_tags_are_frozen() -> None:
    declared = _proto_enum("ErrorCode")
    for value, tag in FROZEN_ERROR_CODE_TAGS.items():
        assert declared.get(value) == tag, f"{value} moved from tag {tag} to {declared.get(value)}"
    tags = sorted(declared.values())
    assert len(tags) == len(set(tags)), f"duplicate ErrorCode tags: {tags}"


def test_go_and_rust_accept_the_same_wire_spellings() -> None:
    canonical = set(_go_string_consts(GO_CODE, "Code").values())

    table = re.search(r"var codeAliases = map\[string\]Code\{(.*?)\n\}", GO_CODE.read_text(), re.S)
    assert table, "libs/go/faults/code.go declares no codeAliases table"
    go_accepted = canonical | set(re.findall(r'"([a-z_]+)"\s*:', table.group(1)))
    rust_accepted = set(_rust_from_str_arms())

    assert go_accepted == rust_accepted, (
        "Go and Rust accept different spellings; "
        f"go-only={sorted(go_accepted - rust_accepted)} "
        f"rust-only={sorted(rust_accepted - go_accepted)}"
    )
    accepted_aliases = go_accepted - canonical
    assert accepted_aliases >= LEGACY_SPELLINGS, (
        "both pre-reconciliation spellings must still parse on both sides; "
        f"missing={sorted(LEGACY_SPELLINGS - accepted_aliases)}"
    )


def test_the_catch_all_code_survives_the_boundary() -> None:
    """Rust must accept the code Go emits when classification fails, and never hard-fail."""
    assert "unknown" in set(_go_string_consts(GO_CODE, "Code").values()), (
        "Go lost its catch-all code"
    )
    assert _rust_from_str_arms().get("unknown") == "Unknown", (
        "Rust's FromStr does not accept Go's catch-all code"
    )

    text = RUST_CODE.read_text()
    assert "pub fn from_wire(" in text, (
        "libs/rust/faults/src/code.rs has no total ingestion path; a peer code that "
        "post-dates this build would hard-fail instead of degrading to Unknown"
    )
    assert "Self::Unknown" in _rust_block(text, "pub fn from_wire("), (
        "from_wire must land unrecognized peer codes on Unknown"
    )
    assert "Code::from_wire(" in RUST_WIRE.read_text(), (
        "the wire ingestion path must use the total parser, not the strict one"
    )


def test_retry_semantics_have_one_wire_representation() -> None:
    proto = set(_proto_wire_names("RetryKind", "RETRY_KIND_")) | {""}
    go = set(_go_string_consts(GO_RETRY, "RetryKind").values())
    rust = set(_rust_as_str(RUST_WIRE, "impl WireRetryKind").values())

    assert proto == go, (
        f"proto and Go disagree on retry kinds; proto-only={sorted(proto - go)} "
        f"go-only={sorted(go - proto)}"
    )
    assert proto == rust, (
        f"proto and Rust disagree on retry kinds; proto-only={sorted(proto - rust)} "
        f"rust-only={sorted(rust - proto)}"
    )
    # The two properties the collapsed representation could not express.
    assert "never" in proto, "an explicit refusal to retry must be distinguishable from silence"
    assert "with_backoff" in proto, "Go's with_backoff must have a cross-language representation"


def test_retry_kind_tags_are_frozen() -> None:
    declared = _proto_enum("RetryKind")
    for value, tag in FROZEN_RETRY_KIND_TAGS.items():
        assert declared.get(value) == tag, f"{value} moved from tag {tag} to {declared.get(value)}"


def test_error_detail_carries_retry_kind_beside_the_delay() -> None:
    body = re.search(r"\bmessage ErrorDetail\s*\{(.*?)\n\}", PROTO.read_text(), re.S)
    assert body, "errors.proto declares no `message ErrorDetail`"
    fields = _strip_comments(body.group(1))
    assert re.search(r"\boptional uint64 retry_after_millis\s*=\s*5\s*;", fields), (
        "retry_after_millis moved off tag 5"
    )
    assert re.search(r"\bRetryKind retry_kind\s*=\s*6\s*;", fields), (
        "ErrorDetail carries a delay but no kind, so a receiver cannot tell an explicit "
        "'never retry' from an omitted hint"
    )
