# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Three-language conformance for resource kinds and resource identifiers.

What this replaces: a single test that re-declared the ID grammar as a local
regular expression and matched one fixture string against it. It passed whether
or not Go, Rust and Python agreed with each other or with the regex, because it
never consulted any of them — the same shape as the vacuous
``test_error_codes.py`` next door, which asserts that two source files exist.

What it does instead. The contract is stated once, at the top, and then:

* every accepted and rejected vector runs through the real Python
  implementation, so Python's behaviour is checked rather than described;
* the named grammar constants are read out of the Go and Rust sources and
  required to equal it, so a language that renumbers its bounds fails here;
* Rust's own conformance vectors (``libs/rust/identifiers/tests/goldens.rs``)
  must be exactly this table, so a vector added on one side of the boundary and
  not the other fails here;
* Go's kind test vectors must classify the same way, so a Go grammar change
  large enough to move its own tests is caught.

The divergence that motivated this: Rust's ``ResourceKind`` carried a second,
laxer grammar than its own ``ResourceId`` — 1 to 48 bytes, a leading digit
allowed, and ``_`` and ``-`` allowed after the first character. It accepted
``runtime_host``, whose identifier ``runtime_host_<32 hex>`` is rejected by
Rust's own parser, by ``libs/go/identifiers.ParseID`` and by
``libs/python/identifiers.ResourceId.parse``, because ``_`` is the separator.
Nothing in this directory noticed.

