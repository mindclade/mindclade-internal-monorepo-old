#!/usr/bin/env python3
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

"""Independently verify connected governance for Terraform module publication."""

from __future__ import annotations

import argparse
import json
import os
import re
import sys
import urllib.error
import urllib.parse
import urllib.request
from pathlib import Path
from typing import Any

ENVIRONMENT = "terraform-module-release"
SECURITY_TEAM = "security"
CREATION_RULESET = "release-tag-creation"
PROTECTION_RULESET = "tag-protection"
WORKFLOW_PATH = ".github/workflows/terraform-module-release.yml"
TAG_PATTERN = r"^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$"
REPOSITORY = re.compile(r"^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$")
LOGIN = re.compile(r"^[A-Za-z0-9](?:[A-Za-z0-9-]{0,37}[A-Za-z0-9])?$")
SHA = re.compile(r"^[0-9a-f]{40}$")


class GovernanceError(ValueError):
    """Connected governance is absent, inaccessible, stale, or unsafe."""


def mapping(value: Any, label: str) -> dict[str, Any]:
    if not isinstance(value, dict):
        raise GovernanceError(f"{label} must be one JSON object")
    return value


def sequence(value: Any, label: str) -> list[Any]:
    if not isinstance(value, list):
        raise GovernanceError(f"{label} must be one JSON array")
    return value


def positive_id(value: Any, label: str) -> int:
    if isinstance(value, bool):
        raise GovernanceError(f"{label} must be a positive integer")
    try:
        result = int(value)
    except (TypeError, ValueError) as error:
        raise GovernanceError(f"{label} must be a positive integer") from error
    if result <= 0 or str(value) != str(result):
        raise GovernanceError(f"{label} must be a positive integer")
    return result


def login(value: Any, label: str) -> str:
    if not isinstance(value, str) or LOGIN.fullmatch(value) is None:
        raise GovernanceError(f"{label} must be a GitHub login")
    return value


def validate_environment(payload: Any) -> tuple[int, int]:
    environment = mapping(payload, ENVIRONMENT)
    if environment.get("name") != ENVIRONMENT:
        raise GovernanceError("Terraform module release environment name differs")
    environment_id = positive_id(environment.get("id"), f"{ENVIRONMENT}.id")
    if environment.get("can_admins_bypass") is not False:
        raise GovernanceError("Terraform module release administrator bypass must be disabled")
    if environment.get("deployment_branch_policy") != {
        "protected_branches": True,
        "custom_branch_policies": False,
    }:
        raise GovernanceError("Terraform module releases must accept protected branches only")

    by_type: dict[str, list[dict[str, Any]]] = {}
    for index, raw_rule in enumerate(
        sequence(environment.get("protection_rules"), f"{ENVIRONMENT}.protection_rules")
    ):
        rule = mapping(raw_rule, f"{ENVIRONMENT}.protection_rules[{index}]")
        rule_type = rule.get("type")
        if not isinstance(rule_type, str):
            raise GovernanceError("Environment protection rule type is absent")
        by_type.setdefault(rule_type, []).append(rule)
    expected_types = {"branch_policy", "required_reviewers", "wait_timer"}
    if set(by_type) != expected_types or any(len(found) != 1 for found in by_type.values()):
        raise GovernanceError(
            "Terraform module release protection must be exactly branch, reviewer, and wait rules"
        )
    if by_type["wait_timer"][0].get("wait_timer") != 5:
        raise GovernanceError("Terraform module release wait timer must be exactly five minutes")

    reviewer_rule = by_type["required_reviewers"][0]
    if reviewer_rule.get("prevent_self_review") is not True:
        raise GovernanceError("Terraform module release self-review must be disabled")
    reviewers = sequence(reviewer_rule.get("reviewers"), f"{ENVIRONMENT}.reviewers")
    if len(reviewers) != 1:
        raise GovernanceError("Terraform module release requires exactly one reviewer team")
    entry = mapping(reviewers[0], f"{ENVIRONMENT}.reviewers[0]")
    reviewer = mapping(entry.get("reviewer"), f"{ENVIRONMENT}.reviewer")
    if entry.get("type") != "Team" or reviewer.get("slug") != SECURITY_TEAM:
        raise GovernanceError("Terraform module release reviewer must be the Security team")
    security_team_id = positive_id(reviewer.get("id"), f"{ENVIRONMENT}.reviewer.id")
    return environment_id, security_team_id


