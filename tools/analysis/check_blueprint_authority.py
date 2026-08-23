#!/usr/bin/env python3
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Reconcile the reserved-path blueprint against the authorities that forbid paths.

`docs/blueprint/production-monorepo-paths.txt` reserves every path the target-state tree is
meant to contain, and `tests/integration/test_blueprint_scaffold.py` ratchets
`MATERIALIZATION_BASELINE == 0` -- every reserved path must exist. `check_blueprint_scaffold.py`
checks that the reservations are well formed and materialized.

Nothing checked that a reservation was *permitted*. Reserved paths were legal by construction:
the manifest could name a file that accepted ADRs and a merged drift gate forbid, and the board
stayed green because the only question anyone asked was "does it exist". It did exist -- someone
had to create it to clear the ratchet. The ratchet was the thing that manufactured the violation.

That is not hypothetical. Five reserved scaffolds under `tools/codegen/` were mass-generated from
a directory drawing in `docs/blueprint/production-monorepo-blueprint.md`, each holding the same
boilerplate `SCAFFOLD_PATH` constant, and each contradicting a standing decision. Two of them
contradict a decision that is already machine-readable, which is what this module gates.

## What is mechanised here

Two classes, both resolved from data this repository already maintains, neither requiring any
natural-language reading:

**`reserved_absent_artifact`** -- a reserved path that itself matches a `GENERATED_RULES` pattern
whose disposition is `ABSENT`. This is a direct, zero-inference contradiction between two gates:
the blueprint ratchet requires the path to exist, and `tools/codegen/verify_generated.py` requires
it never to. Both cannot be satisfied, so whichever runs second reports a defect the author cannot
fix without changing one of the two authorities.

**`reserved_generator_absent_output`** -- a reserved *generator* whose target language has a
committed generated-artifact surface in `.gitattributes`, every rule of which is `ABSENT`. Such a
generator has, by the repository's own disposition table, no legal output: every artifact it could
emit is one the drift gate rejects on sight. `tools/codegen/generate_go_sdk.py` and
`tools/codegen/generate_python_sdk.py` are exactly this -- ADR-0014 makes Go and Python "Bazel
action outputs", and all four Go/Python rules in `GENERATED_RULES` say `ABSENT`.

The verdict is read live from `GENERATED_RULES`, never restated here. If a disposition flips to
`REGENERATED` -- if this repository ever decides to commit Go bindings -- the contradiction
dissolves on its own and this gate stops reporting it, because the gate is not holding a private
copy of the answer. `tools/codegen/generate_typescript_sdk.py` is the live control for that:
TypeScript is the one checked-in language projection, its rule is `REGENERATED`, and this gate
passes it. A gate that flagged every generator would be measuring the word "generate", not the
policy.

## The one thing this module refuses to do

Pass vacuously. Gates in this tree have reported clean while resolving nothing -- scanning zero
files, asserting a string existed without looking inside it. Every input this module depends on is
asserted present before any verdict is issued, and each assertion is a *failure*, never a skip: an
unreadable manifest, a manifest that parses to zero paths, a `GENERATED_RULES` with no `ABSENT`
rule left in it, a language in `LANGUAGE_ARTIFACTS` that no longer resolves to any rule, and a
manifest that reserves no generator at all. Each of those means the ground this gate stands on has
moved, and the honest report is that the gate no longer knows -- not `PASS`.

See `assert_inputs_resolve` for the guards and what each one catches.

## What is deliberately not mechanised

Three of the five removed scaffolds are forbidden by prose, and this module does not pretend
otherwise. Recording them here is the point: a reader looking for the missing coverage finds the
reason rather than assuming it was an oversight.

  * `generate_openapi_clients.py` -- `generate_typescript_sdk.py` already owns
    `public.openapi.yaml` end to end, and `protocols/openapi/README.md` requires
    `admin.openapi.yaml` to stay non-operational pending review. Both facts live in prose. There
    is no machine-readable statement of which generator owns which OpenAPI document; the
    `Generators` table in `tools/codegen/README.md` is the closest thing, and a markdown table is
    not an authority a gate should be parsing for a policy verdict.
  * `generate_config_schema.py` -- `configs/schemas/*.schema.json` *is* the authority per ADR-0014
    and `configs/README.md`, so there is no upstream to derive it from. "This artifact has no
    generator because it is itself canonical" is not expressible in `GENERATED_RULES`, which only
    records dispositions for things that *are* generated.
  * `generate_build_files.py` -- duplicates Gazelle, whose `//:gazelle_check` target fails on
    diff. Detecting "this reserved path would duplicate an existing Bazel target's job" needs a
    generator-to-responsibility map that does not exist in machine-readable form.

