#!/usr/bin/env python3
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

"""Generate and enforce the public Terraform module-interface contract."""

from __future__ import annotations

import argparse
import hashlib
import io
import json
import os
import re
import shutil
import subprocess
import sys
import tarfile
import tempfile
import tomllib
from collections.abc import Iterable
from pathlib import Path
from typing import Any

MANIFEST_SCHEMA_VERSION = 2
VERSION_POLICY_SCHEMA_VERSION = 1
MIGRATION_SCHEMA_VERSION = 2
MANIFEST_RELATIVE = Path("infra/terraform/governance/module-interfaces.json")
BASELINE_RELATIVE = Path("infra/terraform/governance/baselines/v0.1.1.json")
VERSION_RELATIVE = Path("infra/terraform/governance/version.toml")
CONFIG_RELATIVE = Path("infra/terraform/governance/.terraform-docs.yml")
MIGRATIONS_RELATIVE = Path("infra/terraform/governance/migrations")
TOOLCHAIN_RELATIVE = Path("tools/build/nix/toolchain-manifest.json")
BEGIN_MARKER = "<!-- BEGIN_TF_DOCS -->"
END_MARKER = "<!-- END_TF_DOCS -->"
SEMVER = re.compile(r"^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$")
BLOCK_START = re.compile(
    r"(?m)^\s*(variable|output|resource|module|moved|terraform)"
    r'(?:\s+"([^"]+)")?(?:\s+"([^"]+)")?\s*\{'
)
HEREDOC = re.compile(
    r"<<-?(?P<tag>[A-Za-z_][A-Za-z0-9_]*)[^\n]*\n"
    r"(?P<body>.*?)(?m:^\s*(?P=tag)\s*$)",
    re.DOTALL,
)


class GovernanceError(RuntimeError):
    """A deterministic contract or generator failure."""


def _run(command: list[str], *, cwd: Path | None = None) -> subprocess.CompletedProcess[str]:
    return subprocess.run(
        command,
        cwd=cwd,
        check=False,
        text=True,
        capture_output=True,
    )


def _run_bytes(
    command: list[str], *, cwd: Path | None = None
) -> subprocess.CompletedProcess[bytes]:
    return subprocess.run(command, cwd=cwd, check=False, capture_output=True)


def _read_json(path: Path) -> dict[str, Any]:
    try:
        value = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        raise GovernanceError(f"cannot read JSON {path}: {exc}") from exc
    if not isinstance(value, dict):
        raise GovernanceError(f"expected a JSON object in {path}")
    return value


def _json_text(value: dict[str, Any]) -> str:
    return json.dumps(value, indent=2, sort_keys=True, ensure_ascii=False) + "\n"


def _policy(repo: Path) -> dict[str, Any]:
    path = repo / VERSION_RELATIVE
    try:
        value = tomllib.loads(path.read_text(encoding="utf-8"))
    except (OSError, tomllib.TOMLDecodeError) as exc:
        raise GovernanceError(f"cannot read version policy {path}: {exc}") from exc
    required = {
        "schema_version",
        "contract_version",
        "status",
        "baseline_version",
        "baseline_commit",
    }
    missing = required - set(value)
    if missing:
        raise GovernanceError(f"version policy is missing {sorted(missing)}")
    if value["schema_version"] != VERSION_POLICY_SCHEMA_VERSION:
        raise GovernanceError("unsupported Terraform interface version-policy schema")
    for field in ("contract_version", "baseline_version"):
        _parse_semver(str(value[field]), field)
    if value["status"] not in {"planned", "released"}:
        raise GovernanceError("version policy status must be planned or released")
    if not re.fullmatch(r"[0-9a-f]{40}", str(value["baseline_commit"])):
        raise GovernanceError("baseline_commit must be an immutable 40-character Git SHA")
    return value


def _parse_semver(value: str, field: str = "version") -> tuple[int, int, int]:
    match = SEMVER.fullmatch(value)
    if not match:
        raise GovernanceError(f"{field} must be strict SemVer major.minor.patch: {value!r}")
    return tuple(int(part) for part in match.groups())  # type: ignore[return-value]


def terraform_directories(repo: Path) -> list[tuple[str, Path]]:
    modules_root = repo / "infra/terraform/modules"
    result = [
        (path.name, path)
        for path in sorted(modules_root.iterdir())
        if path.is_dir() and any(path.glob("*.tf"))
    ]
    dns_hub = repo / "infra/terraform/environments/dns_hub"
    if dns_hub.is_dir() and any(dns_hub.glob("*.tf")):
        result.append(("dns_hub", dns_hub))
    return result


def _terraform_docs_version(executable: str) -> str:
    completed = _run([executable, "version"])
    if completed.returncode != 0:
        raise GovernanceError(
            f"terraform-docs version failed ({completed.returncode}): {completed.stderr.strip()}"
        )
    match = re.search(r"\bv?(\d+\.\d+\.\d+)\b", completed.stdout + completed.stderr)
    if not match:
        raise GovernanceError("could not parse terraform-docs version")
    return match.group(1)


def _enforce_tool_version(repo: Path, executable: str) -> str:
    actual = _terraform_docs_version(executable)
    manifest = _read_json(repo / TOOLCHAIN_RELATIVE)
    expected = manifest.get("ciTools", {}).get("terraform-docs")
    if actual != expected:
        raise GovernanceError(
            f"terraform-docs version mismatch: Nix manifest requires {expected!r}, found {actual!r}"
        )
    return actual


def _block_end(text: str, opening_brace: int) -> int:
    depth = 0
    quote = False
    escaped = False
    line_comment = False
    block_comment = False
    index = opening_brace
    while index < len(text):
        char = text[index]
        nxt = text[index + 1] if index + 1 < len(text) else ""
        if line_comment:
            if char == "\n":
                line_comment = False
            index += 1
            continue
        if block_comment:
            if char == "*" and nxt == "/":
                block_comment = False
                index += 2
                continue
            index += 1
            continue
        if quote:
            if escaped:
                escaped = False
            elif char == "\\":
                escaped = True
            elif char == '"':
                quote = False
            index += 1
            continue
        if char == "#" or (char == "/" and nxt == "/"):
            line_comment = True
            index += 2 if char == "/" else 1
            continue
        if char == "/" and nxt == "*":
            block_comment = True
            index += 2
            continue
        if char == '"':
            quote = True
        elif char == "{":
            depth += 1
        elif char == "}":
            depth -= 1
            if depth == 0:
                return index
        index += 1
    raise GovernanceError("unterminated HCL block while extracting interface metadata")


