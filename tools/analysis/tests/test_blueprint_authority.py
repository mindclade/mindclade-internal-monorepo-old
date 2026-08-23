# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

"""A reserved blueprint path must be one the architecture permits.

`check_blueprint_scaffold` asks whether every reserved path exists. It does not ask whether the
reservation was allowed, so a path forbidden by an accepted ADR and a merged drift gate could sit
in the manifest with a green board -- and the materialization ratchet would then *require* someone
to create it. Five such paths reached `tools/codegen/`.

The negative cases are the point of this file. A gate is only worth its runtime if some input
makes it fail, and the specific trap this checker exists to avoid is the gate that passes because
it resolved nothing: gates in this tree have reported clean while scanning zero files or asserting
a string existed without looking inside it. So every anti-vacuity guard gets a test that drives it
to a failure, and `test_control_case_typescript_generator_is_permitted` pins the discrimination --
without it, a checker that flagged every `generate_*` path would pass this file while measuring
the word "generate" rather than the policy.
"""

from __future__ import annotations

import importlib.util
import sys
from pathlib import Path

import pytest

ROOT = Path(__file__).resolve().parents[3]
ANALYSIS = ROOT / "tools/analysis"
CODEGEN = ROOT / "tools/codegen"

# The checker imports `check_blueprint_scaffold` as a sibling and `verify_generated` from
# tools/codegen. It bootstraps the latter itself, but the sibling resolves only when
# tools/analysis is on sys.path, which run_architecture_checks.py does for itself and a test
# module has to arrange explicitly rather than inherit from whichever file was collected first.
for entry in (ANALYSIS, CODEGEN):
    if str(entry) not in sys.path:
        sys.path.insert(0, str(entry))


def load(name: str, path: Path):
    spec = importlib.util.spec_from_file_location(name, path)
    if spec is None or spec.loader is None:
        raise RuntimeError(f"unable to load {path}")
    module = importlib.util.module_from_spec(spec)
    sys.modules[name] = module
    spec.loader.exec_module(module)
    return module


authority = load("check_blueprint_authority", ANALYSIS / "check_blueprint_authority.py")
verify_generated = load("verify_generated", CODEGEN / "verify_generated.py")

# The five scaffolds retired because the architecture forbids them. Two are decidable from
# `GENERATED_RULES` alone and are gated; three are forbidden by prose and are deliberately not,
# which the module docstring records. Pinned here so that "we only mechanised two of five" stays a
# stated scope rather than an accident nobody notices.
MECHANISED = ("tools/codegen/generate_go_sdk.py", "tools/codegen/generate_python_sdk.py")
NOT_MECHANISED = (
    "tools/codegen/generate_openapi_clients.py",
    "tools/codegen/generate_config_schema.py",
    "tools/codegen/generate_build_files.py",
)


def write_manifest(tmp_path: Path, *paths: str) -> Path:
    manifest = tmp_path / "manifest.txt"
    manifest.write_text("".join(f"{path}\n" for path in paths), encoding="utf-8")
    return manifest


def messages(tmp_path: Path, *paths: str) -> list[str]:
    """Render the checker's contradictions for a synthetic manifest."""
    result = authority.analyze(ROOT, manifest=write_manifest(tmp_path, *paths))
    contradictions = result["contradictions"]
    return [
        authority.Contradiction(
            kind=item["kind"],
            path=item["path"],
            authority=item["authority"],
            detail=item["detail"],
        ).render()
        for item in contradictions
    ]


# --------------------------------------------------------------------------------------------
# The falsifying fixture: reintroduce a retired scaffold and watch the gate reject it.
# --------------------------------------------------------------------------------------------