Rust is absent from `LANGUAGE_ARTIFACTS` for a related reason. ADR-0014 has Rust generated by a
Bazel-owned Cargo build script and never committed, so Rust has *no* `linguist-generated` surface
at all -- there are zero rules to read a disposition from. A hypothetical `generate_rust_sdk.py`
is forbidden by the same ADR sentence that forbids the Go one, but the Go case is decidable from
the rule table and the Rust case is not. Claiming it would mean hardcoding the answer, which is
the failure mode this module's control case exists to avoid.

Bazel layer classification was evaluated as a third class and rejected. `layers.bzl` governs Bazel
*packages that participate in the dependency graph*; 14 reserved paths under `.github/` classify
into no domain because the layer matrix deliberately does not govern them. Gating on "every
reserved path classifies" would have required an allowlist to pass on a clean tree, and an
allowlist written to make a new gate go green is the defect, not the gate.

## Falsifying fixture

`tools/analysis/check_gate_falsifiability.py` requires every gate to have an input that trips it.
This one is tripped by reinstating a removed reservation -- no file needs to be created, because
the defect is the reservation itself:

    Falsifier(
        check="blueprint authority",
        defect="the blueprint reserves a generator whose entire output surface must be absent",
        expect="reserved generator claims go output",
        inject=lambda tree: tree.replace(
            "docs/blueprint/production-monorepo-paths.txt",
            "tools/codegen/generate_event_catalog.py\n",
            "tools/codegen/generate_event_catalog.py\ntools/codegen/generate_go_sdk.py\n",
        ),
    )
