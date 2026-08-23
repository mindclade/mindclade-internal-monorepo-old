# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""The maturity model's central prohibition, exercised against synthetic import graphs.

`production_dependency = false` sat in maturity.toml with no reader for the whole life of
the repository, so the first thing these tests owe is proof that the gate can fail at all:
the positive cases below inject a violation and assert the finding names both packages and
both statuses. The negative cases are the other half of the same obligation — a gate that
fires on a test file importing an experimental package, or on a component reaching into its
own subtree, would be turned off within a week and would deserve it.

The undeclared-importer case is the one with history. Fail-open on "I cannot classify this"
is the defect this review found repeatedly, and services/ holds ~17.5k lines of undeclared
production Go, so an undeclared consumer is the population that most needs the rule rather
than the one to exempt from it.
"""

from __future__ import annotations

import datetime as dt
import importlib.util
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[3]

GO_LICENCE = (
    "// Copyright © 2026 Mindclade, LLC. All Rights Reserved.\n"
    "// Mindclade Proprietary and Confidential.\n"
    "// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary\n"
    "//\n\n"
)

TODAY = dt.date(2026, 8, 23)


def load(name: str, path: Path):
    spec = importlib.util.spec_from_file_location(name, path)
    # None means the path is not importable, which is how a drifting ROOT (parents[3]) goes
    # wrong. Raised rather than asserted: `python -O` drops asserts, and this runs at import
    # time where the message is all anyone gets.
    if spec is None or spec.loader is None:
        raise RuntimeError(f"unable to load {path}")
    module = importlib.util.module_from_spec(spec)
    sys.modules[name] = module
    spec.loader.exec_module(module)
    return module


gate = load(
    "check_production_dependencies", ROOT / "tools/analysis/check_production_dependencies.py"
)

DEFAULT_POLICY = (
    'statuses = ["planned", "scaffolded", "experimental", "implemented", '
    '"qualified", "production", "deprecated"]\n'
    "[rules.planned]\nproduction_dependency = false\n"
    "[rules.scaffolded]\nproduction_dependency = false\n"
    "[rules.experimental]\nproduction_dependency = false\n"
    "[rules.implemented]\nrequires_tests = true\n"
)


def _write(root: Path, relative: str, text: str) -> None:
    path = root / relative
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(text, encoding="utf-8")


def _go(root: Path, relative: str, package: str, imports: tuple[str, ...] = ()) -> None:
    """One Go file whose only in-module edges are the ones named."""
    body = GO_LICENCE + f"package {package}\n"
    if imports:
        joined = "".join(f'\t"example.test/{value}"\n' for value in imports)
        body += f"\nimport (\n{joined})\n"
    _write(root, relative, body)


def _component(name: str, path: str, status: str) -> str:
    return f'[[component]]\nname = "{name}"\npath = "{path}"\nstatus = "{status}"\n\n'


def _repo(
    tmp_path: Path,
    *,
    label: str = "repo",
    policy: str = DEFAULT_POLICY,
    components: str = "",
    owners: str = '[[owners]]\nteam = "data-platform"\npaths = ["**"]\n',
    adr_status: str = "Accepted",
) -> Path:
    """A miniature module carrying every file the checker reads.

    Each scenario gets its own directory because go_import_graph.import_graph is cached on
    the root path; reusing one root across two graphs would answer the second question with
    the first answer.
    """
    root = tmp_path / label
    root.mkdir(parents=True, exist_ok=True)
    _write(root, "go.mod", "module example.test\n\ngo 1.26.0\n")
    _write(root, "maturity.toml", policy)
    _write(root, "components.toml", components)
    _write(root, "OWNERS.toml", owners)
    _write(
        root,
        "docs/design/adr-0020-unified-stage-worker-protocol.md",
        f"# ADR-0020: Use one ticketed stage-worker protocol\n\n- **Status:** {adr_status}\n",
    )
    return root


def _exception(**overrides) -> dict:
    values = {
        "owner": "data-platform",
        "adr": "ADR-0020",
        "reason": "one durable stage vocabulary",
        "expires_on": "2026-11-14",
    }
    values.update(overrides)
    return values


def _check(root: Path, exceptions: dict | None = None, *, today: dt.date = TODAY) -> list[str]:
    """Run the gate with an explicit exception table, restoring the real one afterwards."""
    original = gate.PRODUCTION_DEPENDENCY_EXCEPTIONS
    gate.PRODUCTION_DEPENDENCY_EXCEPTIONS = exceptions or {}
    try:
        return gate.check(root, today=today)
    finally:
        gate.PRODUCTION_DEPENDENCY_EXCEPTIONS = original


# --------------------------------------------------------------------------------------
# The prohibition itself
# --------------------------------------------------------------------------------------


def test_implemented_importing_scaffolded_is_a_violation(tmp_path: Path) -> None:
    root = _repo(
        tmp_path,
        components=_component("app.pipeline", "control/pipeline", "implemented")
        + _component("app.reserved", "control/reserved", "scaffolded"),
    )
    _go(root, "control/pipeline/pipeline.go", "pipeline", ("control/reserved",))
    _go(root, "control/reserved/reserved.go", "reserved")

    errors = _check(root)
    assert len(errors) == 1, errors
    assert "control/pipeline" in errors[0]
    assert "control/reserved" in errors[0]
    assert "'implemented'" in errors[0]
    assert "'scaffolded'" in errors[0]
    assert "production_dependency = false" in errors[0]


def test_every_permitted_status_is_a_consumer(tmp_path: Path) -> None:
    """implemented, qualified, production and deprecated all carry the prohibition.

    `deprecated` is the deliberate call. maturity.toml puts `production_dependency = false`
    on the first three statuses only, so deprecated code is still a legal dependency — but
    it is code that still ships, so whatever it links still reaches production, and it is
    therefore a consumer the rule applies to.
    """
    for status in ("implemented", "qualified", "production", "deprecated"):
        root = _repo(
            tmp_path,
            label=f"consumer-{status}",
            components=_component("app.consumer", "control/consumer", status)
            + _component("app.draft", "control/draft", "experimental"),
        )
        _go(root, "control/consumer/consumer.go", "consumer", ("control/draft",))
        _go(root, "control/draft/draft.go", "draft")
        errors = _check(root)
        assert len(errors) == 1, (status, errors)
        assert f"'{status}'" in errors[0]


def test_forbidden_status_may_depend_on_forbidden_status(tmp_path: Path) -> None:
    """An experimental component depending on another claims no readiness it cannot back."""
    root = _repo(
        tmp_path,
        components=_component("app.draft", "control/draft", "experimental")
        + _component("app.reserved", "control/reserved", "scaffolded"),
    )
    _go(root, "control/draft/draft.go", "draft", ("control/reserved",))
    _go(root, "control/reserved/reserved.go", "reserved")

    assert _check(root) == []


def test_deprecated_remains_a_legal_dependency(tmp_path: Path) -> None:
    """maturity.toml does not forbid depending on deprecated, and this gate must not invent it.

    Tightening that is a maturity.toml change with its own retirement consequences, not a
    rule a checker gets to add on the way past.
    """
    root = _repo(
        tmp_path,
        components=_component("app.consumer", "control/consumer", "implemented")
        + _component("app.old", "control/old", "deprecated"),
    )
    _go(root, "control/consumer/consumer.go", "consumer", ("control/old",))
    _go(root, "control/old/old.go", "old")

    assert _check(root) == []


def test_component_may_import_its_own_subtree(tmp_path: Path) -> None:
    root = _repo(
        tmp_path,
        components=_component("app.draft", "control/draft", "experimental")
        + _component("app.consumer", "control/consumer", "implemented"),
    )
    _go(root, "control/draft/draft.go", "draft", ("control/draft/adapters/local",))
    _go(root, "control/draft/adapters/local/local.go", "local")
    _go(root, "control/consumer/consumer.go", "consumer")

    assert _check(root) == []


def test_longest_prefix_wins_over_a_parent_entry(tmp_path: Path) -> None:
    """A leaf entry inside a declared subtree means what it says, not what its parent says."""
    root = _repo(
        tmp_path,
        components=_component("app.registry", "control/registry", "implemented")
        + _component("app.leaf", "control/registry/leaf", "experimental")
        + _component("app.consumer", "control/consumer", "implemented"),
    )
    _go(root, "control/registry/registry.go", "registry")
    _go(root, "control/registry/leaf/leaf.go", "leaf")
    _go(root, "control/consumer/consumer.go", "consumer", ("control/registry/leaf",))

    errors = _check(root)
    assert len(errors) == 1, errors
    assert "control/registry/leaf" in errors[0]


# --------------------------------------------------------------------------------------
# Test files
# --------------------------------------------------------------------------------------


def test_a_test_only_import_is_not_a_violation(tmp_path: Path) -> None:
    """The shape of services/control_plane/tests -> control/routing.

    Exercising an experimental package is how it earns implemented; gating that would make
    the model's lower three statuses unreachable.
    """
    root = _repo(
        tmp_path,
        components=_component("app.consumer", "control/consumer", "implemented")
        + _component("app.draft", "control/draft", "experimental"),
    )
    _go(root, "control/consumer/consumer.go", "consumer")
    _go(root, "control/consumer/consumer_test.go", "consumer", ("control/draft",))
    _go(root, "control/draft/draft.go", "draft")

    assert _check(root) == []


def test_the_same_import_in_a_production_file_is_a_violation(tmp_path: Path) -> None:
    """The test exemption is per file, not per package, so it cannot launder a package."""
    root = _repo(
        tmp_path,
        components=_component("app.consumer", "control/consumer", "implemented")
        + _component("app.draft", "control/draft", "experimental"),
    )
    _go(root, "control/consumer/consumer.go", "consumer", ("control/draft",))
    _go(root, "control/consumer/consumer_test.go", "consumer", ("control/draft",))
    _go(root, "control/draft/draft.go", "draft")

    errors = _check(root)
    assert len(errors) == 1, errors
    assert "control/consumer" in errors[0]


# --------------------------------------------------------------------------------------
# Undeclared packages
# --------------------------------------------------------------------------------------


def test_undeclared_importer_fails_closed(tmp_path: Path) -> None:
    """The services/ shape: real production Go with no entry, importing experimental code."""
    root = _repo(tmp_path, components=_component("app.draft", "control/draft", "experimental"))
    _go(root, "services/gateway/gateway.go", "gateway", ("control/draft",))
    _go(root, "control/draft/draft.go", "draft")

    errors = _check(root)
    assert len(errors) == 1, errors
    assert "services/gateway" in errors[0]
    assert "no components.toml entry" in errors[0]
    assert "control/draft" in errors[0]


def test_undeclared_importee_is_left_to_the_declaration_gate(tmp_path: Path) -> None:
    """No status means no rule to apply. check_component_maturity.py owns that omission."""
    root = _repo(tmp_path, components=_component("app.consumer", "control/consumer", "implemented"))
    _go(root, "control/consumer/consumer.go", "consumer", ("control/mystery",))
    _go(root, "control/mystery/mystery.go", "mystery")

    assert _check(root) == []


# --------------------------------------------------------------------------------------
# Policy integrity: the ways this gate could stop gating
# --------------------------------------------------------------------------------------


def test_removing_the_flag_entirely_is_reported(tmp_path: Path) -> None:
    """Deleting `production_dependency = false` must not silently produce a passing gate."""
    root = _repo(
        tmp_path,
        policy='statuses = ["experimental", "implemented"]\n[rules.implemented]\nrequires_tests = true\n',
        components=_component("app.consumer", "control/consumer", "implemented")
        + _component("app.draft", "control/draft", "experimental"),
    )
    _go(root, "control/consumer/consumer.go", "consumer", ("control/draft",))
    _go(root, "control/draft/draft.go", "draft")

    errors = _check(root)
    assert any("no status declares" in error for error in errors), errors


def test_a_non_boolean_flag_is_reported(tmp_path: Path) -> None:
    """`production_dependency = "false"` is truthy in Python and would permit the edge."""
    root = _repo(
        tmp_path,
        policy=(
            'statuses = ["experimental", "implemented"]\n'
            '[rules.experimental]\nproduction_dependency = "false"\n'
        ),
        components=_component("app.consumer", "control/consumer", "implemented")
        + _component("app.draft", "control/draft", "experimental"),
    )
    _go(root, "control/consumer/consumer.go", "consumer", ("control/draft",))
    _go(root, "control/draft/draft.go", "draft")

    errors = _check(root)
    assert any("not a boolean" in error for error in errors), errors


def test_a_rule_for_an_undeclared_status_is_reported(tmp_path: Path) -> None:
    """A typo in a `[rules.<status>]` header silently drops the rule it was meant to add."""
    root = _repo(
        tmp_path,
        policy=(
            'statuses = ["experimental", "implemented"]\n'
            "[rules.experimental]\nproduction_dependency = false\n"
            "[rules.experimentl]\nproduction_dependency = false\n"
        ),
        components="",
    )
    errors = _check(root)
    assert any("experimentl" in error and "apply to nothing" in error for error in errors), errors


def test_an_unknown_component_status_fails_closed(tmp_path: Path) -> None:
    """check_go_layers.py's precedent: unclassifiable is an error, never a silent pass."""
    root = _repo(tmp_path, components=_component("app.consumer", "control/consumer", "prototype"))
    _go(root, "control/consumer/consumer.go", "consumer")

    errors = _check(root)
    assert any("cannot classify" in error for error in errors), errors