def _mask_heredocs(text: str) -> str:
    """Hide heredoc content from the small brace scanner without changing offsets."""
    characters = list(text)
    for match in HEREDOC.finditer(text):
        for index in range(match.start(), match.end()):
            if characters[index] != "\n":
                characters[index] = " "
    return "".join(characters)


def _heredoc_end(text: str, start: int) -> int | None:
    opener = re.match(r"<<-?([A-Za-z_][A-Za-z0-9_]*)[^\n]*\n", text[start:])
    if not opener:
        return None
    tag = re.escape(opener.group(1))
    body_start = start + opener.end()
    terminator = re.search(rf"(?m)^\s*{tag}\s*$", text[body_start:])
    if not terminator:
        raise GovernanceError("unterminated HCL heredoc while extracting interface metadata")
    end = body_start + terminator.end()
    if end < len(text) and text[end] == "\n":
        end += 1
    return end


def _mask_comments(text: str) -> str:
    """Mask HCL comments without changing offsets or treating strings/heredocs as comments."""
    characters = list(text)
    quote = False
    escaped = False
    index = 0
    while index < len(text):
        char = text[index]
        nxt = text[index + 1] if index + 1 < len(text) else ""
        if quote:
            if escaped:
                escaped = False
            elif char == "\\":
                escaped = True
            elif char == '"':
                quote = False
            index += 1
            continue
        if char == '"':
            quote = True
            index += 1
            continue
        if char == "<" and nxt == "<":
            heredoc_end = _heredoc_end(text, index)
            if heredoc_end is not None:
                index = heredoc_end
                continue
        if char == "#" or (char == "/" and nxt == "/"):
            end = text.find("\n", index)
            if end == -1:
                end = len(text)
            for position in range(index, end):
                characters[position] = " "
            index = end
            continue
        if char == "/" and nxt == "*":
            end = text.find("*/", index + 2)
            if end == -1:
                raise GovernanceError("unterminated HCL block comment")
            end += 2
            for position in range(index, end):
                if characters[position] != "\n":
                    characters[position] = " "
            index = end
            continue
        index += 1
    return "".join(characters)


def _blocks(
    directory: Path, kinds: set[str] | None = None
) -> Iterable[tuple[str, str | None, str | None, str]]:
    for path in sorted(directory.glob("*.tf")):
        text = path.read_text(encoding="utf-8")
        masked = _mask_heredocs(_mask_comments(text))
        for match in BLOCK_START.finditer(masked):
            kind, first, second = match.group(1), match.group(2), match.group(3)
            if kinds is not None and kind not in kinds:
                continue
            opening = masked.find("{", match.start(), match.end())
            end = _block_end(masked, opening)
            yield kind, first, second, text[opening + 1 : end]


def _nested_blocks(body: str, kind: str) -> list[str]:
    masked = _mask_heredocs(_mask_comments(body))
    pattern = re.compile(rf"(?m)^\s*{re.escape(kind)}\s*\{{")
    result = []
    for match in pattern.finditer(masked):
        opening = masked.find("{", match.start(), match.end())
        end = _block_end(masked, opening)
        result.append(body[opening + 1 : end])
    return result


def _top_level_assignment(body: str, name: str) -> str | None:
    comment_masked = _mask_comments(body)
    masked = _mask_heredocs(comment_masked)
    match = re.search(rf"(?m)^\s*{re.escape(name)}\s*=", masked)
    if not match:
        return None
    start = masked.find("=", match.start(), match.end()) + 1
    original_index = start
    while original_index < len(body) and body[original_index].isspace():
        original_index += 1
    if body.startswith("<<", original_index):
        end = _heredoc_end(body, original_index)
        if end is None:
            raise GovernanceError(f"cannot parse heredoc assignment {name}")
        return body[start:end].strip()
    depths = {"(": 0, "[": 0, "{": 0}
    closing = {")": "(", "]": "[", "}": "{"}
    quote = False
    escaped = False
    index = start
    while index < len(masked):
        char = masked[index]
        if quote:
            if escaped:
                escaped = False
            elif char == "\\":
                escaped = True
            elif char == '"':
                quote = False
        elif char == '"':
            quote = True
        elif char in depths:
            depths[char] += 1
        elif char in closing:
            depths[closing[char]] -= 1
        elif char in {"\n", ","} and not any(depths.values()):
            return body[start:index].strip()
        index += 1
    return body[start:].strip()


def _normalize_hcl(fragment: str) -> str:
    text = _mask_comments(fragment).replace("\r\n", "\n")
    output: list[str] = []
    quote = False
    escaped = False
    index = 0
    while index < len(text):
        char = text[index]
        nxt = text[index + 1] if index + 1 < len(text) else ""
        if quote:
            output.append(char)
            if escaped:
                escaped = False
            elif char == "\\":
                escaped = True
            elif char == '"':
                quote = False
            index += 1
            continue
        if char == '"':
            quote = True
            output.append(char)
            index += 1
            continue
        if char == "<" and nxt == "<":
            heredoc_end = _heredoc_end(text, index)
            if heredoc_end is not None:
                output.append(text[index:heredoc_end].rstrip())
                index = heredoc_end
                continue
        if not char.isspace():
            output.append(char)
        index += 1
    return "".join(output)


def _fingerprint_hcl(fragment: str) -> str:
    normalized = _normalize_hcl(fragment)
    return "sha256:" + hashlib.sha256(normalized.encode("utf-8")).hexdigest()


def _sensitivity(directory: Path, kind: str) -> dict[str, bool]:
    result: dict[str, bool] = {}
    assignment = re.compile(r"(?m)^\s*sensitive\s*=\s*(true|false)\s*(?:#.*)?$")
    for _, name, _, body in _blocks(directory, {kind}):
        if name is None:
            continue
        match = assignment.search(body)
        result[name] = bool(match and match.group(1) == "true")
    return result


def _static_bool(body: str, name: str, default: bool) -> bool:
    value = _top_level_assignment(body, name)
    if value is None:
        return default
    normalized = _normalize_hcl(value)
    if normalized not in {"true", "false"}:
        raise GovernanceError(f"{name} must be a static boolean in a public interface block")
    return normalized == "true"