def validate_run(
    payload: Any,
    repository: str,
    run_id: int,
    source_sha: str,
    dispatcher: str,
) -> None:
    run = mapping(payload, "workflow run")
    expected = {
        "id": run_id,
        "event": "workflow_dispatch",
        "head_branch": "main",
        "head_sha": source_sha,
        "path": WORKFLOW_PATH,
        "run_attempt": 1,
    }
    for field, value in expected.items():
        if run.get(field) != value:
            raise GovernanceError(f"workflow run {field} must equal {value}")
    repository_payload = mapping(run.get("repository"), "workflow run repository")
    if repository_payload.get("full_name") != repository:
        raise GovernanceError("workflow run repository identity differs")
    actor = mapping(run.get("actor"), "workflow run actor")
    if login(actor.get("login"), "workflow dispatcher").casefold() != dispatcher.casefold():
        raise GovernanceError("workflow run actor differs from the dispatcher")


def validate_approval_history(payload: Any, dispatcher: str, release_signer: str) -> dict[str, Any]:
    relevant: list[dict[str, Any]] = []
    for index, raw_review in enumerate(sequence(payload, "workflow approval history")):
        review = mapping(raw_review, f"workflow approval history[{index}]")
        environments = sequence(
            review.get("environments"), f"workflow approval history[{index}].environments"
        )
        names = {
            mapping(environment, "approval environment").get("name") for environment in environments
        }
        if ENVIRONMENT not in names:
            continue
        if len(environments) != 1 or names != {ENVIRONMENT}:
            raise GovernanceError(
                "Terraform module release approval must target only its environment"
            )
        if review.get("state") != "approved":
            raise GovernanceError("Terraform module release review history contains a non-approval")
        relevant.append(review)
    if len(relevant) != 1:
        raise GovernanceError("Terraform module release requires exactly one approved review")
    user = mapping(relevant[0].get("user"), "Terraform module release reviewer")
    if user.get("type") != "User":
        raise GovernanceError("Terraform module release approval must come from one human user")
    reviewer_login = login(user.get("login"), "Terraform module release reviewer")
    reviewer_id = positive_id(user.get("id"), "Terraform module release reviewer.id")
    if reviewer_login.casefold() == dispatcher.casefold():
        raise GovernanceError("workflow dispatcher cannot approve Terraform module publication")
    if reviewer_login.casefold() == release_signer.casefold():
        raise GovernanceError(
            "qualified Release signer cannot approve Terraform module publication"
        )
    return {"id": reviewer_id, "login": reviewer_login}


def validate_team_membership(payload: Any, reviewer: str) -> str:
    membership = mapping(payload, f"Security membership for {reviewer}")
    role = membership.get("role")
    if membership.get("state") != "active" or role not in {"member", "maintainer"}:
        raise GovernanceError(f"{reviewer} must be an active Security team member")
    return str(role)


def ruleset_candidate(summaries: Any, name: str) -> dict[str, Any]:
    candidates = [
        mapping(summary, "ruleset summary")
        for summary in sequence(summaries, "effective rulesets")
        if isinstance(summary, dict) and summary.get("name") == name
    ]
    if len(candidates) != 1:
        raise GovernanceError(f"{name} must be present exactly once in effective rulesets")
    return candidates[0]


def validate_ruleset_identity(summary: dict[str, Any], detail: Any, name: str) -> dict[str, Any]:
    ruleset_id = positive_id(summary.get("id"), f"{name}.id")
    ruleset = mapping(detail, name)
    expected = {
        "id": ruleset_id,
        "name": name,
        "enforcement": "active",
        "target": "tag",
        "source_type": "Organization",
    }
    for field, value in expected.items():
        if summary.get(field) != value and field != "name":
            raise GovernanceError(f"{name} summary {field} must equal {value}")
        if ruleset.get(field) != value:
            raise GovernanceError(f"{name} {field} must equal {value}")
    if ruleset.get("conditions") != {"ref_name": {"exclude": [], "include": ["refs/tags/v*"]}}:
        raise GovernanceError(f"{name} must target exactly refs/tags/v*")
    return ruleset