@pytest.mark.parametrize("scaffold", MECHANISED)
def test_reintroducing_a_retired_generator_is_rejected(tmp_path: Path, scaffold: str) -> None:
    """Reserving a generator whose every output rule says ABSENT must fail, naming the authority.

    This is the fixture `check_gate_falsifiability` requires. No file is created: the defect is
    the *reservation*, so the gate must fire on the manifest line alone. That matters because the
    reservation is what the materialization ratchet would later force someone to satisfy.
    """
    found = messages(tmp_path, "tools/codegen/generate_event_catalog.py", scaffold)
    assert len(found) == 1, found
    assert scaffold in found[0]
    assert "has no legal output" in found[0]
    # The authority must be named, not merely implied. A failure that does not say which rule it
    # contradicts sends the author to guess, and guessing here means deleting the wrong side.
    assert "GENERATED_RULES" in found[0]
    assert "ADR-0014" in found[0]


def test_control_case_typescript_generator_is_permitted(tmp_path: Path) -> None:
    """The one checked-in language projection must pass, or the gate is measuring the wrong thing.

    ADR-0014 makes TypeScript the single committed language projection because it is an
    independently published SDK input, and its `GENERATED_RULES` disposition is REGENERATED. A
    checker that flagged this too would be pattern-matching on `generate_` and would have rejected
    a generator the repository actually depends on.
    """
    assert messages(tmp_path, "tools/codegen/generate_typescript_sdk.py") == []


def test_prose_forbidden_scaffolds_are_not_claimed(tmp_path: Path) -> None:
    """The three scaffolds forbidden only by prose are reported undecidable, never as passing.

    Silence would be indistinguishable from approval. They surface in `undecidable_generators` so
    the gap is visible in the output rather than inferred from the absence of a message.
    """
    result = authority.analyze(ROOT, manifest=write_manifest(tmp_path, *NOT_MECHANISED))
    assert result["contradictions"] == []
    assert result["undecidable_generators"] == sorted(NOT_MECHANISED)


def test_reserved_path_matching_an_absent_rule_is_rejected(tmp_path: Path) -> None:
    """A reserved path that is itself an artifact required to be absent is a direct contradiction.

    The blueprint ratchet requires the path to exist; `verify_generated` requires it never to.
    Both gates cannot be satisfied, and whichever runs second reports a defect the author cannot
    fix without amending one of the two authorities.
    """
    found = messages(
        tmp_path, "tools/codegen/generate_event_catalog.py", "protocols/proto/v1/run.pb.go"
    )
    assert len(found) == 1, found
    assert "protocols/proto/v1/run.pb.go" in found[0]
    assert "requires never to exist" in found[0]
    assert "protocols/**/*.pb.go" in found[0]


# --------------------------------------------------------------------------------------------
# Anti-vacuity: every input is asserted present, and each assertion is a failure, not a skip.
# --------------------------------------------------------------------------------------------


def test_unreadable_manifest_fails_rather_than_reporting_clean(tmp_path: Path) -> None:
    with pytest.raises(authority.BlueprintAuthorityError, match="unreadable"):
        authority.analyze(ROOT, manifest=tmp_path / "absent.txt")


def test_empty_manifest_fails_rather_than_reporting_clean(tmp_path: Path) -> None:
    """Zero reserved paths is the archetypal vacuous pass: nothing examined, nothing reported."""
    with pytest.raises(authority.BlueprintAuthorityError, match="zero reserved paths"):
        authority.analyze(ROOT, manifest=write_manifest(tmp_path))


def test_manifest_reserving_no_generator_fails(tmp_path: Path) -> None:
    """If no reserved path is a generator any more, the layout moved and the gate cannot know."""
    with pytest.raises(authority.BlueprintAuthorityError, match="reserves no"):
        authority.analyze(ROOT, manifest=write_manifest(tmp_path, "libs/go/retry/retry.go"))


def test_language_table_that_stops_resolving_fails(tmp_path: Path, monkeypatch) -> None:
    """A language with no matching rule would make every verdict for it vacuous.

    This is the guard that keeps `LANGUAGE_ARTIFACTS` from rotting into a no-op. If
    `.gitattributes` stopped marking Go bindings generated, the honest report is that the gate no
    longer knows -- not that `generate_go_sdk.py` has become permissible.
    """
    monkeypatch.setitem(authority.LANGUAGE_ARTIFACTS, "cobol", (".cbl",))
    with pytest.raises(authority.BlueprintAuthorityError, match="no GENERATED_RULES pattern"):
        authority.analyze(ROOT, manifest=write_manifest(tmp_path, *MECHANISED))