def _variable_behaviors(directory: Path) -> dict[str, dict[str, Any]]:
    result: dict[str, dict[str, Any]] = {}
    for _, name, _, body in _blocks(directory, {"variable"}):
        if name is None:
            continue
        validations = []
        for validation in _nested_blocks(body, "validation"):
            condition = _top_level_assignment(validation, "condition")
            error_message = _top_level_assignment(validation, "error_message")
            if condition is None or error_message is None:
                raise GovernanceError(
                    f"{directory}: variable {name} validation needs condition and error_message"
                )
            normalized = _normalize_hcl(condition) + "\0" + _normalize_hcl(error_message)
            validations.append("sha256:" + hashlib.sha256(normalized.encode("utf-8")).hexdigest())
        result[name] = {
            "ephemeral": _static_bool(body, "ephemeral", False),
            "nullable": _static_bool(body, "nullable", True),
            "sensitive": _static_bool(body, "sensitive", False),
            "validation_fingerprints": sorted(validations),
        }
    return result


def _output_behaviors(directory: Path) -> dict[str, dict[str, Any]]:
    result: dict[str, dict[str, Any]] = {}
    for _, name, _, body in _blocks(directory, {"output"}):
        if name is None:
            continue
        value = _top_level_assignment(body, "value")
        if value is None:
            raise GovernanceError(f"{directory}: output {name} has no value expression")
        result[name] = {
            "ephemeral": _static_bool(body, "ephemeral", False),
            "sensitive": _static_bool(body, "sensitive", False),
            "value_fingerprint": _fingerprint_hcl(value),
        }
    return result


def _canonical_provider_source(name: str, expression: str | None) -> str:
    if expression is None:
        return f"registry.terraform.io/hashicorp/{name}"
    try:
        source = json.loads(_normalize_hcl(expression))
    except json.JSONDecodeError as exc:
        raise GovernanceError(f"provider {name} source must be a static string") from exc
    if not isinstance(source, str) or not source:
        raise GovernanceError(f"provider {name} source must be a non-empty static string")
    if source.count("/") == 1:
        return f"registry.terraform.io/{source}"
    return source


def _canonical_provider_aliases(name: str, expression: str | None) -> list[str]:
    if expression is None:
        return []
    normalized = _normalize_hcl(expression)
    if not normalized.startswith("[") or not normalized.endswith("]"):
        raise GovernanceError(f"provider {name} configuration_aliases must be a static list")
    aliases = [item for item in normalized[1:-1].split(",") if item]
    expected_prefix = f"{name}."
    if any(not alias.startswith(expected_prefix) for alias in aliases):
        raise GovernanceError(
            f"provider {name} configuration_aliases must contain only {name} alias references"
        )
    return sorted(set(aliases))


def _provider_contracts(directory: Path, requirement_names: set[str]) -> dict[str, Any]:
    provider_bodies: list[str] = []
    for _, _, _, body in _blocks(directory, {"terraform"}):
        provider_bodies.extend(_nested_blocks(body, "required_providers"))
    result: dict[str, Any] = {}
    for name in sorted(requirement_names - {"terraform"}):
        expression = None
        for body in provider_bodies:
            candidate = _top_level_assignment(body, name)
            if candidate is not None:
                if expression is not None:
                    raise GovernanceError(f"{directory}: duplicate required provider {name}")
                expression = candidate
        source_expression = None
        aliases_expression = None
        if expression is not None and expression.lstrip().startswith("{"):
            stripped = expression.strip()
            if not stripped.endswith("}"):
                raise GovernanceError(f"{directory}: malformed required provider {name}")
            object_body = stripped[1:-1]
            source_expression = _top_level_assignment(object_body, "source")
            aliases_expression = _top_level_assignment(object_body, "configuration_aliases")
        result[name] = {
            "configuration_aliases": _canonical_provider_aliases(name, aliases_expression),
            "source": _canonical_provider_source(name, source_expression),
        }
    return result


def _managed_addresses(directory: Path) -> list[str]:
    addresses = []
    for _, resource_type, name, _ in _blocks(directory, {"resource"}):
        if resource_type and name:
            addresses.append(f"{resource_type}.{name}")
    return sorted(addresses)


def _moved_mappings(directory: Path) -> list[dict[str, str]]:
    mappings = []
    sources: set[str] = set()
    expression = re.compile(r"(?m)^\s*(from|to)\s*=\s*([^#\n]+?)\s*$")
    for _, _, _, body in _blocks(directory, {"moved"}):
        fields = {match.group(1): match.group(2).strip() for match in expression.finditer(body)}
        if set(fields) != {"from", "to"}:
            raise GovernanceError(
                f"{directory}: moved block must have static from and to addresses"
            )
        if fields["from"] in sources:
            raise GovernanceError(f"{directory}: duplicate moved source address {fields['from']}")
        sources.add(fields["from"])
        mappings.append({"from": fields["from"], "to": fields["to"]})
    return sorted(mappings, key=lambda item: (item["from"], item["to"]))


def _normalize_type(value: Any) -> str:
    return " ".join(str(value).split())


def _terraform_docs_json(executable: str, directory: Path) -> dict[str, Any]:
    completed = _run([executable, "json", "--show", "all", str(directory)])
    if completed.returncode != 0:
        raise GovernanceError(
            f"terraform-docs json failed for {directory}: {completed.stderr.strip()}"
        )
    try:
        value = json.loads(completed.stdout)
    except json.JSONDecodeError as exc:
        raise GovernanceError(
            f"terraform-docs emitted invalid JSON for {directory}: {exc}"
        ) from exc
    if not isinstance(value, dict):
        raise GovernanceError(f"terraform-docs emitted a non-object for {directory}")
    return value


def _module_interface(repo: Path, directory: Path, executable: str) -> dict[str, Any]:
    document = _terraform_docs_json(executable, directory)
    variable_behaviors = _variable_behaviors(directory)
    output_behaviors = _output_behaviors(directory)
    inputs: dict[str, Any] = {}
    for item in document.get("inputs", []):
        name = item["name"]
        inputs[name] = {
            "default": item.get("default"),
            "ephemeral": variable_behaviors.get(name, {}).get("ephemeral", False),
            "nullable": variable_behaviors.get(name, {}).get("nullable", True),
            "required": bool(item.get("required")),
            "sensitive": variable_behaviors.get(name, {}).get("sensitive", False),
            "type": _normalize_type(item.get("type", "")),
            "validation_fingerprints": variable_behaviors.get(name, {}).get(
                "validation_fingerprints", []
            ),
        }
    outputs = {item["name"]: output_behaviors[item["name"]] for item in document.get("outputs", [])}
    requirements = {
        item["name"]: " ".join(str(item.get("version", "")).split())
        for item in document.get("requirements", [])
    }
    provider_contracts = _provider_contracts(directory, set(requirements))
    calls = {
        f"module.{item['name']}": {
            "source": item.get("source", ""),
            "version": item.get("version"),
        }
        for item in document.get("modules", [])
    }
    return {
        "inputs": dict(sorted(inputs.items())),
        "managed_resource_addresses": _managed_addresses(directory),
        "module_calls": dict(sorted(calls.items())),
        "moved": _moved_mappings(directory),
        "outputs": dict(sorted(outputs.items())),
        "path": directory.relative_to(repo).as_posix(),
        "provider_contracts": provider_contracts,
        "requirements": dict(sorted(requirements.items())),
    }


