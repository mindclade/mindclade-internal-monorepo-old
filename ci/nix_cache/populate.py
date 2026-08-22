#!/usr/bin/env python3
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

"""Build and publish the reviewed Linux Nix closure from a trusted main revision."""

from __future__ import annotations

import argparse
import json
import os
import platform
import re
import subprocess
import sys
import tempfile
from collections.abc import Mapping, Sequence
from pathlib import Path
from typing import Any
from urllib.parse import urlsplit

ROOT = Path(__file__).resolve().parents[2]
DEFAULT_CONTRACT = Path(__file__).with_name("population.json")
EXPECTED_FIELDS = {
    "activation",
    "attic_client_commit",
    "attic_server_name",
    "caller_workflow_ref",
    "dev_shells",
    "package_selector",
    "schema_version",
    "system",
    "trusted_events",
}
EXPECTED_SHELLS = ("ci-bazel", "ci-infra", "ci-lint", "ci-terraform")
EXPECTED_EVENTS = ("push", "schedule", "workflow_dispatch")
EXPECTED_ATTIC_CLIENT_COMMIT = "7a19204df10d606c5070e6bb72615c3461900c05"
FORBIDDEN_SECRET_ENV = {
    "ATTIC_SERVER_TOKEN_HS256_SECRET_BASE64",
    "ATTIC_SERVER_TOKEN_RS256_SECRET_BASE64",
    "ATTIC_SIGNING_KEY",
    "NIX_CACHE_SIGNING_KEY",
    "NIX_SECRET_KEY_FILE",
}
SENSITIVE_RUNTIME_ENV = FORBIDDEN_SECRET_ENV | {"ATTIC_CACHE_WRITE_TOKEN"}
ATTRIBUTE_RE = re.compile(r"^[A-Za-z0-9][A-Za-z0-9._+-]*$")
CACHE_NAME_RE = re.compile(r"^[a-z0-9][a-z0-9._-]{0,62}$")
PUBLIC_KEY_RE = re.compile(r"^[A-Za-z0-9._-]+:[A-Za-z0-9+/]+={0,2}$")


class PopulationError(RuntimeError):
    """A fail-closed population contract violation."""


def _object(value: Any, label: str) -> dict[str, Any]:
    if not isinstance(value, dict) or not all(isinstance(key, str) for key in value):
        raise PopulationError(f"{label} must be a JSON object")
    return value


def load_contract(path: Path) -> dict[str, Any]:
    try:
        payload = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as error:
        raise PopulationError(f"population contract is unreadable: {error}") from error
    contract = _object(payload, "population contract")
    if set(contract) != EXPECTED_FIELDS:
        raise PopulationError("population contract field inventory is not exact")
    activation = _object(contract["activation"], "activation")
    if set(activation) != {"enabled", "reason"}:
        raise PopulationError("activation field inventory is not exact")
    if not isinstance(activation["enabled"], bool):
        raise PopulationError("activation.enabled must be boolean")
    if not isinstance(activation["reason"], str) or not activation["reason"]:
        raise PopulationError("activation.reason must be non-empty")
    if contract["schema_version"] != 1 or contract["system"] != "x86_64-linux":
        raise PopulationError("only population contract v1 on x86_64-linux is supported")
    if tuple(contract["dev_shells"]) != EXPECTED_SHELLS:
        raise PopulationError("the exact four reviewed CI shells are required")
    if contract["package_selector"] != ".#packages.x86_64-linux.*":
        raise PopulationError("the complete x86_64-linux package set is required")
    if tuple(contract["trusted_events"]) != EXPECTED_EVENTS:
        raise PopulationError("trusted event inventory drifted")
    if contract["attic_server_name"] != "mindclade":
        raise PopulationError("Attic server identity drifted")
    if contract["attic_client_commit"] != EXPECTED_ATTIC_CLIENT_COMMIT:
        raise PopulationError("Attic client source identity drifted")
    if contract["caller_workflow_ref"] != (
        "mindclade/mindclade-internal-monorepo/.github/workflows/nix-cache.yml@refs/heads/main"
    ):
        raise PopulationError("trusted caller workflow identity drifted")
    return contract