"""

from __future__ import annotations

import argparse
import json
import re
import sys
from dataclasses import dataclass
from pathlib import Path

# `check_blueprint_scaffold` is a plain sibling: this module runs as a script from tools/analysis,
# where the interpreter puts that directory on sys.path itself, or as an import from
# run_architecture_checks.py, which inserts tools/analysis first.
#
# `verify_generated` is not a sibling -- it lives in tools/codegen, next to the generators whose
# output it verifies. run_architecture_checks.py appends that directory for its own import, but
# this module also runs standalone from the command line, where nothing has done so. The bootstrap
# below is therefore required rather than redundant, and it must not be replaced with a try/except
# ImportError that skips the check: GENERATED_RULES is the authority this gate reads, and a gate
# that cannot read its authority has to fail, not pass. Appended, not prepended, matching
# run_architecture_checks.py -- tools/analysis must keep priority, and a second tool directory in
# front of the standard library is a shadowing hazard for no benefit.
_CODEGEN = Path(__file__).resolve().parents[1] / "codegen"
if str(_CODEGEN) not in sys.path:
    sys.path.append(str(_CODEGEN))

import check_blueprint_scaffold  # noqa: E402
import verify_generated  # noqa: E402

MANIFEST_RELPATH = check_blueprint_scaffold.MANIFEST_RELPATH

# Where reserved generators live. Narrow on purpose: `tools/codegen/` is the package the
# generation-authority ADR actually speaks about, and a gate that swept every `generate_*` in the
# tree would start issuing verdicts about build scripts and test helpers ADR-0014 is silent on.
GENERATOR_DIRECTORY = "tools/codegen"
_GENERATOR_STEM = re.compile(r"^generate_(?P<subject>[A-Za-z0-9_]+)$")

# Filename subject tokens that name a target language, mapped to the canonical language key. Only
# spellings this repository actually uses; an unrecognised token makes a generator *undecidable*,
# which is reported, not silently passed.
_LANGUAGE_TOKENS = {
    "go": "go",
    "golang": "go",
    "python": "python",
    "py": "python",
    "typescript": "typescript",
    "ts": "typescript",
}

# How to recognise a `GENERATED_RULES` pattern as belonging to a language: by the file extension
# the pattern ends in, or by a path segment naming the language, for patterns that are directory
# globs (`sdk/typescript/src/generated/**` has no extension to read).
#
# Every language listed here MUST resolve to at least one rule -- `assert_inputs_resolve` enforces
# it. That guard is what stops this table from rotting into a no-op: if `.gitattributes` stopped
# marking Go bindings generated, this gate would fail loudly rather than quietly deciding that
# `generate_go_sdk.py` had become permissible.
#
# Rust is deliberately absent; see the module docstring.
LANGUAGE_ARTIFACTS: dict[str, tuple[str, ...]] = {
    "go": (".go",),
    "python": (".py", ".pyi"),
    "typescript": (".ts",),
}


class BlueprintAuthorityError(Exception):
    """An input this gate reasons from could not be resolved.

    Raised, never swallowed. Every construction site for this exception is a place where the
    alternative would be returning "no violations found" from a check that examined nothing.
    """


@dataclass(frozen=True)
class Contradiction:
    """One reserved path and the authority that forbids reserving it."""

    kind: str
    path: str
    authority: str
    detail: str

    def render(self) -> str:
        return f"{self.path}: {self.detail} (contradicts {self.authority})"


def repository_root() -> Path:
    return Path(__file__).resolve().parents[2]


def glob_to_regex(pattern: str) -> re.Pattern[str]:
    """Translate a `.gitattributes`-style path glob into an anchored regex.

    Hand-rolled rather than delegated to `fnmatch` or `PurePosixPath.full_match`: `fnmatch` lets
    `*` cross directory separators, which would make `protocols/**/*.pb.go` match paths it must
    not, and `full_match`'s treatment of a trailing `**` has shifted between releases. The
    dispositions this gate reads are load-bearing enough to be worth an explicit matcher whose
    semantics are pinned by tests in this repository rather than by an interpreter upgrade.

    `**` matches zero or more whole segments; `*` matches within one segment; `?` matches one
    character that is not a separator.
    """
    out: list[str] = []
    index = 0
    while index < len(pattern):
        char = pattern[index]
        if pattern.startswith("**/", index):
            # Zero or more leading segments, so `a/**/b.go` also matches `a/b.go`.
            out.append("(?:[^/]+/)*")
            index += 3
        elif pattern.startswith("**", index):
            out.append(".*")
            index += 2
        elif char == "*":
            out.append("[^/]*")
            index += 1
        elif char == "?":
            out.append("[^/]")
            index += 1
        else:
            out.append(re.escape(char))
            index += 1
    return re.compile("".join(out) + r"\Z")


def absent_rules() -> tuple[verify_generated.GeneratedRule, ...]:
    """Every `GENERATED_RULES` entry whose artifact must never be committed."""
    return tuple(
        rule
        for rule in verify_generated.GENERATED_RULES
        if rule.disposition == verify_generated.ABSENT
    )


def rules_for_language(language: str) -> tuple[verify_generated.GeneratedRule, ...]:
    """Every `GENERATED_RULES` entry describing a committed artifact in `language`."""
    suffixes = LANGUAGE_ARTIFACTS[language]
    return tuple(
        rule
        for rule in verify_generated.GENERATED_RULES
        if rule.pattern.endswith(suffixes) or language in rule.pattern.split("/")
    )


def read_reserved_paths(manifest: Path) -> list[str]:
    """Parse the manifest into reserved paths, or raise.

    Deliberately not reusing `check_blueprint_scaffold.check()`: that entry point returns a result
    dict describing materialization, and its `paths` key is filtered by invariants this gate does
    not share. This gate must see every line the manifest reserves, including one naming a file
    that does not exist -- a reservation is forbidden or permitted whether or not anyone has
    created it yet.
    """
    try:
        text = manifest.read_text(encoding="utf-8")
    except OSError as error:
        raise BlueprintAuthorityError(f"{MANIFEST_RELPATH} is unreadable: {error}") from error
    paths = [
        stripped
        for line in text.splitlines()
        if (stripped := line.strip()) and not stripped.startswith("#")
    ]
    if not paths:
        raise BlueprintAuthorityError(
            f"{MANIFEST_RELPATH} parsed to zero reserved paths; this gate would have examined "
            f"nothing and reported clean"
        )
    return paths


def generator_subject(path: str) -> str | None:
    """The subject of a reserved generator path, or None if the path is not a generator."""
    candidate = Path(path)
    if candidate.parent.as_posix() != GENERATOR_DIRECTORY:
        return None
    match = _GENERATOR_STEM.match(candidate.stem)
    return match.group("subject") if match else None


def claimed_language(subject: str) -> str | None:
    """The language a generator's filename subject claims, or None if it names no language."""
    for token in subject.split("_"):
        language = _LANGUAGE_TOKENS.get(token)
        if language is not None:
            return language
    return None


def assert_inputs_resolve(paths: list[str]) -> None:
    """Assert every input this gate reasons from is present, or raise.

    Each guard corresponds to a way this gate could have gone quiet while still printing PASS.
    `read_reserved_paths` owns the first two (unreadable manifest, zero paths); the rest are here.
    """
    if not absent_rules():
        raise BlueprintAuthorityError(
            "verify_generated.GENERATED_RULES contains no ABSENT rule; the signal this gate "
            "reads has disappeared and no contradiction is detectable"
        )
    for language in LANGUAGE_ARTIFACTS:
        if not rules_for_language(language):
            raise BlueprintAuthorityError(
                f"LANGUAGE_ARTIFACTS declares {language!r} but no GENERATED_RULES pattern matches "
                f"it; the language table has drifted out of step with .gitattributes and every "
                f"{language} verdict this gate issues would be vacuous"
            )
    if not any(generator_subject(path) is not None for path in paths):
        raise BlueprintAuthorityError(
            f"{MANIFEST_RELPATH} reserves no {GENERATOR_DIRECTORY}/generate_* path; the manifest "
            f"layout this gate resolves against has changed"
        )