def build_manifest(
    repo: Path,
    executable: str,
    *,
    contract_version: str,
    status: str,
    source_revision: str,
) -> dict[str, Any]:
    modules = {
        name: _module_interface(repo, directory, executable)
        for name, directory in terraform_directories(repo)
    }
    if not modules:
        raise GovernanceError(f"no Terraform modules found below {repo}")
    return {
        "contract_version": contract_version,
        "generated_by": {"terraform-docs": _terraform_docs_version(executable)},
        "modules": dict(sorted(modules.items())),
        "schema_version": MANIFEST_SCHEMA_VERSION,
        "source_revision": source_revision,
        "status": status,
    }


def _ensure_markers(text: str) -> str:
    begin_count, end_count = text.count(BEGIN_MARKER), text.count(END_MARKER)
    if begin_count == end_count == 0:
        return text.rstrip() + f"\n\n{BEGIN_MARKER}\n{END_MARKER}\n"
    if begin_count != 1 or end_count != 1 or text.index(BEGIN_MARKER) > text.index(END_MARKER):
        raise GovernanceError("README must contain exactly one ordered Terraform docs marker pair")
    return text


def _render_readme(repo: Path, directory: Path, executable: str) -> str:
    readme = directory / "README.md"
    if not readme.is_file():
        raise GovernanceError(f"missing README: {readme}")
    with tempfile.TemporaryDirectory(prefix="mindclade-tf-docs-") as raw_temp:
        temp = Path(raw_temp)
        for source in sorted(directory.iterdir()):
            if source.is_file() and (
                source.suffix == ".tf" or source.name == ".terraform.lock.hcl"
            ):
                shutil.copy2(source, temp / source.name)
        (temp / "README.md").write_text(
            _ensure_markers(readme.read_text(encoding="utf-8")), encoding="utf-8"
        )
        completed = _run(
            [
                executable,
                "--config",
                str(repo / CONFIG_RELATIVE),
                str(temp),
            ]
        )
        if completed.returncode != 0:
            raise GovernanceError(
                f"terraform-docs README generation failed for {directory}: "
                f"{completed.stderr.strip()}"
            )
        return (temp / "README.md").read_text(encoding="utf-8")


def generate(repo: Path, executable: str) -> None:
    policy = _policy(repo)
    _enforce_tool_version(repo, executable)
    for _, directory in terraform_directories(repo):
        expected = _render_readme(repo, directory, executable)
        (directory / "README.md").write_text(expected, encoding="utf-8")
    manifest = build_manifest(
        repo,
        executable,
        contract_version=str(policy["contract_version"]),
        status=str(policy["status"]),
        source_revision="working-tree",
    )
    (repo / MANIFEST_RELATIVE).write_text(_json_text(manifest), encoding="utf-8")
    print(f"generated docs and interface manifest for {len(manifest['modules'])} Terraform units")


def _version_tuple(value: str) -> tuple[int, ...]:
    match = re.match(r"^\s*v?(\d+(?:\.\d+)*)", value)
    if not match:
        raise ValueError(value)
    return tuple(int(part) for part in match.group(1).split("."))


def _constraint_bounds(
    value: str,
) -> tuple[tuple[tuple[int, ...], bool] | None, tuple[tuple[int, ...], bool] | None]:
    lower = None
    upper = None
    for raw in value.split(","):
        part = raw.strip()
        match = re.fullmatch(r"(>=|>|<=|<|=|~>)?\s*v?(\d+(?:\.\d+)*)", part)
        if not match:
            raise ValueError(value)
        operator = match.group(1) or "="
        version = _version_tuple(match.group(2))
        if operator in {">=", ">"}:
            candidate = (version, operator == ">=")
            if (
                lower is None
                or candidate[0] > lower[0]
                or (candidate[0] == lower[0] and not candidate[1])
            ):
                lower = candidate
        elif operator in {"<=", "<"}:
            candidate = (version, operator == "<=")
            if (
                upper is None
                or candidate[0] < upper[0]
                or (candidate[0] == upper[0] and not candidate[1])
            ):
                upper = candidate
        elif operator == "=":
            lower = (version, True)
            upper = (version, True)
        else:  # Terraform's pessimistic constraint.
            lower = (version, True)
            parts = list(version)
            bump_index = 0 if len(parts) == 1 else len(parts) - 2
            parts[bump_index] += 1
            for index in range(bump_index + 1, len(parts)):
                parts[index] = 0
            upper = (tuple(parts), False)
    return lower, upper


def _constraint_tightened(old: str, new: str) -> bool:
    if old == new:
        return False
    try:
        old_lower, old_upper = _constraint_bounds(old)
        new_lower, new_upper = _constraint_bounds(new)
    except ValueError:
        return True
    lower_tighter = new_lower is not None and (
        old_lower is None
        or new_lower[0] > old_lower[0]
        or (new_lower[0] == old_lower[0] and old_lower[1] and not new_lower[1])
    )
    upper_tighter = new_upper is not None and (
        old_upper is None
        or new_upper[0] < old_upper[0]
        or (new_upper[0] == old_upper[0] and old_upper[1] and not new_upper[1])
    )
    return lower_tighter or upper_tighter


def _change(
    module: str, category: str, subject: str, action: str, *, breaking: bool, detail: str
) -> dict[str, Any]:
    return {
        "action": action,
        "breaking": breaking,
        "category": category,
        "detail": detail,
        "id": f"module:{module}:{category}:{subject}:{action}",
        "module": module,
        "subject": subject,
    }