def test_a_duplicated_component_path_fails_closed(tmp_path: Path) -> None:
    root = _repo(
        tmp_path,
        components=_component("app.one", "control/thing", "implemented")
        + _component("app.two", "control/thing", "experimental"),
    )
    _go(root, "control/thing/thing.go", "thing")

    errors = _check(root)
    assert any("declared twice" in error for error in errors), errors


# --------------------------------------------------------------------------------------
# Exceptions
# --------------------------------------------------------------------------------------


def _excepted_repo(tmp_path: Path, label: str, **kwargs) -> Path:
    root = _repo(
        tmp_path,
        label=label,
        components=_component("app.consumer", "control/consumer", "implemented")
        + _component("app.draft", "control/draft", "experimental"),
        **kwargs,
    )
    _go(root, "control/consumer/consumer.go", "consumer", ("control/draft",))
    _go(root, "control/draft/draft.go", "draft")
    return root


KEY = ("control/consumer", "control/draft")


def test_a_valid_exception_suppresses_the_finding(tmp_path: Path) -> None:
    root = _excepted_repo(tmp_path, "valid")
    assert _check(root, {KEY: gate.DependencyException(**_exception())}) == []


def test_an_expired_exception_fails(tmp_path: Path) -> None:
    root = _excepted_repo(tmp_path, "expired")
    errors = _check(root, {KEY: gate.DependencyException(**_exception(expires_on="2026-08-22"))})
    assert any("expired on 2026-08-22" in error for error in errors), errors