Source inspection rather than execution is deliberate. This module runs in the
Python lane — ``uv run --frozen pytest`` — which has no Go and no Rust toolchain,
and shelling out to one would make the test skip wherever that toolchain is
absent, which is the same as not having the test. It is the technique
``test_wire_compatibility.py`` already uses on ``.proto`` files. The behavioural
half of each language's conformance runs in that language's own suite:
``libs/go/identifiers/kind_test.go``, ``libs/rust/identifiers/tests/goldens.rs``,
and the vectors below.
"""

from __future__ import annotations

import json
import re
from pathlib import Path

import pytest

from libs.python.errors import InvalidArgument
from libs.python.identifiers import (
    ID_SEPARATOR,
    MAXIMUM_KIND_LENGTH,
    MINIMUM_KIND_LENGTH,
    UUID_COMPACT_LENGTH,
    ResourceId,
    is_canonical_kind,
    is_canonical_resource_id,
    parse_kind,
)

ROOT = Path(__file__).resolve().parents[3]
FIXTURE = Path(__file__).parent / "fixtures" / "primitives_v1.json"

GO_KIND = ROOT / "libs/go/identifiers/kind.go"
GO_ID = ROOT / "libs/go/identifiers/id.go"
GO_UUID = ROOT / "libs/go/identifiers/uuid.go"
GO_KIND_TEST = ROOT / "libs/go/identifiers/kind_test.go"
RUST_ID = ROOT / "libs/rust/identifiers/src/resource_id.rs"
RUST_GOLDENS = ROOT / "libs/rust/identifiers/tests/goldens.rs"

# The contract, stated once. Every language below is required to agree.
MINIMUM_KIND = 2
MAXIMUM_KIND = 24
BODY_LENGTH = 32
SEPARATOR = "_"

# Kinds every language must accept.
ACCEPTED_KINDS = ("ab", "run", "org", "model2", "runtimehost", "a1b2c3")

# Kinds every language must reject. `run_id`, `run-id` and `runtime_host` are the
# ones Rust used to accept; `a` is the one-character case it also used to accept.
REJECTED_KINDS = (
    "",
    "a",
    "1run",
    "Run",
    "run_id",
    "run-id",
    "runtime_host",
    " run",
    "run.id",
    "rūn",
)

# A canonical RFC-variant UUIDv7 body: byte 6 high nibble is 7, byte 8 top two
# bits are 10.
BODY = "019c0000000070008000000000000001"

ACCEPTED_IDS = (
    f"run{SEPARATOR}{BODY}",
    f"ab{SEPARATOR}{BODY}",
    f"{'a' * MAXIMUM_KIND}{SEPARATOR}{BODY}",
)

REJECTED_IDS = (
    "",
    BODY,  # no separator at all
    f"run{SEPARATOR}{BODY.upper()}",  # uppercase payload is a second spelling
    f"run{SEPARATOR}{BODY[:-1]}",  # short payload
    f"run{SEPARATOR}{BODY}0",  # long payload
    f"run{SEPARATOR}stage{SEPARATOR}{BODY}",  # two separators
    f"runtime_host{SEPARATOR}{BODY}",  # separator inside the kind
    f"Run{SEPARATOR}{BODY}",  # uppercase kind
    f"a{SEPARATOR}{BODY}",  # kind below the minimum
    f"{'a' * (MAXIMUM_KIND + 1)}{SEPARATOR}{BODY}",  # kind above the maximum
    f"run{SEPARATOR}019c0000000040008000000000000001",  # UUIDv4 payload
    f"run{SEPARATOR}019c0000000070000000000000000001",  # non-RFC variant payload
)

# Payloads rejected only once the UUID bits are inspected. `is_canonical_*`
# checks shape alone and is documented to say so, so it must accept these while
# `ResourceId.parse` rejects them.
SHAPE_ONLY_IDS = (
    f"run{SEPARATOR}019c0000000040008000000000000001",
    f"run{SEPARATOR}019c0000000070000000000000000001",
)


def _int_constant(path: Path, name: str) -> int:
    """Read a named integer constant out of a Go or Rust source file."""
    match = re.search(rf"\b{name}\b[^=\n]*=\s*(\d+)", path.read_text(encoding="utf-8"))
    assert match, f"{path.relative_to(ROOT)} no longer declares {name}"
    return int(match.group(1))


def _char_constant(path: Path, name: str) -> str:
    """Read a named single-quoted character constant (Go rune, Rust char)."""
    match = re.search(rf"\b{name}\b[^=\n]*=\s*'(.)'", path.read_text(encoding="utf-8"))
    assert match, f"{path.relative_to(ROOT)} no longer declares {name}"
    return match.group(1)


def _rust_string_slice(path: Path, name: str) -> tuple[str, ...]:
    """Read the literals out of a `const NAME: &[&str] = &[ ... ];` declaration."""
    text = path.read_text(encoding="utf-8")
    match = re.search(rf"const\s+{name}\s*:\s*&\[&str\]\s*=\s*&\[(.*?)\];", text, re.DOTALL)
    assert match, f"{path.relative_to(ROOT)} no longer declares {name}"
    return tuple(re.findall(r'"((?:[^"\\]|\\.)*)"', match.group(1)))


def _go_string_slice(path: Path, name: str) -> tuple[str, ...]:
    """Read the string literals out of a `name := []string{...}` declaration.

    Entries that are not bare literals are skipped rather than scraped: Go's kind
    test builds its boundary cases with `strings.Repeat("a", MaximumKindLength)`,
    and pulling the `"a"` out of that call would read a length case as the
    one-character kind `a`, which the contract rejects. The length bounds are
    pinned separately, by comparing the named constants.
    """
    text = path.read_text(encoding="utf-8")
    match = re.search(rf"\b{name}\s*:?=\s*\[\]string\{{(.*?)\}}\n", text, re.DOTALL)
    assert match, f"{path.relative_to(ROOT)} no longer declares {name}"
    literal = re.compile(r'^"((?:[^"\\]|\\.)*)"$')
    entries = []
    for element in match.group(1).split(","):
        if found := literal.fullmatch(element.strip()):
            entries.append(found.group(1))
    assert entries, f"{path.relative_to(ROOT)} declares {name} with no string literals"
    return tuple(entries)


def test_python_declares_the_contract() -> None:
    assert MINIMUM_KIND_LENGTH == MINIMUM_KIND
    assert MAXIMUM_KIND_LENGTH == MAXIMUM_KIND
    assert UUID_COMPACT_LENGTH == BODY_LENGTH
    assert ID_SEPARATOR == SEPARATOR


def test_python_bounds_match_the_constants_it_publishes() -> None:
    # Python states the bounds twice — as constants, and inside the compiled
    # patterns that actually decide. Exercising the boundaries ties the two
    # together without pinning the regex spelling, which an equivalent rewrite
    # would change without changing a single accept/reject decision.
    assert is_canonical_kind("a" * MINIMUM_KIND)
    assert not is_canonical_kind("a" * (MINIMUM_KIND - 1))
    assert is_canonical_kind("a" * MAXIMUM_KIND)
    assert not is_canonical_kind("a" * (MAXIMUM_KIND + 1))
    assert is_canonical_resource_id(f"run{SEPARATOR}{'0' * BODY_LENGTH}")
    assert not is_canonical_resource_id(f"run{SEPARATOR}{'0' * (BODY_LENGTH - 1)}")
    assert not is_canonical_resource_id(f"run{SEPARATOR}{'0' * (BODY_LENGTH + 1)}")


def test_go_declares_the_contract() -> None:
    assert _int_constant(GO_KIND, "MinimumKindLength") == MINIMUM_KIND
    assert _int_constant(GO_KIND, "MaximumKindLength") == MAXIMUM_KIND
    assert _int_constant(GO_UUID, "UUIDCompactLength") == BODY_LENGTH
    assert _char_constant(GO_ID, "IDSeparator") == SEPARATOR


def test_rust_declares_the_contract() -> None:
    assert _int_constant(RUST_ID, "MINIMUM_KIND_LENGTH") == MINIMUM_KIND
    assert _int_constant(RUST_ID, "MAXIMUM_KIND_LENGTH") == MAXIMUM_KIND
    assert _int_constant(RUST_ID, "ID_BODY_LENGTH") == BODY_LENGTH
    assert _char_constant(RUST_ID, "ID_SEPARATOR") == SEPARATOR


def test_rust_runs_exactly_these_kind_vectors() -> None:
    # Not "a superset": a vector added on one side of the language boundary and
    # not the other is precisely how the two grammars drifted apart.
    assert _rust_string_slice(RUST_GOLDENS, "ACCEPTED") == ACCEPTED_KINDS
    assert _rust_string_slice(RUST_GOLDENS, "REJECTED") == REJECTED_KINDS


def test_go_classifies_its_kind_vectors_the_same_way() -> None:
    for value in _go_string_slice(GO_KIND_TEST, "valid"):
        assert value in ACCEPTED_KINDS, f"Go accepts kind {value!r} that this contract does not"
    for value in _go_string_slice(GO_KIND_TEST, "invalid"):
        assert value in REJECTED_KINDS, f"Go rejects kind {value!r} that this contract does not"


@pytest.mark.parametrize("value", ACCEPTED_KINDS)
def test_python_accepts_shared_kind(value: str) -> None:
    assert is_canonical_kind(value)
    assert parse_kind(value) == value


@pytest.mark.parametrize("value", REJECTED_KINDS)
def test_python_rejects_shared_kind(value: str) -> None:
    assert not is_canonical_kind(value)
    with pytest.raises(InvalidArgument):
        parse_kind(value)


@pytest.mark.parametrize("value", ACCEPTED_IDS)
def test_python_accepts_shared_id(value: str) -> None:
    assert is_canonical_resource_id(value)
    identifier = ResourceId.parse(value)
    assert identifier.text == value
    assert identifier.kind == value.split(SEPARATOR, 1)[0]
    assert len(identifier.body) == BODY_LENGTH


@pytest.mark.parametrize("value", REJECTED_IDS)
def test_python_rejects_shared_id(value: str) -> None:
    if value not in SHAPE_ONLY_IDS:
        assert not is_canonical_resource_id(value)
    with pytest.raises(InvalidArgument):
        ResourceId.parse(value)


def test_fixture_round_trips_through_the_python_implementation() -> None:
    data = json.loads(FIXTURE.read_text(encoding="utf-8"))
    identifier = ResourceId.parse(data["resource_id"])
    assert identifier.kind == data["resource_id_kind"]
    assert identifier.text == data["resource_id"]
    # UUIDv7 version and RFC variant, which is what the previous test checked by
    # hand against a locally re-declared regular expression.
    assert identifier.raw[6] >> 4 == 7
    assert identifier.raw[8] & 0xC0 == 0x80