def classify_interfaces(baseline: dict[str, Any], current: dict[str, Any]) -> list[dict[str, Any]]:
    changes: list[dict[str, Any]] = []
    old_modules = baseline.get("modules", {})
    new_modules = current.get("modules", {})
    for module in sorted(set(old_modules) | set(new_modules)):
        if module not in new_modules:
            changes.append(
                _change(module, "module", module, "removed", breaking=True, detail="module removed")
            )
            removed = old_modules[module]
            for category, key in (
                ("resource", "managed_resource_addresses"),
                ("module-call", "module_calls"),
            ):
                for address in sorted(removed.get(key, {})):
                    changes.append(
                        _change(
                            module,
                            category,
                            address,
                            "removed",
                            breaking=True,
                            detail=f"{category} address removed with module",
                        )
                    )
            for mapping in removed.get("moved", []):
                changes.append(
                    _change(
                        module,
                        "moved",
                        mapping["from"],
                        "removed",
                        breaking=True,
                        detail="native moved mapping removed with module",
                    )
                )
            continue
        if module not in old_modules:
            changes.append(
                _change(module, "module", module, "added", breaking=False, detail="module added")
            )
            continue
        old, new = old_modules[module], new_modules[module]
        old_inputs, new_inputs = old.get("inputs", {}), new.get("inputs", {})
        for name in sorted(set(old_inputs) | set(new_inputs)):
            if name not in new_inputs:
                changes.append(
                    _change(module, "input", name, "removed", breaking=True, detail="input removed")
                )
                continue
            if name not in old_inputs:
                required = bool(new_inputs[name].get("required"))
                changes.append(
                    _change(
                        module,
                        "input",
                        name,
                        "added-required" if required else "added-optional",
                        breaking=required,
                        detail="new required input" if required else "new optional input",
                    )
                )
                continue
            for field in (
                "type",
                "default",
                "sensitive",
                "nullable",
                "ephemeral",
                "validation_fingerprints",
            ):
                if old_inputs[name].get(field) != new_inputs[name].get(field):
                    changes.append(
                        _change(
                            module,
                            "input",
                            name,
                            f"{field}-changed",
                            breaking=True,
                            detail=f"input {field} changed",
                        )
                    )
            if not old_inputs[name].get("required") and new_inputs[name].get("required"):
                changes.append(
                    _change(
                        module,
                        "input",
                        name,
                        "requiredness-tightened",
                        breaking=True,
                        detail="optional input became required",
                    )
                )
        old_outputs, new_outputs = old.get("outputs", {}), new.get("outputs", {})
        for name in sorted(set(old_outputs) | set(new_outputs)):
            if name not in new_outputs:
                changes.append(
                    _change(
                        module, "output", name, "removed", breaking=True, detail="output removed"
                    )
                )
            elif name not in old_outputs:
                changes.append(
                    _change(module, "output", name, "added", breaking=False, detail="output added")
                )
            else:
                for field in ("sensitive", "ephemeral", "value_fingerprint"):
                    if old_outputs[name].get(field) != new_outputs[name].get(field):
                        changes.append(
                            _change(
                                module,
                                "output",
                                name,
                                f"{field}-changed",
                                breaking=True,
                                detail=f"output {field} changed",
                            )
                        )
        old_requirements, new_requirements = (
            old.get("requirements", {}),
            new.get("requirements", {}),
        )
        for name in sorted(set(old_requirements) | set(new_requirements)):
            if name not in new_requirements:
                changes.append(
                    _change(
                        module,
                        "requirement",
                        name,
                        "removed",
                        breaking=False,
                        detail="requirement removed",
                    )
                )
            elif name not in old_requirements:
                changes.append(
                    _change(
                        module,
                        "requirement",
                        name,
                        "added",
                        breaking=True,
                        detail="requirement added",
                    )
                )
            elif old_requirements[name] != new_requirements[name]:
                tightened = _constraint_tightened(old_requirements[name], new_requirements[name])
                changes.append(
                    _change(
                        module,
                        "requirement",
                        name,
                        "tightened" if tightened else "loosened",
                        breaking=tightened,
                        detail="version requirement tightened"
                        if tightened
                        else "version requirement loosened",
                    )
                )
        old_providers = old.get("provider_contracts", {})
        new_providers = new.get("provider_contracts", {})
        for name in sorted(set(old_providers) | set(new_providers)):
            if name not in new_providers:
                changes.append(
                    _change(
                        module,
                        "provider-contract",
                        name,
                        "removed",
                        breaking=True,
                        detail="required provider contract removed",
                    )
                )
            elif name not in old_providers:
                changes.append(
                    _change(
                        module,
                        "provider-contract",
                        name,
                        "added",
                        breaking=True,
                        detail="required provider contract added",
                    )
                )
            else:
                for field in ("source", "configuration_aliases"):
                    if old_providers[name].get(field) != new_providers[name].get(field):
                        changes.append(
                            _change(
                                module,
                                "provider-contract",
                                name,
                                f"{field}-changed",
                                breaking=True,
                                detail=f"required provider {field} changed",
                            )
                        )
        for category, key in (
            ("resource", "managed_resource_addresses"),
            ("module-call", "module_calls"),
        ):
            old_values = old.get(key, {})
            new_values = new.get(key, {})
            old_addresses = set(old_values)
            new_addresses = set(new_values)
            for address in sorted(old_addresses - new_addresses):
                changes.append(
                    _change(
                        module,
                        category,
                        address,
                        "removed",
                        breaking=True,
                        detail=f"{category} address removed",
                    )
                )
            for address in sorted(new_addresses - old_addresses):
                changes.append(
                    _change(
                        module,
                        category,
                        address,
                        "added",
                        breaking=False,
                        detail=f"{category} address added",
                    )
                )
            if category == "module-call":
                for address in sorted(old_addresses & new_addresses):
                    if old_values[address] != new_values[address]:
                        changes.append(
                            _change(
                                module,
                                category,
                                address,
                                "source-changed",
                                breaking=True,
                                detail="child-module source or version changed",
                            )
                        )
        old_moved = {item["from"]: item["to"] for item in old.get("moved", [])}
        new_moved = {item["from"]: item["to"] for item in new.get("moved", [])}
        for source in sorted(set(old_moved) | set(new_moved)):
            if source not in new_moved:
                changes.append(
                    _change(
                        module,
                        "moved",
                        source,
                        "removed",
                        breaking=True,
                        detail="native moved mapping removed",
                    )
                )
            elif source not in old_moved:
                changes.append(
                    _change(
                        module,
                        "moved",
                        source,
                        "added",
                        breaking=False,
                        detail="native moved mapping added",
                    )
                )
            elif old_moved[source] != new_moved[source]:
                changes.append(
                    _change(
                        module,
                        "moved",
                        source,
                        "target-changed",
                        breaking=True,
                        detail="native moved mapping target changed",
                    )
                )
    return sorted(changes, key=lambda item: item["id"])