def test_an_exception_beyond_the_cap_fails(tmp_path: Path) -> None:
    root = _excepted_repo(tmp_path, "far")
    errors = _check(root, {KEY: gate.DependencyException(**_exception(expires_on="2027-08-23"))})
    assert any(f"more than the {gate.MAX_EXCEPTION_DAYS}-day cap" in e for e in errors), errors


def test_an_exception_with_an_unknown_owner_fails(tmp_path: Path) -> None:
    root = _excepted_repo(tmp_path, "owner")
    errors = _check(root, {KEY: gate.DependencyException(**_exception(owner="nobody"))})
    assert any("is not a team declared" in error for error in errors), errors


def test_an_exception_without_an_accepted_adr_fails(tmp_path: Path) -> None:
    root = _excepted_repo(tmp_path, "adr", adr_status="Proposed")
    errors = _check(root, {KEY: gate.DependencyException(**_exception())})
    assert any("is not an accepted decision" in error for error in errors), errors


def test_an_exception_with_a_malformed_adr_fails(tmp_path: Path) -> None:
    root = _excepted_repo(tmp_path, "adrform")
    errors = _check(root, {KEY: gate.DependencyException(**_exception(adr="see the wiki"))})
    assert any("is not of the form ADR-NNNN" in error for error in errors), errors


