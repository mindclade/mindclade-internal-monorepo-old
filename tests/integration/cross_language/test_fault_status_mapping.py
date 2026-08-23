# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Release-blocking conformance for how a fault renders on the wire.

`test_error_codes.py` pins the fault *taxonomy* across proto, Go, and Rust. It
says nothing about what a client actually receives, and that is where the two
halves of the fleet diverged: Go rendered `not_found` as HTTP 404 through
`libs/go/httpx`, while `services/runtime_gateway` rendered the same fault as
HTTP 500 and `services/ai_gateway_proxy` rendered it as 404. One taxonomy, three
answers. A 500 tells a client to retry and pages an operator; a 404 tells it the
request was wrong. Getting that backwards is an availability-signal defect as
much as a debuggability one.

`libs/go/httpx/codes.go` and `libs/go/grpcx/codes.go` were already the fleet's
mapping and predate every Rust edge, so `libs/rust/faults/src/status.rs` is a
mirror of them rather than a second opinion. Nothing in a compiler compares the
two — the Go and Rust tables are hand-written in different languages — so these
tests read both mechanically and fail when either moves.

Source-text assertions on purpose, for the same reason as `test_error_codes.py`:
a compiled test in either language can only ever check its own side.
"""

from __future__ import annotations

import http
import re
from pathlib import Path

ROOT = Path(__file__).resolve().parents[3]

RUST_CODE = ROOT / "libs/rust/faults/src/code.rs"
RUST_STATUS = ROOT / "libs/rust/faults/src/status.rs"
GO_HTTP = ROOT / "libs/go/httpx/codes.go"
GO_GRPC = ROOT / "libs/go/grpcx/codes.go"

# Every Rust transport edge that renders a fault for a client. Each one used to
# carry its own `match error.code()`, and that duplication is what let two of
# them drift; `test_no_edge_keeps_a_private_status_table` holds the line.
RUST_EDGES = (
    ROOT / "services/runtime_gateway/src/network.rs",
    ROOT / "services/runtime_gateway/src/grpc.rs",
    ROOT / "services/runtime_host/src/grpc.rs",
    ROOT / "services/ai_gateway_proxy/src/server.rs",
)

# Go and Rust spell two codes differently in source while agreeing on the wire
# ("canceled", "not_implemented"). The wire spelling is the authority, so both
# sides are normalized onto it before anything is compared. `test_error_codes.py`
# is what keeps the wire spellings themselves honest.
FAULT_ALIASES = {"cancelled": "canceled", "unimplemented": "not_implemented"}

# gRPC's own name for the code is `Unimplemented` in both `grpc/codes` and
# `tonic`, so the fault-level alias above must not be applied to gRPC names.
# Only the cancellation spelling differs, and only in the Rust constant.
GRPC_ALIASES = {"cancelled": "canceled"}

# A 500 asserts the server failed. Exactly three codes may claim that; every
# other code describes the request, and rendering one of those as 500 tells a
# client to retry something that will never succeed and pages an operator for it.
SERVER_SIDE_FAULTS = frozenset({"internal", "data_loss", "unknown"})


def _snake(name: str) -> str:
    return re.sub(r"(?<!^)(?=[A-Z])", "_", name).lower()


def _fault_key(name: str) -> str:
    key = _snake(name)
    return FAULT_ALIASES.get(key, key)


def _grpc_key(name: str) -> str:
    key = _snake(name) if not name.isupper() else name.lower()
    return GRPC_ALIASES.get(key, key)


def _body(text: str, opener: str, terminator: str = "\n}") -> str:
    """Returns the source between `opener` and the next column-0 terminator."""
    start = text.index(opener) + len(opener)
    end = text.index(terminator, start)
    return text[start:end]


def _rust_fault_codes() -> set[str]:
    body = _body(RUST_CODE.read_text(encoding="utf-8"), "pub enum Code {")
    body = re.sub(r"//.*", "", body)
    return {_fault_key(name) for name in re.findall(r"^ {4}(\w+),$", body, re.MULTILINE)}


def _rust_declared_all() -> list[str]:
    body = _body(RUST_STATUS.read_text(encoding="utf-8"), "pub const ALL: &[Code] = &[", "\n];")
    return [_fault_key(name) for name in re.findall(r"Code::(\w+)", body)]


def _rust_arms(function: str) -> dict[str, str]:
    """Returns {fault key: rendered token} for one wildcard-free Rust match."""
    text = RUST_STATUS.read_text(encoding="utf-8")
    body = _body(text, f"pub const fn {function}(code: Code)")
    body = re.sub(r"//.*", "", body)
    rendered: dict[str, str] = {}
    for arm, value in re.findall(r"((?:\s*Code::\w+\s*\|?)+)=>\s*([\w]+),", body):
        for name in re.findall(r"Code::(\w+)", arm):
            rendered[_fault_key(name)] = value
    return rendered


def _rust_http() -> dict[str, int]:
    return {code: int(value) for code, value in _rust_arms("http_status").items()}


def _rust_grpc() -> dict[str, str]:
    text = RUST_STATUS.read_text(encoding="utf-8")
    constants = dict(re.findall(r"const (GRPC_\w+): i32 = (\d+);", text))
    rendered = _rust_arms("grpc_code")
    assert set(rendered.values()) <= set(constants), (
        "grpc_code returns a token that is not a declared GRPC_* constant"
    )
    return {code: _grpc_key(name.removeprefix("GRPC_")) for code, name in rendered.items()}


def _go_switch(path: Path, opener: str, prefix: str) -> dict[str, str]:
    """Returns {fault key: rendered token} for a Go `switch` over fault codes.

    A code with no explicit `case` takes the `default` arm — `grpcx` relies on
    that for `CodeUnknown` — so the default is resolved for every code the
    taxonomy declares rather than only for the ones Go happens to name.
    """
    body = _body(path.read_text(encoding="utf-8"), opener)
    rendered: dict[str, str] = {}
    pending: list[str] = []
    default: str | None = None
    in_default = False
    for line in body.splitlines():
        line = line.strip()
        if line.startswith("case "):
            pending = [_fault_key(name) for name in re.findall(r"faults\.Code(\w+)", line)]
            in_default = False
        elif line == "default:":
            pending = []
            in_default = True
        elif line.startswith("return "):
            token = line.removeprefix("return ").strip()
            assert token.startswith(prefix), f"{path.name}: unexpected return {token!r}"
            token = token.removeprefix(prefix)
            if in_default:
                default = token
            for code in pending:
                rendered[code] = token
            pending = []
    assert default is not None, f"{path.name}: {opener} has no default arm"
    for code in _rust_fault_codes():
        rendered.setdefault(code, default)
    return rendered


def _go_http() -> dict[str, int]:
    switch = _go_switch(GO_HTTP, "func StatusFromCode(code faults.Code) int {", "http.Status")
    return {code: int(http.HTTPStatus[_snake(name).upper()]) for code, name in switch.items()}


def _go_grpc() -> dict[str, str]:
    switch = _go_switch(GO_GRPC, "func CodeFromFault(code faults.Code) codes.Code {", "codes.")
    return {code: _grpc_key(name) for code, name in switch.items()}


def test_the_parsers_see_the_whole_taxonomy() -> None:
    """Guards every other test here: a silent parse miss would pass vacuously."""
    codes = _rust_fault_codes()
    assert len(codes) == 17, f"expected 17 fault codes, parsed {sorted(codes)}"
    declared = _rust_declared_all()
    assert len(declared) == len(set(declared)), "status::ALL repeats a code"
    assert set(declared) == codes, "status::ALL does not cover the taxonomy"
    for parsed, label in ((_rust_http(), "http_status"), (_rust_grpc(), "grpc_code")):
        assert set(parsed) == codes, f"{label} does not name every code"
    for parsed, label in ((_go_http(), "httpx"), (_go_grpc(), "grpcx")):
        assert set(parsed) == codes, f"{label} does not resolve every code"


def test_rust_and_go_render_the_same_http_status_for_every_fault_code() -> None:
    rust, go = _rust_http(), _go_http()
    divergent = {code: (rust[code], go[code]) for code in rust if rust[code] != go[code]}
    assert not divergent, f"Rust and Go disagree on HTTP status: {divergent}"


def test_rust_and_go_render_the_same_grpc_code_for_every_fault_code() -> None:
    rust, go = _rust_grpc(), _go_grpc()
    divergent = {code: (rust[code], go[code]) for code in rust if rust[code] != go[code]}
    assert not divergent, f"Rust and Go disagree on gRPC code: {divergent}"


def test_only_server_side_faults_render_500() -> None:
    """The defect this file exists for, asserted on the value rather than the pair.

    Both tables agreeing is not enough on its own: they could agree on a wrong
    answer, which is exactly what shipped when `not_found` rendered as 500.
    """
    for label, table in (("Rust", _rust_http()), ("Go", _go_http())):
        rendered_500 = {code for code, value in table.items() if value == 500}
        assert rendered_500 == SERVER_SIDE_FAULTS, (
            f"{label} renders {sorted(rendered_500)} as HTTP 500"
        )


def test_not_found_is_reported_as_a_missing_resource_everywhere() -> None:
    assert _rust_http()["not_found"] == 404
    assert _go_http()["not_found"] == 404
    assert _rust_grpc()["not_found"] == "not_found"
    assert _go_grpc()["not_found"] == "not_found"


def test_no_fault_renders_as_a_grpc_success() -> None:
    for label, table in (("Rust", _rust_grpc()), ("Go", _go_grpc())):
        assert "ok" not in table.values(), f"{label} renders a fault as a success"


def test_the_rust_tables_have_no_wildcard_arm() -> None:
    """The structural half of the fix.

    `Code` is no longer `#[non_exhaustive]`, so these two matches are the point
    at which a newly added fault code stops compiling. A wildcard arm here would
    restore the fail-open behaviour that let four edges render an unhandled code
    as 500 without anyone noticing.
    """
    text = RUST_STATUS.read_text(encoding="utf-8")
    for function in ("http_status", "grpc_code"):
        body = _body(text, f"pub const fn {function}(code: Code)")
        assert "_ =>" not in body, f"{function} has a wildcard arm"
    # An attribute line, not a mention: `code.rs` documents at length why the
    # attribute was removed, and that prose must not read as the attribute.
    attributes = [
        line.strip()
        for line in RUST_CODE.read_text(encoding="utf-8").splitlines()
        if line.strip().startswith("#[")
    ]
    assert "#[non_exhaustive]" not in attributes, (
        "Code is #[non_exhaustive] again, which forces a wildcard arm on every edge"
    )


def test_no_edge_keeps_a_private_status_table() -> None:
    """Duplication is how the two gateways drifted; this is what stops a third.

    An edge converts the canonical number into its transport's type. The moment
    one switches on `error.code()` itself it has an opinion of its own, and
    nothing compares that opinion to anything.
    """
    for edge in RUST_EDGES:
        source = edge.read_text(encoding="utf-8")
        assert "match error.code()" not in source, (
            f"{edge.relative_to(ROOT)} matches on the fault code directly instead of "
            "rendering through mindclade_faults::status"
        )
        assert "mindclade_faults::status" in source or "status::" in source, (
            f"{edge.relative_to(ROOT)} does not render through mindclade_faults::status"
        )