def _validate_version_change(
    baseline: dict[str, Any], current: dict[str, Any], changes: list[dict[str, Any]]
) -> list[str]:
    errors: list[str] = []
    old_text, new_text = str(baseline["contract_version"]), str(current["contract_version"])
    old, new = _parse_semver(old_text), _parse_semver(new_text)
    if not changes:
        if old != new:
            errors.append(
                f"interface version changed without an API change: {old_text} -> {new_text}"
            )
        return errors
    if new <= old:
        errors.append(f"interface changes require a version increment: {old_text} -> {new_text}")
        return errors
    if any(change["breaking"] for change in changes):
        if old[0] == 0:
            if new[0] == 0 and new[1] <= old[1]:
                errors.append("pre-1.0 breaking changes require at least a minor-version increment")
        elif new[0] <= old[0]:
            errors.append("breaking changes after 1.0 require a major-version increment")
    return errors


def _migration_records(repo: Path) -> list[tuple[Path, dict[str, Any]]]:
    result = []
    directory = repo / MIGRATIONS_RELATIVE
    for path in sorted(directory.glob("*.toml")):
        try:
            value = tomllib.loads(path.read_text(encoding="utf-8"))
        except (OSError, tomllib.TOMLDecodeError) as exc:
            raise GovernanceError(f"cannot read migration record {path}: {exc}") from exc
        result.append((path, value))
    return result


def _validate_migrations(
    repo: Path,
    baseline: dict[str, Any],
    current: dict[str, Any],
    changes: list[dict[str, Any]],
) -> list[str]:
    breaking = {change["id"]: change for change in changes if change["breaking"]}
    if not breaking:
        return []
    old_version = str(baseline["contract_version"])
    new_version = str(current["contract_version"])
    records = [
        (path, value)
        for path, value in _migration_records(repo)
        if value.get("from_version") == old_version and value.get("to_version") == new_version
    ]
    if not records:
        return [
            f"no migration record covers breaking interface change {old_version} -> {new_version}"
        ]
    errors: list[str] = []
    covered: set[str] = set()
    removed_addresses = {
        (change["module"], change["subject"]): change
        for change in changes
        if change["category"] in {"resource", "module-call"} and change["action"] == "removed"
    }
    dispositions: dict[tuple[str, str], list[str]] = {}
    for path, record in records:
        label = path.relative_to(repo).as_posix()
        if record.get("schema_version") != MIGRATION_SCHEMA_VERSION:
            errors.append(f"{label}: schema_version must be {MIGRATION_SCHEMA_VERSION}")
        if record.get("status") not in {"planned", "released"}:
            errors.append(f"{label}: status must be planned or released")
        for field in ("owner", "summary"):
            if not isinstance(record.get(field), str) or not record[field].strip():
                errors.append(f"{label}: {field} must be a non-empty string")
        for field in ("consumer_steps", "rollback_steps", "qualification_evidence"):
            values = record.get(field)
            if (
                not isinstance(values, list)
                or not values
                or not all(isinstance(item, str) and item.strip() for item in values)
            ):
                errors.append(f"{label}: {field} must be a non-empty string array")
        for evidence in record.get("qualification_evidence", []):
            if isinstance(evidence, str) and not (repo / evidence).exists():
                errors.append(f"{label}: qualification evidence path does not exist: {evidence}")
        identifiers = record.get("breaking_change_ids")
        if not isinstance(identifiers, list) or identifiers != sorted(set(identifiers)):
            errors.append(f"{label}: breaking_change_ids must be a sorted unique array")
            identifiers = []
        identifier_set = set(identifiers)
        unknown = set(identifiers) - set(breaking)
        if unknown:
            errors.append(f"{label}: unknown breaking_change_ids: {sorted(unknown)}")
        overlap = covered & set(identifiers)
        if overlap:
            errors.append(
                f"{label}: breaking_change_ids covered by multiple records: {sorted(overlap)}"
            )
        covered.update(identifiers)
        expected_modules = sorted(
            {breaking[item]["module"] for item in identifiers if item in breaking}
        )
        if record.get("affected_modules") != expected_modules:
            errors.append(f"{label}: affected_modules must equal {expected_modules}")
        moves = record.get("state_move", [])
        if not isinstance(moves, list):
            errors.append(f"{label}: state_move must be an array of tables")
            moves = []
        for move in moves:
            if not isinstance(move, dict) or not all(
                isinstance(move.get(field), str) and move[field]
                for field in ("module", "from", "to")
            ):
                errors.append(f"{label}: every state_move needs module, from, and to")
                continue
            module = move["module"]
            old = baseline.get("modules", {}).get(module, {})
            new = current.get("modules", {}).get(module, {})
            old_addresses = set(old.get("managed_resource_addresses", [])) | set(
                old.get("module_calls", {})
            )
            new_addresses = set(new.get("managed_resource_addresses", [])) | set(
                new.get("module_calls", {})
            )
            disposition_key = (module, move["from"])
            removed = removed_addresses.get(disposition_key)
            if removed is None:
                errors.append(
                    f"{label}: state_move source {module} {move['from']} is not a removed public address"
                )
            else:
                dispositions.setdefault(disposition_key, []).append(f"{label}:state_move")
                if removed["id"] not in identifier_set:
                    errors.append(
                        f"{label}: state_move for {module} {move['from']} must cover {removed['id']}"
                    )
            if move["from"] not in old_addresses or move["to"] not in new_addresses:
                errors.append(
                    f"{label}: state_move {module} {move['from']} -> {move['to']} is not an interface address move"
                )
            native = {(item["from"], item["to"]) for item in new.get("moved", [])}
            if (move["from"], move["to"]) not in native:
                errors.append(
                    f"{label}: state_move {module} {move['from']} -> {move['to']} lacks a native moved block"
                )
        removals = record.get("intentional_removal", [])
        if not isinstance(removals, list):
            errors.append(f"{label}: intentional_removal must be an array of tables")
            removals = []
        for removal in removals:
            required_fields = (
                "module",
                "address",
                "reason",
                "consumer_action",
                "rollback_action",
            )
            if not isinstance(removal, dict) or not all(
                isinstance(removal.get(field), str) and removal[field].strip()
                for field in required_fields
            ):
                errors.append(
                    f"{label}: every intentional_removal needs module, address, reason, "
                    "consumer_action, and rollback_action"
                )
                continue
            disposition_key = (removal["module"], removal["address"])
            removed = removed_addresses.get(disposition_key)
            if removed is None:
                errors.append(
                    f"{label}: intentional_removal {removal['module']} {removal['address']} "
                    "is not a removed public address"
                )
                continue
            dispositions.setdefault(disposition_key, []).append(f"{label}:intentional_removal")
            if removed["id"] not in identifier_set:
                errors.append(
                    f"{label}: intentional_removal for {removal['module']} "
                    f"{removal['address']} must cover {removed['id']}"
                )
            new = current.get("modules", {}).get(removal["module"], {})
            native_sources = {item["from"] for item in new.get("moved", [])}
            if removal["address"] in native_sources:
                errors.append(
                    f"{label}: intentional_removal {removal['module']} {removal['address']} "
                    "conflicts with a native moved block"
                )
    missing = set(breaking) - covered
    if missing:
        errors.append(f"migration records do not cover breaking changes: {sorted(missing)}")
    for key, entries in sorted(dispositions.items()):
        if len(entries) > 1:
            errors.append(f"removed address {key[0]} {key[1]} has multiple dispositions: {entries}")
    missing_dispositions = set(removed_addresses) - set(dispositions)
    if missing_dispositions:
        errors.append(
            "removed public addresses lack state_move or intentional_removal disposition: "
            f"{[f'{module} {address}' for module, address in sorted(missing_dispositions)]}"
        )
    return errors