def test_an_exception_without_a_reason_fails(tmp_path: Path) -> None:
    root = _excepted_repo(tmp_path, "reason")
    errors = _check(root, {KEY: gate.DependencyException(**_exception(reason="   "))})
    assert any("reason is required" in error for error in errors), errors


def test_a_stale_exception_fails(tmp_path: Path) -> None:
    """The import is gone; the grant must go with it rather than cover its successor."""
    root = _repo(
        tmp_path,
        label="stale",
        components=_component("app.consumer", "control/consumer", "implemented")
        + _component("app.draft", "control/draft", "experimental"),
    )
    _go(root, "control/consumer/consumer.go", "consumer")
    _go(root, "control/draft/draft.go", "draft")

    errors = _check(root, {KEY: gate.DependencyException(**_exception())})
    assert any("stale and must be deleted" in error for error in errors), errors


def test_an_exception_whose_dependency_was_promoted_is_stale(tmp_path: Path) -> None:
    """The import still exists but no longer violates anything, so the grant means nothing."""
    root = _repo(
        tmp_path,
        label="promoted",
        components=_component("app.consumer", "control/consumer", "implemented")
        + _component("app.draft", "control/draft", "implemented"),
    )
    _go(root, "control/consumer/consumer.go", "consumer", ("control/draft",))
    _go(root, "control/draft/draft.go", "draft")

    errors = _check(root, {KEY: gate.DependencyException(**_exception())})
    assert any("stale and must be deleted" in error for error in errors), errors


def test_an_exception_does_not_cover_a_sibling_package(tmp_path: Path) -> None:
    """Package-level keys, so a grant argued for one package is not inherited by a subtree."""
    root = _repo(
        tmp_path,
        label="sibling",
        components=_component("app.consumer", "control/consumer", "implemented")
        + _component("app.draft", "control/draft", "experimental"),
    )
    _go(root, "control/consumer/consumer.go", "consumer", ("control/draft",))
    _go(root, "control/consumer/adapters/k8s/k8s.go", "k8s", ("control/draft",))
    _go(root, "control/draft/draft.go", "draft")

    errors = _check(root, {KEY: gate.DependencyException(**_exception())})
    assert len(errors) == 1, errors
    assert "control/consumer/adapters/k8s" in errors[0]


# --------------------------------------------------------------------------------------
# The shipped exception table
# --------------------------------------------------------------------------------------


def test_the_shipped_exception_table_is_empty() -> None:
    """The steady state has no policy waivers; a new one requires an explicit test change."""
    assert not gate.PRODUCTION_DEPENDENCY_EXCEPTIONS
