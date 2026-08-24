# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

from __future__ import annotations

import copy
import importlib.util
from pathlib import Path

import pytest

ROOT = Path(__file__).resolve().parents[3]
SPEC = importlib.util.spec_from_file_location(
    "release_governance", ROOT / "ci/terraform/release_governance.py"
)
assert SPEC is not None and SPEC.loader is not None
GOVERNANCE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(GOVERNANCE)

REPOSITORY = "mindclade/mindclade-internal-monorepo"
RUN_ID = 501
SOURCE_SHA = "a" * 40
DISPATCHER = "publisher"
SIGNER = "release-signer"
RELEASE_TEAM_ID = 103


def fixtures() -> dict[str, object]:
    environment = {
        "id": 301,
        "name": "terraform-module-release",
        "can_admins_bypass": False,
        "deployment_branch_policy": {
            "protected_branches": True,
            "custom_branch_policies": False,
        },
        "protection_rules": [
            {"id": 1, "type": "branch_policy"},
            {"id": 2, "type": "wait_timer", "wait_timer": 5},
            {
                "id": 3,
                "type": "required_reviewers",
                "prevent_self_review": True,
                "reviewers": [
                    {
                        "type": "Team",
                        "reviewer": {"id": 102, "slug": "security"},
                    }
                ],
            },
        ],
    }
    summaries = [
        {
            "enforcement": "active",
            "id": 201,
            "name": "release-tag-creation",
            "source_type": "Organization",
            "target": "tag",
        },
        {
            "enforcement": "active",
            "id": 202,
            "name": "tag-protection",
            "source_type": "Organization",
            "target": "tag",
        },
    ]
    creation = {
        **summaries[0],
        "bypass_actors": [
            {
                "actor_id": RELEASE_TEAM_ID,
                "actor_type": "Team",
                "bypass_mode": "always",
            }
        ],
        "conditions": {"ref_name": {"exclude": [], "include": ["refs/tags/v*"]}},
        "rules": [{"type": "creation"}],
    }
    protection = {
        **summaries[1],
        "bypass_actors": [],
        "conditions": {"ref_name": {"exclude": [], "include": ["refs/tags/v*"]}},
        "rules": [
            {"type": "deletion"},
            {"type": "non_fast_forward"},
            {
                "type": "tag_name_pattern",
                "parameters": {
                    "name": "stable-semver-only",
                    "negate": False,
                    "operator": "regex",
                    "pattern": GOVERNANCE.TAG_PATTERN,
                },
            },
            {"type": "update"},
        ],
    }
    run = {
        "id": RUN_ID,
        "event": "workflow_dispatch",
        "head_branch": "main",
        "head_sha": SOURCE_SHA,
        "path": GOVERNANCE.WORKFLOW_PATH,
        "run_attempt": 1,
        "repository": {"full_name": REPOSITORY},
        "actor": {"login": DISPATCHER},
    }
    approvals = [
        {
            "state": "approved",
            "environments": [{"name": GOVERNANCE.ENVIRONMENT}],
            "user": {"id": 601, "login": "security-reviewer", "type": "User"},
        }
    ]
    return {
        "environment": environment,
        "immutable": {"enabled": True, "enforced_by_owner": True},
        "run": run,
        "approvals": approvals,
        "membership": {"state": "active", "role": "member"},
        "summaries": summaries,
        "creation": creation,
        "protection": protection,
    }


class FakeClient:
    def __init__(self, values: dict[str, object]) -> None:
        self.values = values

    def get(self, path: str) -> tuple[object, dict[str, str]]:
        routes = {
            f"/repos/{REPOSITORY}/environments/{GOVERNANCE.ENVIRONMENT}": "environment",
            f"/repos/{REPOSITORY}/immutable-releases": "immutable",
            f"/repos/{REPOSITORY}/actions/runs/{RUN_ID}": "run",
            f"/repos/{REPOSITORY}/actions/runs/{RUN_ID}/approvals": "approvals",
            "/orgs/mindclade/teams/security/memberships/security-reviewer": "membership",
            f"/repos/{REPOSITORY}/rulesets/201": "creation",
            f"/repos/{REPOSITORY}/rulesets/202": "protection",
        }
        return self.values[routes[path]], {}

    def get_pages(self, path: str) -> list[object]:
        assert path == f"/repos/{REPOSITORY}/rulesets?targets=tag&per_page=100"
        return list(self.values["summaries"])