def plan(contract: Mapping[str, Any]) -> dict[str, Any]:
    return {
        "activation": dict(contract["activation"]),
        "attic_client_commit": contract["attic_client_commit"],
        "client_signing_key_in_scope": False,
        "dev_shell_installables": [
            f".#devShells.x86_64-linux.{name}" for name in contract["dev_shells"]
        ],
        "package_selector": contract["package_selector"],
        "system": contract["system"],
        "trusted_events": list(contract["trusted_events"]),
    }


def _required(environment: Mapping[str, str], name: str) -> str:
    value = environment.get(name, "")
    if not value:
        raise PopulationError(f"required environment value is absent: {name}")
    return value


def _sanitized_environment(environment: Mapping[str, str]) -> dict[str, str]:
    return {name: value for name, value in environment.items() if name not in SENSITIVE_RUNTIME_ENV}


def _validate_endpoint(raw: str) -> None:
    endpoint = urlsplit(raw)
    if (
        endpoint.scheme != "https"
        or not endpoint.hostname
        or endpoint.username is not None
        or endpoint.password is not None
        or endpoint.query
        or endpoint.fragment
        or endpoint.hostname in {"localhost", "127.0.0.1", "::1"}
        or endpoint.hostname.endswith(".invalid")
    ):
        raise PopulationError("Attic endpoint must be a credential-free qualified HTTPS URL")


def _require_caller_workflow_present(contract: Mapping[str, Any], *, repo: Path = ROOT) -> None:
    """The trusted caller must be a workflow that exists in this repository.

    load_contract pins caller_workflow_ref to an exact string, and authorize compares the
    runtime GITHUB_WORKFLOW_REF against it. Neither establishes that the named workflow EXISTS
    -- and it does not: the contract has always referenced
    .github/workflows/nix-cache.yml, which was never added, because activation is staged.

    While activation stays disabled that is correct and this check never runs; authorize
    refuses on the activation gate several lines above. What it prevents is enabling activation
    and discovering the gap at first publish, when a real token and a real endpoint are already
    in scope. The trust boundary here is a workflow identity, so a dangling one is the same
    class of defect as the runner group restricted to a workflow ref nobody publishes.

    Path only. The `owner/repo` prefix is already constrained by the GITHUB_REPOSITORY check,
    and the `@ref` suffix names the branch the workflow runs FROM, which is not a path in this
    checkout.
    """
    reference = str(contract["caller_workflow_ref"])
    path_part = reference.split("@", 1)[0]
    _, separator, workflow_path = path_part.partition("/.github/workflows/")
    relative = Path(workflow_path)
    if (
        not separator
        or not workflow_path
        or relative.is_absolute()
        or len(relative.parts) != 1
        or relative.name != workflow_path
        or relative.suffix not in {".yml", ".yaml"}
    ):
        raise PopulationError(
            "caller workflow reference must name one canonical .github/workflows file: " + reference
        )
    candidate = repo / ".github" / "workflows" / workflow_path
    if candidate.is_symlink() or not candidate.is_file():
        raise PopulationError(
            f"trusted caller workflow does not exist in this repository: "
            f".github/workflows/{workflow_path}"
        )