def _verify_manifest_shape(value: dict[str, Any], label: str) -> None:
    if value.get("schema_version") != MANIFEST_SCHEMA_VERSION:
        raise GovernanceError(
            f"{label} schema_version must be {MANIFEST_SCHEMA_VERSION}, "
            f"found {value.get('schema_version')!r}"
        )
    _parse_semver(str(value.get("contract_version", "")), f"{label} contract_version")
    if not isinstance(value.get("modules"), dict):
        raise GovernanceError(f"{label} modules must be an object")


def _resolve_commit(repo: Path, ref: str, label: str) -> str:
    completed = _run(
        ["git", "rev-parse", "--verify", "--end-of-options", f"{ref}^{{commit}}"], cwd=repo
    )
    if completed.returncode != 0 or not re.fullmatch(r"[0-9a-f]{40}", completed.stdout.strip()):
        raise GovernanceError(
            f"{label} {ref!r} is invalid or unfetched; fetch the exact base commit before checking"
        )
    return completed.stdout.strip()


def _rebuild_baseline_manifest(
    repo: Path, policy: dict[str, Any], executable: str
) -> dict[str, Any]:
    revision = str(policy["baseline_commit"])
    archived = _run_bytes(
        [
            "git",
            "archive",
            "--format=tar",
            revision,
            "--",
            "infra/terraform/modules",
        ],
        cwd=repo,
    )
    if archived.returncode != 0:
        stderr = archived.stderr.decode("utf-8", errors="replace").strip()
        raise GovernanceError(f"cannot archive baseline Terraform at {revision}: {stderr}")
    with tempfile.TemporaryDirectory(prefix="mindclade-tf-baseline-") as raw_temp:
        source_root = Path(raw_temp)
        try:
            with tarfile.open(fileobj=io.BytesIO(archived.stdout), mode="r:") as archive:
                archive.extractall(source_root, filter="data")
        except (tarfile.TarError, OSError) as exc:
            raise GovernanceError(f"cannot extract baseline Terraform archive: {exc}") from exc
        return build_manifest(
            source_root,
            executable,
            contract_version=str(policy["baseline_version"]),
            status="released",
            source_revision=revision,
        )


def _verified_fallback_manifest(
    repo: Path, policy: dict[str, Any], executable: str
) -> dict[str, Any]:
    baseline = _read_json(repo / BASELINE_RELATIVE)
    _verify_manifest_shape(baseline, "fallback baseline")
    expected_version = str(policy["baseline_version"])
    expected_revision = str(policy["baseline_commit"])
    if baseline.get("contract_version") != expected_version:
        raise GovernanceError(
            f"fallback baseline version differs from version.toml: "
            f"{baseline.get('contract_version')!r} != {expected_version!r}"
        )
    if baseline.get("source_revision") != expected_revision:
        raise GovernanceError(
            f"fallback baseline revision differs from version.toml: "
            f"{baseline.get('source_revision')!r} != {expected_revision!r}"
        )
    resolved = _resolve_commit(repo, expected_revision, "fallback baseline commit")
    if resolved != expected_revision:
        raise GovernanceError(
            f"fallback baseline commit resolved to {resolved}, expected {expected_revision}"
        )
    rebuilt = _rebuild_baseline_manifest(repo, policy, executable)
    if baseline != rebuilt:
        raise GovernanceError(
            "fallback baseline content does not match a deterministic rebuild from "
            f"{expected_revision}; regenerate baselines/v{expected_version}.json from that commit"
        )
    return baseline


def _base_manifest(
    repo: Path, base_ref: str | None, policy: dict[str, Any], executable: str
) -> dict[str, Any]:
    if not base_ref:
        return _verified_fallback_manifest(repo, policy, executable)
    commit = _resolve_commit(repo, base_ref, "base ref")
    path = MANIFEST_RELATIVE.as_posix()
    listing = _run(["git", "ls-tree", "-r", "--name-only", commit, "--", path], cwd=repo)
    if listing.returncode != 0:
        raise GovernanceError(
            f"cannot inspect interface manifest at verified base commit {commit}: "
            f"{listing.stderr.strip()}"
        )
    if not listing.stdout.strip():
        return _verified_fallback_manifest(repo, policy, executable)
    completed = _run(["git", "show", f"{commit}:{path}"], cwd=repo)
    if completed.returncode != 0:
        raise GovernanceError(
            f"cannot read interface manifest at verified base commit {commit}: "
            f"{completed.stderr.strip()}"
        )
    try:
        value = json.loads(completed.stdout)
    except json.JSONDecodeError as exc:
        raise GovernanceError(f"base-ref interface manifest is invalid JSON: {exc}") from exc
    if not isinstance(value, dict):
        raise GovernanceError("base-ref interface manifest is not an object")
    _verify_manifest_shape(value, "base-ref interface manifest")
    return value


