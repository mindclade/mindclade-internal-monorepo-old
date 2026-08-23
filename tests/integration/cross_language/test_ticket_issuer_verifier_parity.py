# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""The Go issuer may never mint an execution ticket the Rust verifier always rejects.

`control/runtime_authority` signs execution tickets; `libs/rust/worker_protocol` verifies them
on the node. Both are hand-written projections of `mindclade.runtime.v1.ExecutionTicket`, and
neither build sees the other. The verifier was strictly stricter than the issuer on seven
invariants, so the control plane could mint a well-formed, correctly signed ticket that every
node refuses. It fails closed, so it is not a privilege hole -- it surfaces as an unexplained
rejection at lease time, arbitrarily far from the mint that caused it, with no test in either
language able to say why.

The same asymmetry existed in `ArtifactGrant`: Rust bounds both collections and validates every
writable namespace, Go bounded neither. CLAUDE.md requires bounded collections on both sides of
a seam, and an unbounded issuer-side set is the side that gets to allocate first.

Most of this gate is *derived*, not tabulated: every non-zero requirement and every length bound
the Rust verifier states is extracted from its source and demanded of the Go issuer. A new bound
added to Rust therefore fails here until Go grows it too. Only the invariants with no mechanical
shape -- "some work identity is present", "an optional id has the right resource kind", and the
namespace-escape rules -- are named explicitly.
"""

from __future__ import annotations

import re
from pathlib import Path

import pytest

ROOT = Path(__file__).resolve().parents[3]
RUST = ROOT / "libs/rust/worker_protocol/src/lib.rs"
GO_TICKET = ROOT / "control/runtime_authority/execution_ticket.go"
GO_GRANT = ROOT / "control/runtime_authority/artifact_grant.go"

# Rust field -> Go field. Only the identifier spellings differ; the invariants must not.
GO_NAMES = {
    "issuer": "Issuer",
    "attempt": "Attempt",
    "execution_class": "ExecutionClass",
    "accelerator_capability": "AcceleratorCapability",
    "idempotency_key": "IdempotencyKey",
    "policy_epoch": "PolicyEpoch",
    "route_snapshot_version": "RouteSnapshotVersion",
    "revocation_epoch": "RevocationEpoch",
    "readable_digests": "ReadableDigests",
    "writable_namespaces": "WritableNamespaces",
}

# Invariants with no mechanical shape to extract. Each entry is (description, rust, go).
STRUCTURAL_INVARIANTS = (
    (
        "a ticket must identify some unit of work",
        "self.run_id.is_none()",
        'c.RunID == "" && c.JobID == "" && c.StageID == "" && c.RequestID == ""',
    ),
    (
        "an optional work id must carry its own resource kind",
        "id.kind() != kind",
        "validateCanonicalID(value, name)",
    ),
    (
        "a writable namespace may not escape its prefix",
        'value.contains("..")',
        'strings.Contains(namespace, "..")',
    ),
    (
        "a writable namespace may not be absolute",
        "value.starts_with('/')",
        'strings.HasPrefix(namespace, "/")',
    ),
)

_RUST_CONST = re.compile(r"^const\s+(\w+):\s*usize\s*=\s*([\d_]+);", re.MULTILINE)


def _balanced_body(source: str, opening: int, what: str) -> str:
    depth, index = 1, opening + 1
    while index < len(source) and depth:
        depth += {"{": 1, "}": -1}.get(source[index], 0)
        index += 1
    assert depth == 0, f"unbalanced braces reading {what}"
    return source[opening + 1 : index - 1]


def _rust_body(impl_type: str, name: str) -> str:
    """The body of one Rust `fn`, scoped to its `impl` block.

    Scoping matters: `validate` is declared on four types in this crate, and the first one in
    the file is `DetachedSignature`. Searching the whole file made the `ArtifactGrant` half of
    this gate compare Go against the wrong Rust function and pass while proving nothing.
    """

    source = RUST.read_text()
    block = _balanced_body(
        source, source.index("{", source.index(f"impl {impl_type} {{")), f"impl {impl_type}"
    )
    start = block.index(f"pub fn {name}(&self)")
    return _balanced_body(block, block.index("{", start), f"Rust fn {impl_type}::{name}")


def _go_body(path: Path, receiver: str, name: str) -> str:
    source = path.read_text()
    signature = f"func ({receiver}) {name}() error {{"
    start = source.index(signature)
    return _balanced_body(source, source.index("{", start + len(signature) - 1), f"Go func {name}")


_GO_CONST = re.compile(r"^\s*(\w+)\s+=\s+(\d[\d_]*)\s*$", re.MULTILINE)


def _constants() -> dict[str, int]:
    return {
        name: int(value.replace("_", "")) for name, value in _RUST_CONST.findall(RUST.read_text())
    }


def _go_expanded(body: str) -> str:
    """`body` with the package's integer constants substituted for their values.

    Both sides name their bounds rather than repeating magic numbers, so comparing the *values*
    is the only way to notice a Go bound that silently drifts to a different constant.
    """

    constants: dict[str, int] = {}
    for path in sorted(GO_TICKET.parent.glob("*.go")):
        constants.update(
            {
                name: int(value.replace("_", ""))
                for name, value in _GO_CONST.findall(path.read_text())
            }
        )
    return re.sub(r"\b[A-Za-z_]\w*\b", lambda m: str(constants.get(m.group(0), m.group(0))), body)


def _rust_non_zero(body: str) -> set[str]:
    return set(re.findall(r"self\.(\w+)\s*==\s*0\b", body))


def _rust_length_bounds(body: str, constants: dict[str, int]) -> dict[str, int]:
    bounds: dict[str, int] = {}
    for field, limit in re.findall(r"self\.(\w+)\.len\(\)\s*>\s*([\w_]+)", body):
        bounds[field] = constants.get(limit, 0) or int(limit.replace("_", ""))
    return bounds


TICKET = (
    GO_TICKET,
    "c ExecutionTicketClaims",
    "ValidateStatic",
    "ExecutionTicketClaims",
    "validate_static",
)
GRANT = (GO_GRANT, "g ArtifactGrant", "Validate", "ArtifactGrant", "validate")


def test_rust_verifier_parsing_is_not_vacuous() -> None:
    # Every assertion below has the form "the Go body contains what the Rust body states". If
    # the Rust extraction silently returned nothing, all of them would pass while proving
    # nothing, so the extraction itself is pinned first.
    constants = _constants()
    ticket = _rust_body("ExecutionTicketClaims", "validate_static")
    grant = _rust_body("ArtifactGrant", "validate")
    assert _rust_non_zero(ticket) >= {"attempt", "policy_epoch", "revocation_epoch"}
    assert set(_rust_length_bounds(ticket, constants)) >= {"issuer", "execution_class"}
    assert set(_rust_length_bounds(grant, constants)) >= {
        "readable_digests",
        "writable_namespaces",
    }
    assert constants.get("MAX_SET_ENTRIES", 0) > 0


def test_go_issuer_requires_every_non_zero_field_the_rust_verifier_does() -> None:
    rust_body = _rust_body("ExecutionTicketClaims", "validate_static")
    go_body = _go_body(GO_TICKET, "c ExecutionTicketClaims", "ValidateStatic")
    missing = sorted(
        field
        for field in _rust_non_zero(rust_body)
        if field in GO_NAMES and f"c.{GO_NAMES[field]} == 0" not in go_body
    )
    assert not missing, (
        "the Rust verifier rejects a zero value for these ticket fields but the Go issuer mints "
        f"them anyway: rust-only={missing}. Every such ticket is signed, delivered, and then "
        "refused by every node."
    )


@pytest.mark.parametrize(
    ("path", "receiver", "method", "rust_type", "rust_fn"), [TICKET, GRANT], ids=["ticket", "grant"]
)
def test_go_issuer_bounds_everything_the_rust_verifier_bounds(
    path: Path, receiver: str, method: str, rust_type: str, rust_fn: str
) -> None:
    constants = _constants()
    rust_body = _rust_body(rust_type, rust_fn)
    go_body = _go_expanded(_go_body(path, receiver, method))
    missing = []
    for field, limit in sorted(_rust_length_bounds(rust_body, constants).items()):
        if field not in GO_NAMES:
            continue
        # Go spells the same bound `len(x.Field) > N`. The numeric limit has to agree too: a
        # looser issuer-side bound is exactly the asymmetry this test exists to stop.
        if not re.search(rf"len\([a-z]\.{GO_NAMES[field]}\)\s*>\s*{limit}\b", go_body):
            missing.append(f"{field} (limit {limit})")
    assert not missing, (
        f"{rust_type}: the Rust verifier bounds these but the Go issuer does not: rust-only={missing}"
    )


@pytest.mark.parametrize(
    ("description", "rust", "go"),
    STRUCTURAL_INVARIANTS,
    ids=[entry[0].split()[-1] for entry in STRUCTURAL_INVARIANTS],
)
def test_declared_structural_invariants_hold_on_both_sides(
    description: str, rust: str, go: str
) -> None:
    rust_source = RUST.read_text()
    go_source = GO_TICKET.read_text() + GO_GRANT.read_text()
    assert rust in rust_source, f"Rust no longer enforces: {description} (expected `{rust}`)"
    assert go in go_source, (
        f"the Go issuer does not enforce an invariant the Rust verifier does: {description} "
        f"(expected `{go}`)"
    )