def validate_release_tag_creation(
    summary: dict[str, Any], detail: Any, release_team_id: int
) -> int:
    ruleset = validate_ruleset_identity(summary, detail, CREATION_RULESET)
    if ruleset.get("rules") != [{"type": "creation"}]:
        raise GovernanceError("release-tag-creation must contain only the creation restriction")
    if ruleset.get("bypass_actors") != [
        {
            "actor_id": release_team_id,
            "actor_type": "Team",
            "bypass_mode": "always",
        }
    ]:
        raise GovernanceError("release-tag-creation must have only the exact Release-team bypass")
    return positive_id(ruleset.get("id"), f"{CREATION_RULESET}.id")


def validate_tag_protection(summary: dict[str, Any], detail: Any) -> int:
    ruleset = validate_ruleset_identity(summary, detail, PROTECTION_RULESET)
    if ruleset.get("bypass_actors") != []:
        raise GovernanceError("tag-protection must have no bypass actors")
    rules = sequence(ruleset.get("rules"), "tag-protection.rules")
    by_type: dict[str, list[dict[str, Any]]] = {}
    for index, raw_rule in enumerate(rules):
        rule = mapping(raw_rule, f"tag-protection.rules[{index}]")
        rule_type = rule.get("type")
        if not isinstance(rule_type, str):
            raise GovernanceError("tag-protection rule type is absent")
        by_type.setdefault(rule_type, []).append(rule)
    expected_types = {"deletion", "non_fast_forward", "tag_name_pattern", "update"}
    if set(by_type) != expected_types or any(len(found) != 1 for found in by_type.values()):
        raise GovernanceError("tag-protection must contain the exact four immutability rules")
    for rule_type in ("deletion", "non_fast_forward", "update"):
        if by_type[rule_type][0] != {"type": rule_type}:
            raise GovernanceError(f"tag-protection {rule_type} rule has unexpected parameters")
    if by_type["tag_name_pattern"][0] != {
        "type": "tag_name_pattern",
        "parameters": {
            "name": "stable-semver-only",
            "negate": False,
            "operator": "regex",
            "pattern": TAG_PATTERN,
        },
    }:
        raise GovernanceError("tag-protection must require the exact stable SemVer pattern")
    return positive_id(ruleset.get("id"), f"{PROTECTION_RULESET}.id")


def validate_immutable_releases(payload: Any) -> None:
    settings = mapping(payload, "immutable releases")
    if settings.get("enabled") is not True or settings.get("enforced_by_owner") is not True:
        raise GovernanceError("immutable releases must be enabled and enforced by the owner")


class GitHubClient:
    def __init__(self, token: str, api_url: str = "https://api.github.com") -> None:
        self.token = token
        self.api_url = api_url

    def get(self, path: str) -> tuple[Any, dict[str, str]]:
        base = urllib.parse.urlsplit(self.api_url)
        url = urllib.parse.urljoin(self.api_url.rstrip("/") + "/", path.lstrip("/"))
        target = urllib.parse.urlsplit(url)
        if (target.scheme, target.netloc) != (base.scheme, base.netloc):
            raise GovernanceError("GitHub API pagination escaped the configured origin")
        request = urllib.request.Request(
            url,
            headers={
                "Accept": "application/vnd.github+json",
                "Authorization": f"Bearer {self.token}",
                "X-GitHub-Api-Version": "2026-03-10",
                "User-Agent": "mindclade-terraform-module-release-governance",
            },
        )
        try:
            with urllib.request.urlopen(request, timeout=30) as response:
                return json.load(response), dict(response.headers.items())
        except (urllib.error.HTTPError, urllib.error.URLError, json.JSONDecodeError) as error:
            status = getattr(error, "code", "unavailable")
            raise GovernanceError(f"GitHub API read failed for {path}: {status}") from error

    def get_pages(self, path: str) -> list[Any]:
        values: list[Any] = []
        next_path: str | None = path
        while next_path is not None:
            payload, headers = self.get(next_path)
            values.extend(sequence(payload, f"GitHub API page {next_path}"))
            next_path = None
            for part in headers.get("Link", headers.get("link", "")).split(","):
                if 'rel="next"' in part:
                    next_path = part.split(";", 1)[0].strip().strip("<>")
                    break
        return values