def check(repo: Path, executable: str, base_ref: str | None) -> None:
    policy = _policy(repo)
    _enforce_tool_version(repo, executable)
    errors: list[str] = []
    for _, directory in terraform_directories(repo):
        actual = (directory / "README.md").read_text(encoding="utf-8")
        expected = _render_readme(repo, directory, executable)
        if actual != expected:
            errors.append(
                f"generated Terraform docs drift: {directory.relative_to(repo)}/README.md "
                "(run infra/terraform/governance/generate.sh)"
            )
    expected_manifest = build_manifest(
        repo,
        executable,
        contract_version=str(policy["contract_version"]),
        status=str(policy["status"]),
        source_revision="working-tree",
    )
    committed_manifest = _read_json(repo / MANIFEST_RELATIVE)
    if committed_manifest != expected_manifest:
        errors.append(
            "Terraform interface manifest drift: infra/terraform/governance/module-interfaces.json "
            "(run infra/terraform/governance/generate.sh)"
        )
    baseline = _base_manifest(repo, base_ref, policy, executable)
    changes = classify_interfaces(baseline, expected_manifest)
    errors.extend(_validate_version_change(baseline, expected_manifest, changes))
    errors.extend(_validate_migrations(repo, baseline, expected_manifest, changes))
    if errors:
        raise GovernanceError("\n".join(errors))
    breaking_count = sum(1 for change in changes if change["breaking"])
    print(
        f"Terraform interface governance passed: {len(expected_manifest['modules'])} units, "
        f"{len(changes)} changes from {baseline['contract_version']} "
        f"({breaking_count} breaking, all recorded)"
    )


def _declarations(directory: Path, kind: str) -> set[str]:
    values: set[str] = set()
    for _, first, second, _ in _blocks(directory, {kind}):
        if kind == "resource" and first and second:
            values.add(f"{first}.{second}")
        elif kind == "module" and first:
            values.add(f"module.{first}")
        elif first:
            values.add(first)
    return values


def verify_scope(repo: Path, scope: str) -> list[str]:
    """Hermetic Bazel-side structural verification without invoking terraform-docs."""
    manifest = _read_json(repo / MANIFEST_RELATIVE)
    units = manifest.get("modules", {})
    expected = {
        name: directory
        for name, directory in terraform_directories(repo)
        if (scope == "modules" and name != "dns_hub") or (scope == "dns_hub" and name == "dns_hub")
    }
    actual = {
        name
        for name, item in units.items()
        if item.get("path", "").startswith(
            "infra/terraform/modules/"
            if scope == "modules"
            else "infra/terraform/environments/dns_hub"
        )
    }
    errors: list[str] = []
    if set(expected) != actual:
        errors.append(
            f"{scope}: manifest units differ: expected {sorted(expected)}, found {sorted(actual)}"
        )
    for name, directory in expected.items():
        item = units.get(name)
        if not isinstance(item, dict):
            continue
        readme = directory / "README.md"
        if not readme.is_file():
            errors.append(f"{name}: README missing")
        else:
            text = readme.read_text(encoding="utf-8")
            if text.count(BEGIN_MARKER) != 1 or text.count(END_MARKER) != 1:
                errors.append(f"{name}: README has invalid generated-doc markers")
        checks = (
            ("input", _declarations(directory, "variable"), set(item.get("inputs", {}))),
            ("output", _declarations(directory, "output"), set(item.get("outputs", {}))),
            (
                "resource",
                _declarations(directory, "resource"),
                set(item.get("managed_resource_addresses", [])),
            ),
            ("module-call", _declarations(directory, "module"), set(item.get("module_calls", {}))),
        )
        for label, source, recorded in checks:
            if source != recorded:
                errors.append(f"{name}: {label} declarations differ from manifest")
        variable_behaviors = _variable_behaviors(directory)
        recorded_variable_behaviors = {
            input_name: {
                field: definition.get(field)
                for field in (
                    "ephemeral",
                    "nullable",
                    "sensitive",
                    "validation_fingerprints",
                )
            }
            for input_name, definition in item.get("inputs", {}).items()
        }
        if variable_behaviors != recorded_variable_behaviors:
            errors.append(f"{name}: variable behavior fingerprints differ from manifest")
        if _output_behaviors(directory) != item.get("outputs", {}):
            errors.append(f"{name}: output behavior fingerprints differ from manifest")
        provider_contracts = _provider_contracts(directory, set(item.get("requirements", {})))
        if provider_contracts != item.get("provider_contracts", {}):
            errors.append(f"{name}: provider source/alias contracts differ from manifest")
        if _moved_mappings(directory) != item.get("moved", []):
            errors.append(f"{name}: native moved mappings differ from manifest")
    return errors


def _parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description=__doc__)
    subparsers = parser.add_subparsers(dest="command", required=True)
    for name in ("generate", "check"):
        command = subparsers.add_parser(name)
        command.add_argument("--repo", type=Path, required=True)
        command.add_argument(
            "--terraform-docs", default=os.environ.get("TERRAFORM_DOCS", "terraform-docs")
        )
        if name == "check":
            command.add_argument("--base-ref")
    snapshot = subparsers.add_parser("snapshot")
    snapshot.add_argument("--repo", type=Path, required=True)
    snapshot.add_argument(
        "--terraform-docs", default=os.environ.get("TERRAFORM_DOCS", "terraform-docs")
    )
    snapshot.add_argument("--contract-version", required=True)
    snapshot.add_argument("--status", choices=("planned", "released"), required=True)
    snapshot.add_argument("--source-revision", required=True)
    snapshot.add_argument("--output", type=Path, required=True)
    verify = subparsers.add_parser("verify-scope")
    verify.add_argument("--repo", type=Path, required=True)
    verify.add_argument("--scope", choices=("modules", "dns_hub"), required=True)
    return parser


def main() -> int:
    arguments = _parser().parse_args()
    try:
        repo = arguments.repo.resolve()
        if arguments.command == "generate":
            generate(repo, arguments.terraform_docs)
        elif arguments.command == "check":
            check(repo, arguments.terraform_docs, arguments.base_ref)
        elif arguments.command == "snapshot":
            manifest = build_manifest(
                repo,
                arguments.terraform_docs,
                contract_version=arguments.contract_version,
                status=arguments.status,
                source_revision=arguments.source_revision,
            )
            arguments.output.write_text(_json_text(manifest), encoding="utf-8")
        else:
            errors = verify_scope(repo, arguments.scope)
            if errors:
                raise GovernanceError("\n".join(errors))
            print(f"Terraform {arguments.scope} interface structure passed")
    except GovernanceError as exc:
        print(f"terraform-interface: {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