def authorize(
    contract: Mapping[str, Any],
    environment: Mapping[str, str],
    *,
    repo: Path = ROOT,
) -> None:
    if contract["activation"]["enabled"] is not True:
        raise PopulationError(f"population is blocked: {contract['activation']['reason']}")
    if environment.get("MINDCLADE_NIX_CACHE_ACTIVATED") != "true":
        raise PopulationError("runtime activation acknowledgement is absent")
    forbidden = sorted(name for name in FORBIDDEN_SECRET_ENV if name in environment)
    if forbidden:
        raise PopulationError(
            "client scope contains forbidden server/signing secret variables: "
            + ", ".join(forbidden)
        )
    if _required(environment, "GITHUB_REPOSITORY") != "mindclade/mindclade-internal-monorepo":
        raise PopulationError("only the canonical monorepo may publish")
    if _required(environment, "GITHUB_WORKFLOW_REF") != contract["caller_workflow_ref"]:
        raise PopulationError("caller workflow must be the protected main cache workflow")
    if _required(environment, "GITHUB_EVENT_NAME") not in contract["trusted_events"]:
        raise PopulationError("untrusted GitHub event cannot publish")
    if _required(environment, "GITHUB_REF") != "refs/heads/main":
        raise PopulationError("only the main branch may publish")
    if environment.get("GITHUB_REF_PROTECTED") != "true":
        raise PopulationError("the publishing ref must be protected")
    if environment.get("RUNNER_OS") != "Linux" or platform.machine().lower() not in {
        "amd64",
        "x86_64",
    }:
        raise PopulationError("population requires a native x86_64 Linux runner")

    current_head = subprocess.run(
        ["git", "rev-parse", "HEAD"],
        cwd=repo,
        check=True,
        capture_output=True,
        text=True,
        env=_sanitized_environment(environment),
    ).stdout.strip()
    if _required(environment, "GITHUB_SHA") != current_head:
        raise PopulationError("checked-out HEAD does not match GITHUB_SHA")
    for command in (["git", "diff", "--quiet"], ["git", "diff", "--cached", "--quiet"]):
        if (
            subprocess.run(
                command,
                cwd=repo,
                check=False,
                env=_sanitized_environment(environment),
            ).returncode
            != 0
        ):
            raise PopulationError("publishing checkout must be clean")
    untracked = subprocess.run(
        ["git", "ls-files", "--others", "--exclude-standard"],
        cwd=repo,
        check=True,
        capture_output=True,
        text=True,
        env=_sanitized_environment(environment),
    ).stdout.strip()
    if untracked:
        raise PopulationError("publishing checkout contains untracked files")

    endpoint = _required(environment, "ATTIC_SERVER_ENDPOINT")
    _validate_endpoint(endpoint)
    if not CACHE_NAME_RE.fullmatch(_required(environment, "ATTIC_CACHE_NAME")):
        raise PopulationError("Attic cache name is invalid")
    if not PUBLIC_KEY_RE.fullmatch(_required(environment, "NIX_CACHE_TRUSTED_PUBLIC_KEY")):
        raise PopulationError("trusted public key is invalid")
    token = _required(environment, "ATTIC_CACHE_WRITE_TOKEN")
    if any(character.isspace() for character in token):
        raise PopulationError("Attic write token must not contain whitespace")

    # Last, deliberately. This is the only check here that touches the filesystem, and every
    # cheaper environment check above should report its own specific refusal rather than be
    # masked by this one.
    _require_caller_workflow_present(contract, repo=repo)


def _run(
    command: Sequence[str],
    *,
    repo: Path,
    environment: Mapping[str, str] | None = None,
    capture_output: bool = False,
) -> subprocess.CompletedProcess[str]:
    return subprocess.run(
        list(command),
        cwd=repo,
        env=_sanitized_environment(os.environ if environment is None else environment),
        check=True,
        capture_output=capture_output,
        text=True,
    )


def package_installables(repo: Path = ROOT) -> list[str]:
    completed = _run(
        [
            "nix",
            "eval",
            "--json",
            "--no-write-lock-file",
            ".#packages.x86_64-linux",
            "--apply",
            "builtins.attrNames",
        ],
        repo=repo,
        capture_output=True,
    )
    try:
        attributes = json.loads(completed.stdout)
    except json.JSONDecodeError as error:
        raise PopulationError("Nix package inventory was not valid JSON") from error
    if not isinstance(attributes, list) or not attributes:
        raise PopulationError("x86_64-linux package inventory must be non-empty")
    if not all(isinstance(name, str) and ATTRIBUTE_RE.fullmatch(name) for name in attributes):
        raise PopulationError("x86_64-linux package inventory contains an invalid attribute")
    return [f".#packages.x86_64-linux.{name}" for name in sorted(set(attributes))]