def verify(values: dict[str, object]) -> dict[str, object]:
    return GOVERNANCE.verify_connected(
        FakeClient(values),
        REPOSITORY,
        RELEASE_TEAM_ID,
        RUN_ID,
        SOURCE_SHA,
        DISPATCHER,
        SIGNER,
    )


def test_connected_governance_is_exact_and_emits_bounded_evidence() -> None:
    evidence = verify(fixtures())
    assert evidence["approval"] == {
        "reviewer_id": 601,
        "reviewer_login": "security-reviewer",
        "security_membership_role": "member",
        "state": "approved",
    }
    assert evidence["dispatcher"] == DISPATCHER
    assert evidence["qualified_release_signer"] == SIGNER
    assert evidence["immutable_releases"] == {
        "enabled": True,
        "enforced_by_owner": True,
    }
    assert evidence["rulesets"]["release-tag-creation"]["bypass"] == {
        "mode": "always",
        "release_team_id": 103,
    }


@pytest.mark.parametrize(
    ("field", "value", "message"),
    [
        ("can_admins_bypass", True, "administrator bypass"),
        ("deployment_branch_policy", None, "protected branches only"),
    ],
)
def test_environment_mutations_fail_closed(field: str, value: object, message: str) -> None:
    values = fixtures()
    values["environment"][field] = value
    with pytest.raises(GOVERNANCE.GovernanceError, match=message):
        verify(values)


def test_wait_reviewer_and_approval_identity_mutations_fail_closed() -> None:
    values = fixtures()
    values["environment"]["protection_rules"][1]["wait_timer"] = 4
    with pytest.raises(GOVERNANCE.GovernanceError, match="exactly five"):
        verify(values)

    values = fixtures()
    values["approvals"][0]["user"]["login"] = DISPATCHER
    with pytest.raises(GOVERNANCE.GovernanceError, match="dispatcher cannot approve"):
        verify(values)

    values = fixtures()
    values["approvals"][0]["user"]["login"] = SIGNER
    with pytest.raises(GOVERNANCE.GovernanceError, match="signer cannot approve"):
        verify(values)

    values = fixtures()
    values["approvals"].append(copy.deepcopy(values["approvals"][0]))
    with pytest.raises(GOVERNANCE.GovernanceError, match="exactly one"):
        verify(values)

    values = fixtures()
    values["membership"]["state"] = "pending"
    with pytest.raises(GOVERNANCE.GovernanceError, match="active Security"):
        verify(values)


def test_run_immutability_and_ruleset_mutations_fail_closed() -> None:
    values = fixtures()
    values["run"]["run_attempt"] = 2
    with pytest.raises(GOVERNANCE.GovernanceError, match="run_attempt"):
        verify(values)

    values = fixtures()
    values["immutable"]["enforced_by_owner"] = False
    with pytest.raises(GOVERNANCE.GovernanceError, match="enforced by the owner"):
        verify(values)

    values = fixtures()
    values["creation"]["bypass_actors"] = []
    with pytest.raises(GOVERNANCE.GovernanceError, match="Release-team bypass"):
        verify(values)

    values = fixtures()
    values["protection"]["bypass_actors"] = [
        {"actor_id": RELEASE_TEAM_ID, "actor_type": "Team", "bypass_mode": "always"}
    ]
    with pytest.raises(GOVERNANCE.GovernanceError, match="no bypass"):
        verify(values)