def verify_connected(
    client: GitHubClient,
    repository: str,
    release_team_id: int,
    run_id: int,
    source_sha: str,
    dispatcher: str,
    release_signer: str,
) -> dict[str, Any]:
    if REPOSITORY.fullmatch(repository) is None:
        raise GovernanceError("repository must be owner/name")
    if SHA.fullmatch(source_sha) is None:
        raise GovernanceError("source revision must be a full commit SHA")
    dispatcher = login(dispatcher, "workflow dispatcher")
    release_signer = login(release_signer, "qualified Release signer")

    organization = repository.split("/", 1)[0]
    environment = client.get(f"/repos/{repository}/environments/{ENVIRONMENT}")[0]
    environment_id, security_team_id = validate_environment(environment)
    validate_immutable_releases(client.get(f"/repos/{repository}/immutable-releases")[0])
    validate_run(
        client.get(f"/repos/{repository}/actions/runs/{run_id}")[0],
        repository,
        run_id,
        source_sha,
        dispatcher,
    )
    reviewer = validate_approval_history(
        client.get(f"/repos/{repository}/actions/runs/{run_id}/approvals")[0],
        dispatcher,
        release_signer,
    )
    membership_role = validate_team_membership(
        client.get(f"/orgs/{organization}/teams/{SECURITY_TEAM}/memberships/{reviewer['login']}")[
            0
        ],
        reviewer["login"],
    )

    summaries = client.get_pages(f"/repos/{repository}/rulesets?targets=tag&per_page=100")
    creation_summary = ruleset_candidate(summaries, CREATION_RULESET)
    creation_id = positive_id(creation_summary.get("id"), f"{CREATION_RULESET}.id")
    creation_id = validate_release_tag_creation(
        creation_summary,
        client.get(f"/repos/{repository}/rulesets/{creation_id}")[0],
        release_team_id,
    )
    protection_summary = ruleset_candidate(summaries, PROTECTION_RULESET)
    protection_id = positive_id(protection_summary.get("id"), f"{PROTECTION_RULESET}.id")
    protection_id = validate_tag_protection(
        protection_summary,
        client.get(f"/repos/{repository}/rulesets/{protection_id}")[0],
    )

    return {
        "approval": {
            "reviewer_id": reviewer["id"],
            "reviewer_login": reviewer["login"],
            "security_membership_role": membership_role,
            "state": "approved",
        },
        "dispatcher": dispatcher,
        "environment": {
            "can_admins_bypass": False,
            "custom_branch_policies": False,
            "id": environment_id,
            "name": ENVIRONMENT,
            "prevent_self_review": True,
            "protected_branches": True,
            "reviewer_team": SECURITY_TEAM,
            "security_team_id": security_team_id,
            "wait_timer_minutes": 5,
        },
        "immutable_releases": {"enabled": True, "enforced_by_owner": True},
        "qualified_release_signer": release_signer,
        "repository": repository,
        "rulesets": {
            CREATION_RULESET: {
                "bypass": {"mode": "always", "release_team_id": release_team_id},
                "enforcement": "active",
                "id": creation_id,
                "ref_include": ["refs/tags/v*"],
                "rules": ["creation"],
                "source_type": "Organization",
                "target": "tag",
            },
            PROTECTION_RULESET: {
                "bypass_actors": [],
                "enforcement": "active",
                "id": protection_id,
                "ref_include": ["refs/tags/v*"],
                "rules": ["deletion", "non_fast_forward", "tag_name_pattern", "update"],
                "semver_pattern": TAG_PATTERN,
                "source_type": "Organization",
                "target": "tag",
            },
        },
        "run_id": run_id,
        "schema_version": 1,
        "source_revision": source_sha,
    }


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--repository", default=os.environ.get("GITHUB_REPOSITORY", ""))
    parser.add_argument("--release-team-id", required=True)
    parser.add_argument("--run-id", required=True)
    parser.add_argument("--source-sha", required=True)
    parser.add_argument("--dispatcher", required=True)
    parser.add_argument("--release-signer", required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument(
        "--api-url", default=os.environ.get("GITHUB_API_URL", "https://api.github.com")
    )
    args = parser.parse_args()
    try:
        token = os.environ.get("GH_TOKEN", "")
        if not token:
            raise GovernanceError("GH_TOKEN is required for connected governance qualification")
        evidence = verify_connected(
            GitHubClient(token, args.api_url),
            args.repository,
            positive_id(args.release_team_id, "Release team ID"),
            positive_id(args.run_id, "workflow run ID"),
            args.source_sha,
            args.dispatcher,
            args.release_signer,
        )
        args.output.write_text(
            json.dumps(evidence, indent=2, sort_keys=True) + "\n", encoding="utf-8"
        )
    except (GovernanceError, OSError) as error:
        print(f"ERROR: {error}", file=sys.stderr)
        return 1
    print("Terraform module release governance passed")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
