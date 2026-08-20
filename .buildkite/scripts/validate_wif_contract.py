#!/usr/bin/env python3
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Validate the source and runtime halves of Mindclade's Buildkite WIF canary.

The checked-in contract deliberately supports only two coherent states: every live identifier
is absent, or every identifier is present and exact. A partial activation is more dangerous
than a clean failure because it encourages operators to widen IAM until one lane happens to
work. Runtime validation additionally compares Buildkite's immutable job variables with the
reviewed IDs before any OIDC token is requested.
"""

from __future__ import annotations

import argparse
import json
import os
import re
import subprocess
import sys
from collections.abc import Mapping, Sequence
from pathlib import Path
from typing import Any

try:
    import yaml
except ModuleNotFoundError as exc:  # pragma: no cover - exercised by the declared Nix shell.
    raise SystemExit(
        "PyYAML is required; run this validator through nix develop .#ci-infra"
    ) from exc


ROOT = Path(__file__).resolve().parents[2]
DEFAULT_CONTRACT = ROOT / ".buildkite/contracts/wif-preflight.json"

UUID = re.compile(r"^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$")
COMMIT = re.compile(r"^[0-9a-f]{40}$")
WIF_AUDIENCE = re.compile(
    r"^https://iam\.googleapis\.com/projects/[0-9]+/locations/global/"
    r"workloadIdentityPools/buildkite/providers/buildkite$"
)

NOTICE = [
    "Copyright © 2026 Mindclade, LLC. All Rights Reserved.",
    "Mindclade Proprietary and Confidential.",
    "SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary",
]

PIPELINE_CONTRACT = {
    "build": {
        "slug": "mindclade-artifact-build",
        "definition": ".buildkite/pipelines/artifact-build.yml",
        "step_key": "artifact-build",
        "denied_step_key": "artifact-build-wif-denied",
        "service_account_prefix": "sa-artifact-builder@",
    },
    "qualification": {
        "slug": "mindclade-artifact-qualify",
        "definition": ".buildkite/pipelines/artifact-qualify.yml",
        "step_key": "artifact-qualify",
        "denied_step_key": "artifact-qualify-wif-denied",
        "service_account_prefix": "sa-artifact-qualifier@",
    },
    "promotion": {
        "slug": "mindclade-artifact-promote",
        "definition": ".buildkite/pipelines/artifact-promote.yml",
        "step_key": "artifact-promote",
        "denied_step_key": "artifact-promote-wif-denied",
        "service_account_prefix": "sa-artifact-promoter@",
    },
}

EXPECTED_REPOSITORIES = [
    "git@github.com:mindclade/mindclade-internal-monorepo.git",
    "https://github.com/mindclade/mindclade-internal-monorepo.git",
]
EXPECTED_SOURCES = ["api", "webhook"]
EXPECTED_QUEUE_FIXED = {"key": "mindclade-artifact-private", "runner_environment": "self-hosted"}


class ContractError(ValueError):
    """A checked-in or runtime trust invariant is not satisfied."""


def require(condition: bool, message: str) -> None:
    if not condition:
        raise ContractError(message)


def load_contract(path: Path = DEFAULT_CONTRACT) -> dict[str, Any]:
    try:
        value = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        raise ContractError(f"cannot read Buildkite WIF contract {path}: {exc}") from exc
    require(isinstance(value, dict), "Buildkite WIF contract must be a JSON object")
    return value


def _validate_active_identifiers(contract: Mapping[str, Any]) -> None:
    organization_id = contract.get("organization_id")
    audience = contract.get("wif_provider_audience")
    require(
        isinstance(organization_id, str) and UUID.fullmatch(organization_id) is not None,
        "active contract organization_id must be an immutable UUID",
    )
    require(
        isinstance(audience, str) and WIF_AUDIENCE.fullmatch(audience) is not None,
        "active contract wif_provider_audience must be the exact bootstrap provider URL",
    )
    for key in ("cluster_id", "id"):
        value = contract["queue"].get(key)
        require(
            isinstance(value, str) and UUID.fullmatch(value) is not None,
            f"active queue.{key} must be an immutable UUID",
        )

    pipeline_ids: list[str] = []
    service_accounts: list[str] = []
    for stage, fixed in PIPELINE_CONTRACT.items():
        configured = contract["pipelines"][stage]
        pipeline_id = configured.get("id")
        service_account = configured.get("service_account")
        require(
            isinstance(pipeline_id, str) and UUID.fullmatch(pipeline_id) is not None,
            f"active {stage} pipeline id must be an immutable UUID",
        )
        require(
            isinstance(service_account, str)
            and service_account.startswith(fixed["service_account_prefix"])
            and re.fullmatch(
                r"[a-z0-9-]+@[a-z][a-z0-9-]{4,28}[a-z0-9]\.iam\.gserviceaccount\.com",
                service_account,
            )
            is not None,
            f"active {stage} service account must be its dedicated normal-plane identity",
        )
        pipeline_ids.append(pipeline_id)
        service_accounts.append(service_account)

    require(
        len(set(pipeline_ids)) == len(pipeline_ids),
        "build, qualification, and promotion must use distinct pipeline UUIDs",
    )
    require(
        len(set(service_accounts)) == len(service_accounts),
        "build, qualification, and promotion must use distinct service accounts",
    )


def validate_contract(contract: Mapping[str, Any]) -> None:
    expected_keys = {
        "_comment",
        "schema_version",
        "activation_state",
        "issuer",
        "organization_slug",
        "organization_id",
        "wif_provider_audience",
        "default_branch",
        "allowed_build_sources",
        "repository_urls",
        "queue",
        "pipelines",
    }
    require(
        set(contract) == expected_keys,
        f"contract keys must be exact; found {sorted(set(contract) ^ expected_keys)}",
    )
    require(contract["_comment"] == NOTICE, "contract proprietary notice changed")
    require(contract["schema_version"] == 1, "unsupported Buildkite WIF contract version")
    require(contract["issuer"] == "https://agent.buildkite.com", "Buildkite OIDC issuer changed")
    require(contract["organization_slug"] == "mindclade", "Buildkite organization slug changed")
    require(contract["default_branch"] == "main", "WIF is restricted to main")
    require(
        contract["allowed_build_sources"] == EXPECTED_SOURCES,
        "only API and webhook builds are accepted by bootstrap",
    )
    require(
        contract["repository_urls"] == EXPECTED_REPOSITORIES,
        "pipeline repository must be the canonical Mindclade monorepo",
    )
    queue = contract.get("queue")
    require(
        isinstance(queue, dict) and set(queue) == {"cluster_id", "id", "key", "runner_environment"},
        "queue contract keys are not exact",
    )
    for key, expected in EXPECTED_QUEUE_FIXED.items():
        require(
            queue[key] == expected,
            "artifact identity canaries require the dedicated self-hosted queue",
        )

    pipelines = contract.get("pipelines")
    require(
        isinstance(pipelines, dict) and set(pipelines) == set(PIPELINE_CONTRACT),
        "contract must define exactly build, qualification, and promotion pipelines",
    )
    for stage, fixed in PIPELINE_CONTRACT.items():
        configured = pipelines[stage]
        require(isinstance(configured, dict), f"{stage} pipeline contract must be an object")
        require(
            set(configured)
            == {"slug", "id", "definition", "step_key", "denied_step_key", "service_account"},
            f"{stage} pipeline contract keys are not exact",
        )
        for key in ("slug", "definition", "step_key", "denied_step_key"):
            require(
                configured[key] == fixed[key], f"{stage}.{key} changed from bootstrap's contract"
            )

    state = contract.get("activation_state")
    require(
        state in {"unprovisioned", "active"}, "activation_state must be unprovisioned or active"
    )
    if state == "unprovisioned":
        live_values = [
            contract.get("organization_id"),
            contract.get("wif_provider_audience"),
            queue.get("cluster_id"),
            queue.get("id"),
        ]
        live_values.extend(pipelines[stage].get("id") for stage in PIPELINE_CONTRACT)
        live_values.extend(pipelines[stage].get("service_account") for stage in PIPELINE_CONTRACT)
        require(
            all(value is None for value in live_values),
            "unprovisioned contract must not contain a partial live identity",
        )
    else:
        _validate_active_identifiers(contract)


def _expected_step(stage: str, expectation: str, configured: Mapping[str, Any]) -> dict[str, Any]:
    key_name = "step_key" if expectation == "allowed" else "denied_step_key"
    key = configured[key_name]
    artifact = f".buildkite-evidence/wif-{stage}-{expectation}.json"
    return {
        "label": (
            f":closed_lock_with_key: {stage} WIF accepts the exact contract"
            if expectation == "allowed"
            else f":no_entry: {stage} WIF rejects an untrusted step"
        ),
        "key": key,
        "branches": "main",
        "agents": {"queue": EXPECTED_QUEUE_FIXED["key"]},
        "timeout_in_minutes": 10,
        "command": f".buildkite/scripts/wif-preflight.sh {stage} {expectation}",
        "artifact_paths": [artifact],
    }


def validate_pipeline_files(contract: Mapping[str, Any], root: Path = ROOT) -> None:
    for stage, configured in contract["pipelines"].items():
        path = root / configured["definition"]
        try:
            pipeline = yaml.safe_load(path.read_text(encoding="utf-8"))
        except (OSError, yaml.YAMLError) as exc:
            raise ContractError(f"cannot parse {configured['definition']}: {exc}") from exc
        require(
            isinstance(pipeline, dict) and set(pipeline) == {"checkout", "steps"},
            f"{configured['definition']} may define only checkout and steps",
        )
        require(
            pipeline["checkout"] == {"commit_verification": "strict"},
            f"{configured['definition']} must enforce strict branch/commit verification",
        )
        steps = pipeline["steps"]
        require(
            isinstance(steps, list) and len(steps) == 3,
            f"{configured['definition']} must contain denied, wait, and allowed steps",
        )
        require(
            steps[1] == "wait",
            f"{configured['definition']} must gate the allowed canary after denial",
        )
        require(
            steps[0] == _expected_step(stage, "denied", configured),
            f"{configured['definition']} denied step drifted from the exact canary",
        )
        require(
            steps[2] == _expected_step(stage, "allowed", configured),
            f"{configured['definition']} allowed step drifted from the exact canary",
        )


def validate_helper_source(root: Path = ROOT) -> None:
    helper_path = root / ".buildkite/scripts/wif-preflight.sh"
    try:
        helper = helper_path.read_text(encoding="utf-8")
    except OSError as exc:
        raise ContractError(f"cannot read WIF helper: {exc}") from exc

    required = (
        "set +x",
        "umask 077",
        "oidc request-token",
        '--audience "${wif_audience}"',
        "--subject-claim pipeline_id",
        "--claim organization_id",
        "--format gcp",
        "create-cred-config",
        "--credential-source-type=json",
        "--credential-source-field-name=id_token",
        '--service-account="${service_account}"',
        "gcloud auth login",
        "gcloud auth print-access-token",
        'grep -Fqi "invalid_grant"',
        'grep -Fqi "attribute condition"',
        'write_evidence "indeterminate_failure"',
    )
    for fragment in required:
        require(
            fragment in helper,
            f"WIF helper is missing required token-exchange fragment: {fragment}",
        )

    forbidden = (
        "sign-and-create",
        "container binauthz attestations create",
        "gcloud artifacts",
        "docker push",
        "crane copy",
        "kubectl ",
        "terraform ",
        "terragrunt ",
        "pipeline upload",
        "BUILDKITE_AGENT_ACCESS_TOKEN",
        "service-account-key",
        "credentials.json",
    )
    lowered = helper.lower()
    for fragment in forbidden:
        require(
            fragment.lower() not in lowered,
            f"WIF canary contains forbidden privileged operation: {fragment}",
        )


def validate_source(contract_path: Path = DEFAULT_CONTRACT, root: Path = ROOT) -> dict[str, Any]:
    contract = load_contract(contract_path)
    validate_contract(contract)
    validate_pipeline_files(contract, root)
    validate_helper_source(root)
    return contract


def _env(environ: Mapping[str, str], name: str) -> str:
    value = environ.get(name, "")
    require(bool(value), f"required immutable Buildkite variable is absent: {name}")
    return value


def validate_runtime(
    contract: Mapping[str, Any],
    stage: str,
    expectation: str,
    environ: Mapping[str, str],
    checkout_commit: str,
) -> tuple[str, str, str]:
    require(
        contract["activation_state"] == "active",
        "Buildkite WIF contract is unprovisioned; live token exchange is prohibited",
    )
    require(stage in PIPELINE_CONTRACT, f"unknown WIF stage: {stage}")
    require(expectation in {"allowed", "denied"}, f"unknown WIF expectation: {expectation}")

    configured = contract["pipelines"][stage]
    expected_step = (
        configured["step_key"] if expectation == "allowed" else configured["denied_step_key"]
    )
    checks = {
        "BUILDKITE_ORGANIZATION_ID": contract["organization_id"],
        "BUILDKITE_ORGANIZATION_SLUG": contract["organization_slug"],
        "BUILDKITE_PIPELINE_ID": configured["id"],
        "BUILDKITE_PIPELINE_SLUG": configured["slug"],
        "BUILDKITE_PIPELINE_PROVIDER": "github",
        "BUILDKITE_PIPELINE_DEFAULT_BRANCH": contract["default_branch"],
        "BUILDKITE_BRANCH": contract["default_branch"],
        "BUILDKITE_CLUSTER_ID": contract["queue"]["cluster_id"],
        "BUILDKITE_COMPUTE_TYPE": contract["queue"]["runner_environment"],
        "BUILDKITE_COMMIT_RESOLVED": "true",
        "BUILDKITE_GIT_COMMIT_VERIFICATION": "strict",
        "BUILDKITE_STEP_KEY": expected_step,
        "BUILDKITE_AGENT_META_DATA_QUEUE": contract["queue"]["key"],
        "BUILDKITE_PULL_REQUEST": "false",
    }
    for variable, expected in checks.items():
        require(
            _env(environ, variable) == expected,
            f"{variable} does not match the reviewed immutable contract",
        )

    require(
        _env(environ, "BUILDKITE_SOURCE") in contract["allowed_build_sources"],
        "Buildkite WIF accepts only API or webhook builds",
    )
    require(
        _env(environ, "BUILDKITE_REPO") in contract["repository_urls"],
        "Buildkite pipeline is not connected to the canonical monorepo",
    )
    commit = _env(environ, "BUILDKITE_COMMIT").lower()
    require(COMMIT.fullmatch(commit) is not None, "BUILDKITE_COMMIT must be a full Git SHA-1")
    require(checkout_commit.lower() == commit, "checked-out HEAD does not match BUILDKITE_COMMIT")
    for variable in ("BUILDKITE_AGENT_ID", "BUILDKITE_JOB_ID", "BUILDKITE_STEP_ID"):
        require(
            UUID.fullmatch(_env(environ, variable).lower()) is not None,
            f"{variable} must be an immutable UUID",
        )

    audience = contract["wif_provider_audience"]
    provider_resource = audience.removeprefix("https://iam.googleapis.com/")
    return audience, provider_resource, configured["service_account"]


def git_head(root: Path) -> str:
    result = subprocess.run(
        ["git", "rev-parse", "HEAD"],
        cwd=root,
        check=True,
        capture_output=True,
        text=True,
    )
    return result.stdout.strip()


def parse_args(argv: Sequence[str] | None = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--contract", type=Path, default=DEFAULT_CONTRACT)
    parser.add_argument("--root", type=Path, default=ROOT)
    parser.add_argument("--runtime-stage", choices=tuple(PIPELINE_CONTRACT))
    parser.add_argument("--expectation", choices=("allowed", "denied"))
    parser.add_argument("--emit-runtime-values", action="store_true")
    args = parser.parse_args(argv)
    if bool(args.runtime_stage) != bool(args.expectation):
        parser.error("--runtime-stage and --expectation must be supplied together")
    if args.emit_runtime_values and not args.runtime_stage:
        parser.error("--emit-runtime-values requires runtime validation")
    return args


def main(argv: Sequence[str] | None = None) -> int:
    args = parse_args(argv)
    try:
        contract = validate_source(args.contract, args.root)
        if args.runtime_stage:
            values = validate_runtime(
                contract,
                args.runtime_stage,
                args.expectation,
                os.environ,
                git_head(args.root),
            )
            if args.emit_runtime_values:
                print("\n".join(values))
            else:
                print(
                    f"Buildkite {args.runtime_stage} {args.expectation} runtime contract is valid."
                )
        else:
            print(f"Buildkite WIF source contract is valid ({contract['activation_state']}).")
    except (ContractError, subprocess.CalledProcessError) as exc:
        print(f"Buildkite WIF contract rejected: {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