def test_absent_dispositions_disappearing_fails(tmp_path: Path, monkeypatch) -> None:
    """With no ABSENT rule left, the signal is gone and no contradiction is detectable."""
    kept = tuple(
        rule
        for rule in verify_generated.GENERATED_RULES
        if rule.disposition != verify_generated.ABSENT
    )
    monkeypatch.setattr(authority.verify_generated, "GENERATED_RULES", kept)
    with pytest.raises(authority.BlueprintAuthorityError, match="no ABSENT rule"):
        authority.analyze(ROOT, manifest=write_manifest(tmp_path, *MECHANISED))


def test_check_reports_a_drifted_input_as_its_own_failure(tmp_path: Path) -> None:
    """`check()` converts the error into a message instead of a traceback blamed on the harness."""
    reported = authority.check(tmp_path)
    assert len(reported) == 1
    assert "unreadable" in reported[0]


# --------------------------------------------------------------------------------------------
# The glob matcher, whose semantics decide what `ABSENT` covers.
# --------------------------------------------------------------------------------------------


@pytest.mark.parametrize(
    ("pattern", "path", "expected"),
    [
        # `**` spans zero or more segments, so a rule written for nested packages still covers a
        # top-level one. A matcher that required at least one segment would let
        # `protocols/run.pb.go` through the gate that exists to forbid it.
        ("protocols/**/*.pb.go", "protocols/run.pb.go", True),
        ("protocols/**/*.pb.go", "protocols/proto/v1/run.pb.go", True),
        # `*` must not cross a separator; `fnmatch` would wrongly accept this.
        ("protocols/*.pb.go", "protocols/proto/v1/run.pb.go", False),
        ("protocols/**/*.pb.go", "sdk/go/run.pb.go", False),
        # Anchored at both ends: a rule for `.pb.go` must not match a longer name.
        ("protocols/**/*.pb.go", "protocols/run.pb.go.bak", False),
        ("sdk/typescript/src/generated/**", "sdk/typescript/src/generated/a/b_pb.ts", True),
        ("sdk/typescript/src/generated/**", "sdk/typescript/src/other.ts", False),
    ],
)
def test_glob_semantics(pattern: str, path: str, expected: bool) -> None:
    assert bool(authority.glob_to_regex(pattern).match(path)) is expected


def test_generator_subject_is_scoped_to_the_codegen_package() -> None:
    """A `generate_*` file elsewhere is not a codegen generator and gets no verdict.

    ADR-0014 speaks about `tools/codegen/`. Sweeping the whole tree would have this gate issuing
    generation-authority verdicts about build scripts and test helpers the ADR is silent on.
    """
    assert authority.generator_subject("tools/codegen/generate_go_sdk.py") == "go_sdk"
    assert authority.generator_subject("tools/codegen/generate_proto.sh") == "proto"
    assert authority.generator_subject("libs/go/x/generate_go_sdk.py") is None
    assert authority.generator_subject("tools/codegen/nested/generate_go_sdk.py") is None
    assert authority.generator_subject("tools/codegen/verify_generated.py") is None


def test_claimed_language_reads_any_token_not_just_the_first() -> None:
    assert authority.claimed_language("go_sdk") == "go"
    assert authority.claimed_language("sdk_go") == "go"
    assert authority.claimed_language("python_sdk") == "python"
    assert authority.claimed_language("openapi_clients") is None


# --------------------------------------------------------------------------------------------
# Registration. A checker nothing calls is a checker that cannot fail anything.
# --------------------------------------------------------------------------------------------


def test_checker_is_registered_in_the_presubmit_entry_point() -> None:
    runner = load("run_architecture_checks", ANALYSIS / "run_architecture_checks.py")
    assert any(name == "blueprint authority" for name, _ in runner.CHECKS)