def build_store_paths(contract: Mapping[str, Any], repo: Path = ROOT) -> list[str]:
    installables = [
        f".#devShells.x86_64-linux.{name}" for name in contract["dev_shells"]
    ] + package_installables(repo)
    completed = _run(
        [
            "nix",
            "build",
            "--no-link",
            "--print-out-paths",
            "--no-write-lock-file",
            *installables,
        ],
        repo=repo,
        capture_output=True,
    )
    paths = sorted(set(completed.stdout.splitlines()))
    if not paths or not all(path.startswith("/nix/store/") for path in paths):
        raise PopulationError("Nix build did not return an exact store-path inventory")
    return paths


def publish(
    contract: Mapping[str, Any],
    environment: Mapping[str, str],
    store_paths: Sequence[str],
    *,
    repo: Path = ROOT,
) -> None:
    with tempfile.TemporaryDirectory(prefix="mindclade-attic-client-") as temporary:
        client_environment = _sanitized_environment(environment)
        client_environment["HOME"] = temporary
        client_environment["XDG_CONFIG_HOME"] = str(Path(temporary) / "config")
        attic_directory = Path(client_environment["XDG_CONFIG_HOME"]) / "attic"
        attic_directory.mkdir(parents=True, mode=0o700)
        token_path = attic_directory / "write-token"
        token_descriptor = os.open(
            token_path,
            os.O_WRONLY | os.O_CREAT | os.O_EXCL,
            0o600,
        )
        with os.fdopen(token_descriptor, "w", encoding="utf-8") as token_file:
            token_file.write(environment["ATTIC_CACHE_WRITE_TOKEN"])
        config_path = attic_directory / "config.toml"
        config_descriptor = os.open(
            config_path,
            os.O_WRONLY | os.O_CREAT | os.O_EXCL,
            0o600,
        )
        with os.fdopen(config_descriptor, "w", encoding="utf-8") as config_file:
            config_file.write(
                'default-server = "mindclade"\n'
                "[servers.mindclade]\n"
                f"endpoint = {json.dumps(environment['ATTIC_SERVER_ENDPOINT'])}\n"
                f"token-file = {json.dumps(str(token_path))}\n"
            )
        info = _run(
            ["attic", "cache", "info", environment["ATTIC_CACHE_NAME"]],
            repo=repo,
            environment=client_environment,
            capture_output=True,
        )
        rendered_info = f"{info.stdout}\n{info.stderr}"
        if "Public: false" not in rendered_info:
            raise PopulationError("Attic cache must remain private")
        if f"Public Key: {environment['NIX_CACHE_TRUSTED_PUBLIC_KEY']}" not in rendered_info:
            raise PopulationError("Attic public key does not match the reviewed client key")
        _run(
            ["attic", "push", environment["ATTIC_CACHE_NAME"], *store_paths],
            repo=repo,
            environment=client_environment,
        )


def main(argv: Sequence[str] | None = None) -> int:
    parser = argparse.ArgumentParser()
    mode = parser.add_mutually_exclusive_group(required=True)
    mode.add_argument("--plan", action="store_true")
    mode.add_argument("--execute", action="store_true")
    parser.add_argument("--contract", type=Path, default=DEFAULT_CONTRACT)
    args = parser.parse_args(argv)

    try:
        contract = load_contract(args.contract)
        if args.plan:
            print(json.dumps(plan(contract), indent=2, sort_keys=True))
            return 0
        authorize(contract, os.environ)
        paths = build_store_paths(contract)
        publish(contract, os.environ, paths)
    except (PopulationError, subprocess.CalledProcessError) as error:
        print(f"nix-cache population failed: {error}", file=sys.stderr)
        return 1
    print(f"published {len(paths)} root store path(s) from trusted main")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