def _absent_artifact_contradictions(paths: list[str]) -> list[Contradiction]:
    """Reserved paths that match a rule requiring the artifact never to be committed."""
    matchers = [(rule, glob_to_regex(rule.pattern)) for rule in absent_rules()]
    found: list[Contradiction] = []
    for path in paths:
        for rule, matcher in matchers:
            if matcher.match(path):
                found.append(
                    Contradiction(
                        kind="reserved_absent_artifact",
                        path=path,
                        authority=(
                            f"GENERATED_RULES {rule.pattern!r} disposition ABSENT -- {rule.reason}"
                        ),
                        detail=(
                            "blueprint reserves a path that the generated-artifact gate requires "
                            "never to exist"
                        ),
                    )
                )
                break
    return found


def _generator_contradictions(paths: list[str]) -> tuple[list[Contradiction], list[str], int]:
    """Reserved generators with no legal output, plus the ones this gate cannot decide."""
    found: list[Contradiction] = []
    undecidable: list[str] = []
    examined = 0
    for path in paths:
        subject = generator_subject(path)
        if subject is None:
            continue
        examined += 1
        language = claimed_language(subject)
        if language is None:
            undecidable.append(path)
            continue
        rules = rules_for_language(language)
        if not all(rule.disposition == verify_generated.ABSENT for rule in rules):
            continue
        patterns = ", ".join(repr(rule.pattern) for rule in rules)
        found.append(
            Contradiction(
                kind="reserved_generator_absent_output",
                path=path,
                authority=(
                    f"GENERATED_RULES ({patterns}) all disposition ABSENT, per ADR-0014 "
                    f"'Go and Python are Bazel action outputs'"
                ),
                detail=(
                    f"reserved generator claims {language} output, but every committed {language} "
                    f"generated-artifact rule requires absence, so it has no legal output"
                ),
            )
        )
    return found, undecidable, examined


def analyze(root: Path, manifest: Path | None = None) -> dict[str, object]:
    """Reconcile reserved paths against the authorities, returning a structured result."""
    manifest = manifest if manifest is not None else root / MANIFEST_RELPATH
    paths = read_reserved_paths(manifest)
    assert_inputs_resolve(paths)

    contradictions = _absent_artifact_contradictions(paths)
    generator_found, undecidable, examined = _generator_contradictions(paths)
    contradictions.extend(generator_found)

    return {
        "schema_version": 1,
        "reserved_path_count": len(paths),
        "generators_examined": examined,
        "absent_rule_count": len(absent_rules()),
        "contradictions": [
            {
                "kind": item.kind,
                "path": item.path,
                "authority": item.authority,
                "detail": item.detail,
            }
            for item in contradictions
        ],
        "undecidable_generators": sorted(undecidable),
    }


def check(root: Path) -> list[str]:
    """The `run_architecture_checks.py` contract: a list of human-readable failures.

    A `BlueprintAuthorityError` is reported as a failure rather than allowed to propagate, so a
    drifted input surfaces as this gate's own defect with its own message instead of a traceback
    attributed to the harness.
    """
    try:
        result = analyze(root)
    except BlueprintAuthorityError as error:
        return [str(error)]
    contradictions: list[dict[str, str]] = result["contradictions"]  # type: ignore[assignment]
    return [
        Contradiction(
            kind=item["kind"],
            path=item["path"],
            authority=item["authority"],
            detail=item["detail"],
        ).render()
        for item in contradictions
    ]


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description="Reconcile reserved blueprint paths with policy.")
    parser.add_argument("--repo", type=Path, default=repository_root())
    parser.add_argument("--json", action="store_true", help="emit the structured result")
    args = parser.parse_args(argv)

    root = args.repo.resolve()
    try:
        result = analyze(root)
    except BlueprintAuthorityError as error:
        print(f"blueprint authority: {error}", file=sys.stderr)
        return 1

    if args.json:
        print(json.dumps(result, indent=2, sort_keys=True))
    else:
        print(
            f"reserved paths: {result['reserved_path_count']}, "
            f"generators examined: {result['generators_examined']}, "
            f"ABSENT rules read: {result['absent_rule_count']}"
        )
        undecidable: list[str] = result["undecidable_generators"]  # type: ignore[assignment]
        for path in undecidable:
            print(f"UNDECIDABLE {path}: names no target language; not covered by this gate")

    failures = check(root)
    for failure in failures:
        print(f"blueprint authority: {failure}", file=sys.stderr)
    return 1 if failures else 0


if __name__ == "__main__":
    raise SystemExit(main())
