#!/usr/bin/env python3
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

from __future__ import annotations

import ast
import contextlib
import hashlib
import io
import json
import shlex
import sys
from pathlib import Path
from typing import Any
from unittest import mock

ROOT = Path(__file__).resolve().parents[2]
if str(ROOT) not in sys.path:
    sys.path.insert(0, str(ROOT))

from workflow_yaml import WorkflowYamlError, parse_workflow  # noqa: E402

from ci.common import affected  # noqa: E402
from ci.common.affected_contract import (  # noqa: E402
    ContractError,
    GlobalInputContract,
    load_global_input_contract,
    load_global_input_payload,
)
from ci.nightly import pipeline as nightly_pipeline  # noqa: E402
from ci.presubmit import pipeline as presubmit_pipeline  # noqa: E402

PRESUBMIT_EVENTS = frozenset({"merge_group", "pull_request", "push"})
NIGHTLY_EVENTS = frozenset({"schedule", "workflow_dispatch"})
PULL_REQUEST_CACHE_BASE_REF = (
    "${{ github.event.pull_request.stack.base.ref || github.event.pull_request.base.ref }}"
)
PULL_REQUEST_CACHE_BASE_SHA = (
    "${{ github.event.pull_request.stack.base.sha || github.event.pull_request.base.sha }}"
)
PULL_REQUEST_SELECTION_BASE_SHA = "${{ github.event.pull_request.base.sha }}"
REMOTE_CACHE_ENABLED_EXPRESSION = (
    "${{ steps.bazel-remote-cache.outcome == 'success' "
    "&& steps.bazel-remote-cache.outputs.enabled || 'false' }}"
)
PERSISTENT_CACHE_TRUST_IF = "steps.bazel-remote-cache.outputs.enabled != 'true'"
PERSISTENT_CACHE_RESTORE_IF = (
    "steps.bazel-remote-cache.outputs.enabled != 'true' "
    "&& steps.bazel-cache-trust.outcome == 'success'"
)
PERSISTENT_CACHE_ROLE_EXPRESSION = (
    "${{ steps.bazel-cache-trust.outcome == 'success' "
    "&& steps.bazel-cache-trust.outputs.role || 'reader' }}"
)
GOVERNED_CACHE_ROLE_EXPRESSION = (
    "${{ steps.bazel-remote-cache.outputs.enabled == 'true' "
    "&& steps.bazel-remote-cache.outputs.role "
    "|| steps.bazel-cache-trust.outcome == 'success' "
    "&& steps.bazel-cache-trust.outputs.role || 'reader' }}"
)
PERSISTENT_CACHE_MEASURE_IF = (
    "always() && steps.bazel-remote-cache.outputs.enabled != 'true' "
    "&& steps.bazel-cache-trust.outcome == 'success'"
)
PRESUBMIT_BAZEL_COMMAND = (
    "/nix/var/nix/profiles/default/bin/nix",
    "develop",
    ".#ci-bazel",
    "--command",
    "python3",
    "-B",
    "-I",
    "ci/presubmit/pipeline.py",
    "--bazel-only",
    "--mode",
    "auto",
    "--base",
    "${PR_BASE_SHA}",
    "--event",
    "${GITHUB_EVENT_NAME}",
    "--ref",
    "${GITHUB_REF}",
    "--head",
    "${GITHUB_SHA}",
    "--evidence-dir",
    "${RUNNER_TEMP}/bazel-evidence",
    "--job-started-at-file",
    "${RUNNER_TEMP}/bazel-job-started",
    "--runner-temp",
    "${RUNNER_TEMP}",
    "--cache-mode",
    "${BAZEL_CACHE_MODE}",
    "--cache-role",
    "${BAZEL_CACHE_ROLE}",
)
NIGHTLY_BAZEL_COMMAND = (
    "/nix/var/nix/profiles/default/bin/nix",
    "develop",
    ".#ci-bazel",
    "--command",
    "python3",
    "-B",
    "-I",
    "ci/nightly/pipeline.py",
    "--event",
    "${GITHUB_EVENT_NAME}",
    "--ref",
    "${GITHUB_REF}",
    "--head",
    "${GITHUB_SHA}",
    "--evidence-dir",
    "${RUNNER_TEMP}/bazel-evidence",
    "--job-started-at-file",
    "${RUNNER_TEMP}/bazel-job-started",
    "--runner-temp",
    "${RUNNER_TEMP}",
    "--cache-mode",
    "${BAZEL_CACHE_MODE}",
    "--cache-role",
    "${BAZEL_CACHE_ROLE}",
)
UPLOAD_ARTIFACT_ACTION = "actions/upload-artifact@ea165f8d65b6e75b540449e92b4886f43607fa02"
DOWNLOAD_ARTIFACT_ACTION = "actions/download-artifact@634f93cb2916e3fdff6788551b99b062d0335ce0"
CHECKOUT_ACTION = "actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1"
# fmt: off
PRESUBMIT_PLAN_STEP_CONTRACT = (
    ("uses:actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1", "69983d424f70d56e16aca2c5bc441464578ac832b548ce342069dd3e5a1ca9e1"),
    ("uses:actions/setup-python@ece7cb06caefa5fff74198d8649806c4678c61a1", "be44ea9b59f4d2b9ef17fc2526f2d1b8607bfcba533e0e8c81636aa6cbbb1f7d"),
    ("name:Select qualified Bazel remote-cache route for topology", "92b2e9a94eb29b8f3c62e7fa143a71e14fd4d0e03e9b08cd013d42d09e71582d"),
    ("name:Select presubmit, fallback, or complete shard workers", "15104f529af37a1778ca416ffc346a3f78ad6d502147dc56a50e5f1d7101466f"),
)

PRESUBMIT_BAZEL_STEP_CONTRACT = (
    ("name:Record Bazel worker start", "ebbea1fb183f31a06004e81796280e9eda032e3cedcdac4fb2e4aff15e5e800a"),
    ("uses:actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1", "3facce523aa8a0e29a937485282e18e07e2907c45914b4ce7745b674c765f3b7"),
    ("name:Prepare GitHub-hosted runner disk for Nix", "c8ce580064e9a2ca3c9dabcdac62b53fe14d8ce7b17fa00de32d1a1e97915267"),
    ("uses:bazel-contrib/setup-bazel@4fd964a13a440a8aeb0be47350db2fc640f19ca8", "5c825b313c6e81fad2e993ae09ce267a40dd48b30816e4e151764b96273d16f2"),
    ("uses:cachix/install-nix-action@630ae543ea3a38a9a4166f03376c02c50f408342", "889e811758a8eb0bfe298bec1412219d252c18e4946c086c793465172a6a4513"),
    ("name:Select qualified Bazel remote-cache route", "17f1e826fbd763e44b11c1d9b11e8f21b62c8fde215ec4e63810bc6ad168473c"),
    ("name:Verify worker topology still matches cache qualification", "3daf2e1f1fa43b9f8a6537ba80783c30081cfaca72d2a3d8e8923478422053fe"),
    ("name:Select trusted Bazel cache revision", "bea656ab62c9f9a001e3c1ad62af6470630387cdc39aa490946410a3d9d6ba8e"),
    ("name:Restore trusted Bazel persistent action cache", "130a9b6847621a68bb1b45ebbb5a6b7fdb98d479b758e215f1b9c586dff951ea"),
    ("name:Configure bounded Bazel persistent action cache", "09667757ca3924ef1858867e2bc4ec849de1d42f53437d7125c0d358c3721b6b"),
    ("name:Build qualified Bazel GCS cache gateway", "6f1f4320ac9b5bd8d49ac0784f852d711aea478def27cebd4104d687909e8096"),
    ("name:Authenticate read-only Bazel cache route", "0744449797a1f59706bfd676fb0b6b408d366731a605eee8127922ce210f5931"),
    ("name:Authenticate trusted Bazel cache writer route", "4f3a50497ca60210af3343e78880a5de32eae5b0921bf639674deb75f525548b"),
    ("name:Start loopback Bazel GCS cache gateway", "9c57f2444e9db26b2f8814163b2314a7bdc8c366b9c6711c9083e574254a26c4"),
    ("name:buildifier", "92af3aaebc20a2145f6756ad32330ea5174f55e46dc9a86ceb16c2234c41cecd"),
    ("name:Every BUILD file loads and every label resolves", "c39f7f4b83b46ac46627a640d9a4687e384ef1e8a7219c2401775d8fb1743bb2"),
    ("name:Prove affected selection against the real Bazel graph", "66567131eb51965014ce86328e7c3887e4ab8632092cbdd9f7fb5a50f4c75d1b"),
    ("name:Enforce Bazel dependency layers", "6085cb60c60baafa38e8ca9a205ab62e10d61f6eca03d3815a4574e1c93357bc"),
    ("name:Validate and resolve the registered C/C++ toolchain", "94adcdaa6f8e676dce149820b3b12a968bb11a093b16463e1c6c22b06125cfd9"),
    ("name:Remove ignored checkout byproducts", "799d86234a1bf27e77569c23ef52802e80e03dc89e4f2c2a47630fef2f0caeae"),
    ("name:Run event-governed Bazel validation", "1f87c93b03269d1e9bf50d6598d20bc406568e16ec02fa1a1b033e2da3014414"),
    ("name:Redact completed Bazel worker selection", "733419ff8de8d49f0a95b7eaf1865f9210769687667f717944876afe09f88850"),
    ("name:Measure bounded Bazel persistent action cache", "95b92bedabf6707c2764881bd036ab7fd73f3e26b4e6258de880830752acdfd5"),
    ("name:Save trusted Bazel persistent action cache", "dcbadf057eab254e9a46988c53ee666724ba6afc007b26e6a977efbf00e81127"),
    ("name:Record Bazel persistent action cache metrics", "9b4261daaa5be664481d8a50a5c9875daf0d72f466c70b9f1d9244c91f4b1875"),
    ("name:Record Bazel GCS remote-cache metrics and stop gateway", "48b22f3adde66695cd963bf0bb50b07f7f35e9beebef3562f6e330ba380abe63"),
    ("name:Upload Bazel performance evidence", "1e9b7293ed88d18c63aad10fbee4b3b44ce0cdbca5c43e031f2e3df58ca72f01"),
    ("name:Upload redacted Bazel health metrics", "7e26a049271357069db9313b7347a99a61730a9ec657402d7ccc57a6ce124384"),
    ("name:Upload redacted Bazel worker selection", "142b944b484fa14736495f4444875651ddadd286651de3df1e32a2f177ddc8e7"),
    ("name:Upload Bazel latency metric", "6a9fe809a0489b2829fe0d1f78fe9f4066b965132c5fa115af5909c23eda3c42"),
)

PRESUBMIT_VERDICT_STEP_CONTRACT = (
    ("uses:actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1", "69983d424f70d56e16aca2c5bc441464578ac832b548ce342069dd3e5a1ca9e1"),
    ("uses:actions/setup-python@ece7cb06caefa5fff74198d8649806c4678c61a1", "be44ea9b59f4d2b9ef17fc2526f2d1b8607bfcba533e0e8c81636aa6cbbb1f7d"),
    ("name:Download redacted Bazel worker selections", "8fc21eab2b1421a5b96400bcfd153794ba1afd6def1f5175a2190c0a42bb5d57"),
    ("name:Download redacted Bazel health metrics", "52a2d9b8546342c62236670763a3ed2db1b5fb6e6dd0b744eae8fb2551b6d829"),
    ("name:Generate the Bazel CI health dashboard", "38e0d50135f9546cf13839bc3e79bacfdbdfc57a39cdfb3804ee16ed29368780"),
    ("name:Upload the Bazel CI health dashboard", "8fda1f70474bc51466fa9d052fd9dd7dfbfb2268aa5b51dc32f725e5ffffe310"),
    ("name:Aggregate the complete Bazel verdict", "57e3e5c8c8699391f59817c0726e66f25068bce0353f71a259b41e3d45c3c8b7"),
)

NIGHTLY_PLAN_STEP_CONTRACT = (
    ("uses:actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1", "69983d424f70d56e16aca2c5bc441464578ac832b548ce342069dd3e5a1ca9e1"),
    ("uses:actions/setup-python@ece7cb06caefa5fff74198d8649806c4678c61a1", "be44ea9b59f4d2b9ef17fc2526f2d1b8607bfcba533e0e8c81636aa6cbbb1f7d"),
    ("name:Select qualified nightly Bazel remote-cache route for topology", "6e24f1b7afdb369a1c603faddb6a10c79417466e279f508c74bf9158b01f1082"),
    ("name:Select fallback or complete shard workers", "1a00cc24da3dd53e50e7ea7812e6a2d9145c57f4e2c70e5ac1687690caea199f"),
)

NIGHTLY_BAZEL_STEP_CONTRACT = (
    ("name:Record nightly Bazel worker start", "e1cf7d309e7de6ab674dcf7d4a60245045e0aa0a286020bba1922e57fea3aa92"),
    ("uses:actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1", "69983d424f70d56e16aca2c5bc441464578ac832b548ce342069dd3e5a1ca9e1"),
    ("name:Prepare GitHub-hosted runner disk for Nix", "c8ce580064e9a2ca3c9dabcdac62b53fe14d8ce7b17fa00de32d1a1e97915267"),
    ("uses:bazel-contrib/setup-bazel@4fd964a13a440a8aeb0be47350db2fc640f19ca8", "5c825b313c6e81fad2e993ae09ce267a40dd48b30816e4e151764b96273d16f2"),
    ("uses:cachix/install-nix-action@630ae543ea3a38a9a4166f03376c02c50f408342", "889e811758a8eb0bfe298bec1412219d252c18e4946c086c793465172a6a4513"),
    ("name:Select qualified nightly Bazel remote-cache route", "f946134ef78e00c5703c21fd217855b331e8d1c6a6ef166800cad24bbd95a21c"),
    ("name:Verify worker topology still matches cache qualification", "24917606b35637d9306c319ede0e349e32cd12c5a2bfc96393cbf28414b41ae8"),
    ("name:Select trusted nightly Bazel cache revision", "8a11170e45d5ba4e1ab10f13a6032f173057e060766aa8895f18b7f242514a7b"),
    ("name:Restore trusted nightly Bazel persistent action cache", "d2f8fb316c6fd8485dea77c8bd7c4f356376a70fcac19485c3ce23384e0341b0"),
    ("name:Configure bounded nightly Bazel persistent action cache", "c307e45367643c715bd84ce724a8364413b4f9367948e1f2925bf23b2103066c"),
    ("name:Build qualified nightly Bazel GCS cache gateway", "5d490c127bcbe8e961d15be535f78e809bcaf6fee8e64d4af32ed7cffe7c0ea0"),
    ("name:Authenticate scheduled Bazel cache writer route", "1e6a054411a5caed2fc04fb952c2cdf918c403f9c0871622f14f7c31a621bd81"),
    ("name:Start scheduled loopback Bazel GCS cache gateway", "5d87963363e71b5606287182b94caa97a9c4f41ff1a1db20066529df80a4e518"),
    ("name:Validate complete loading, formatting, and layer policy", "041657899e59b4e043e7ad2342e72e9458795c556c53ca5037bf66210b031f11"),
    ("name:Validate and resolve the registered C/C++ toolchain", "3fd02d1b463a91acccf44039cd942bf18d6cd3de0a3c9cd099f67464906fbc6d"),
    ("name:Remove ignored checkout byproducts", "799d86234a1bf27e77569c23ef52802e80e03dc89e4f2c2a47630fef2f0caeae"),
    ("name:Analyze and test the selected repository-graph workload", "4b83233850c325791937e173349d35a70429ca007d52b4dfbe5848e564185342"),
    ("name:Redact completed nightly Bazel worker selection", "9990a7fae2199d3f1841b8b5abbb2e861dbc568172032fe50d1226bddeaebb76"),
    ("name:Qualify the rolling affected-presubmit latency SLO", "9ab6f78df68c7169b3468352f541e1a9c6b08dacbad44778b2d1dc3e3af39e57"),
    ("name:Measure bounded nightly Bazel persistent action cache", "867a6253a4d0c3d2996066a6d842fae33252c4fdf4c4e8e2478d75ffd6ff9c17"),
    ("name:Save trusted nightly Bazel persistent action cache", "628d8743e71c0cdda2bde68e2f1c24f0f7cea9f6d60b9b11d8d464302ec0c2a3"),
    ("name:Record nightly Bazel persistent action cache metrics", "d7997e4788ffd0e339395d1d154a5dfaca8cab0559f0c26ac234ea54944c3298"),
    ("name:Record nightly Bazel GCS remote-cache metrics and stop gateway", "f6f3004b9f1f49492a6bb1d030bc0d0518145a09709e3f4a3824dd465edd5112"),
    ("name:Upload nightly Bazel evidence", "5338817a03fb411508e4d256a6e4ec0e2c719269c9dd0dbd127774ffee55c809"),
    ("name:Upload redacted nightly Bazel health metrics", "c43b26fde631a7cf48f85661f128a7ff6cd4ee346c0dba95e5d1036e11a1ee4c"),
    ("name:Upload redacted nightly Bazel worker selection", "a4d65697706ff3d752142ef0027ff2ee4a950938918bdec75e56366a0895358e"),
)

NIGHTLY_VERDICT_STEP_CONTRACT = (
    ("uses:actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1", "69983d424f70d56e16aca2c5bc441464578ac832b548ce342069dd3e5a1ca9e1"),
    ("uses:actions/setup-python@ece7cb06caefa5fff74198d8649806c4678c61a1", "be44ea9b59f4d2b9ef17fc2526f2d1b8607bfcba533e0e8c81636aa6cbbb1f7d"),
    ("name:Download redacted nightly Bazel worker selections", "e8e24cd90169ec82cd0d22bca18d370fa199fef64addde9bda2e4a3aa65808e2"),
    ("name:Download redacted nightly Bazel health metrics", "5cc1530adeb58f98710fac6b9757f58eb964486b64b5f1e5d464a7835e66b602"),
    ("name:Generate the nightly Bazel CI health dashboard", "1463857245703eb56f2717bc1a1e96b84cb8409a51adbe44af45ef4c3ee11838"),
    ("name:Upload the nightly Bazel CI health dashboard", "d91598640a677c0f1ee643b70c473d7ee4ad25fdd3dc8a1e5152c373cc6c3721"),
    ("name:Aggregate the complete nightly Bazel verdict", "05af79101cc7b2d765f961a68aa6da62472930d9c0e119470385a179dcda8ac3"),
)
# fmt: on


def _error(code: str, message: str) -> str:
    return f"[{code}] {message}"


def _top_level_symbols(path: Path) -> set[str]:
    tree = ast.parse(path.read_text(encoding="utf-8"), filename=str(path))
    return {
        node.name
        for node in tree.body
        if isinstance(node, (ast.ClassDef, ast.FunctionDef, ast.AsyncFunctionDef))
    }


def _top_level_assignments(path: Path) -> set[str]:
    tree = ast.parse(path.read_text(encoding="utf-8"), filename=str(path))
    names: set[str] = set()
    for node in tree.body:
        if isinstance(node, ast.Assign):
            targets = node.targets
        elif isinstance(node, ast.AnnAssign):
            targets = [node.target]
        else:
            continue
        names.update(target.id for target in targets if isinstance(target, ast.Name))
    return names


def _imports(path: Path) -> set[str]:
    tree = ast.parse(path.read_text(encoding="utf-8"), filename=str(path))
    names: set[str] = set()
    for node in ast.walk(tree):
        if isinstance(node, ast.Import):
            names.update(alias.name for alias in node.names)
        elif isinstance(node, ast.ImportFrom) and node.module:
            names.add(node.module)
    return names


def _tracked_paths(root: Path) -> tuple[str, ...]:
    try:
        result = affected.run_git(
            ["ls-files", "--cached", "--others", "--exclude-standard", "-z"],
            root=root,
        )
    except affected.SelectionError as error:
        raise ContractError("AFFECTED-GLOBAL-008", "tracked-path inventory failed") from error
    if result.returncode:
        raise ContractError("AFFECTED-GLOBAL-008", "tracked-path inventory failed")
    try:
        return tuple(
            sorted(
                field.decode("utf-8", errors="strict")
                for field in result.stdout.split(b"\0")
                if field
            )
        )
    except UnicodeError as error:
        raise ContractError("AFFECTED-GLOBAL-008", "tracked-path inventory failed") from error


def _boundary_entries(paths: tuple[str, ...], boundary: str) -> set[str]:
    if not boundary:
        return {path.split("/", 1)[0] for path in paths}
    prefix = f"{boundary}/"
    return {
        path[len(prefix) :].split("/", 1)[0]
        for path in paths
        if path.startswith(prefix) and path != prefix
    }


def _review_boundary_errors(contract: GlobalInputContract, paths: tuple[str, ...]) -> list[str]:
    errors: list[str] = []
    for boundary, expected_entries in contract.review_boundaries:
        expected = set(expected_entries)
        actual = _boundary_entries(paths, boundary)
        if actual - expected:
            errors.append(
                _error("AFFECTED-GLOBAL-006", "an unreviewed repository authority exists")
            )
        if expected - actual:
            errors.append(_error("AFFECTED-GLOBAL-007", "a reviewed authority is stale"))
    return errors


def _activation_errors(path: Path) -> list[str]:
    try:
        payload = load_global_input_payload(path)
    except ContractError as error:
        return [str(error)]
    activation = payload.get("activation")
    if not isinstance(activation, dict) or set(activation) != {
        "blockers",
        "release",
        "state",
        "tool",
    }:
        return [_error("AFFECTED-GLOBAL-009", "graph-native activation evidence is invalid")]
    errors: list[str] = []
    if activation.get("state") != "blocked":
        errors.append(
            _error(
                "AFFECTED-GLOBAL-009",
                "graph-native activation must remain blocked pending evidence",
            )
        )
    expected_blockers = [
        "bazel_version_parse_not_qualified",
        "external_required_workflow_not_active",
        "full_graph_linux_not_qualified",
        "remote_cache_not_qualified",
        "workspace_restoration_not_hardened",
    ]
    if activation.get("blockers") != expected_blockers:
        errors.append(_error("AFFECTED-GLOBAL-009", "graph-native blockers are invalid"))
    release = activation.get("release")
    if not isinstance(release, dict) or set(release) != {"assets", "commit", "license", "tag"}:
        errors.append(_error("AFFECTED-GLOBAL-010", "graph-native release pin is invalid"))
        return errors
    if (
        activation.get("tool") != "bazel-contrib/target-determinator"
        or release.get("tag") != "v0.34.0"
        or release.get("commit") != "d4b6125546979713431e63b5c3e65810fa989446"
        or release.get("license") != "Apache-2.0"
    ):
        errors.append(_error("AFFECTED-GLOBAL-010", "graph-native release identity drifted"))
    assets = release.get("assets")
    expected_assets = {
        "aarch64-darwin": (
            "target-determinator.darwin.arm64",
            "1405ff844db1255fc1e10f28c04ed72ced648822c2a5d39a393a4d6a6b7b890d",
        ),
        "aarch64-linux": (
            "target-determinator.linux.arm64",
            "e818a59b1813ba4053eb0011a5302932cdc32a7879ae019ac4ef8f879c3953a9",
        ),
        "x86_64-linux": (
            "target-determinator.linux.amd64",
            "115e1c63d39e2cd0d0b011c9fadc80f059f021176a4ae0de2232cdd83b1f8011",
        ),
    }
    if not isinstance(assets, dict) or set(assets) != set(expected_assets):
        errors.append(_error("AFFECTED-GLOBAL-010", "graph-native release assets drifted"))
        return errors
    for system, (expected_name, expected_digest) in expected_assets.items():
        asset = assets.get(system)
        if (
            not isinstance(asset, dict)
            or set(asset) != {"name", "sha256"}
            or asset.get("name") != expected_name
            or asset.get("sha256") != expected_digest
        ):
            errors.append(_error("AFFECTED-GLOBAL-010", "a release asset pin is invalid"))
    return errors


def _mapping(value: Any) -> dict[str, Any] | None:
    if not isinstance(value, dict) or not all(isinstance(key, str) for key in value):
        return None
    return value


def _step_contract_is_exact(job: dict[str, Any], expected: tuple[tuple[str, str], ...]) -> bool:
    steps = job.get("steps")
    if not isinstance(steps, list):
        return False
    actual: list[tuple[str, str]] = []
    try:
        for step in steps:
            if not isinstance(step, dict):
                return False
            name = step.get("name")
            uses = step.get("uses")
            if isinstance(name, str) and name:
                identity = f"name:{name}"
            elif isinstance(uses, str) and uses:
                identity = f"uses:{uses}"
            else:
                return False
            canonical = json.dumps(
                step,
                ensure_ascii=True,
                separators=(",", ":"),
                sort_keys=True,
            ).encode("ascii")
            actual.append((identity, hashlib.sha256(canonical).hexdigest()))
    except (TypeError, UnicodeError, ValueError):
        return False
    return tuple(actual) == expected


def _verdict_job_is_unique(
    jobs: dict[str, Any] | None, *, expected_id: str, expected_name: str
) -> bool:
    if jobs is None:
        return False
    matching_ids = [
        job_id
        for job_id, candidate in jobs.items()
        if isinstance(candidate, dict) and candidate.get("name") == expected_name
    ]
    return matching_ids == [expected_id]


def _verdict_context_errors(workflows: dict[str, dict[str, Any]]) -> list[str]:
    protected_names = {"bazel / verdict", "nightly Bazel / verdict"}
    expected = {
        (".github/workflows/nightly.yml", "bazel-nightly", "nightly Bazel / verdict"),
        (".github/workflows/presubmit.yml", "bazel", "bazel / verdict"),
    }
    observed: set[tuple[str, str, str]] = set()
    allowed_dynamic_names = {
        (
            ".github/workflows/nightly.yml",
            "bazel-nightly-workers",
            "nightly Bazel / worker ${{ matrix.worker }}",
        ),
        (
            ".github/workflows/presubmit.yml",
            "bazel-workers",
            "bazel / worker ${{ matrix.worker }}",
        ),
    }
    for path, workflow in workflows.items():
        jobs = _mapping(workflow.get("jobs"))
        if jobs is None:
            continue
        for job_id, candidate in jobs.items():
            job = _mapping(candidate)
            name = job.get("name") if job is not None else None
            if (
                isinstance(name, str)
                and "${{" in name
                and (path, job_id, name) not in allowed_dynamic_names
            ):
                return [_error("AFFECTED-WORKFLOW-010", "Bazel verdict context is ambiguous")]
            if name in protected_names:
                observed.add((path, job_id, name))
    if observed != expected:
        return [_error("AFFECTED-WORKFLOW-010", "Bazel verdict context is ambiguous")]
    return []


def _named_step(job: dict[str, Any], name: str) -> dict[str, Any] | None:
    steps = job.get("steps")
    if not isinstance(steps, list):
        return None
    matches = [step for step in steps if isinstance(step, dict) and step.get("name") == name]
    if len(matches) != 1:
        return None
    return matches[0]


def _steps_are_ordered(job: dict[str, Any], identities: tuple[str, ...]) -> bool:
    steps = job.get("steps")
    if not isinstance(steps, list):
        return False
    observed: list[int] = []
    for identity in identities:
        matches = [
            index
            for index, step in enumerate(steps)
            if isinstance(step, dict)
            and (step.get("name") == identity or step.get("uses") == identity)
        ]
        if len(matches) != 1:
            return False
        observed.append(matches[0])
    return observed == sorted(observed)


def _command(value: Any) -> tuple[str, ...] | None:
    if not isinstance(value, str):
        return None
    try:
        return tuple(shlex.split(value, comments=False, posix=True))
    except ValueError:
        return None


def _checkout_is_complete(job: dict[str, Any], *, full_history: bool, before_step: str) -> bool:
    steps = job.get("steps")
    if not isinstance(steps, list):
        return False
    checkout_indices = [
        index
        for index, step in enumerate(steps)
        if isinstance(step, dict)
        and isinstance(step.get("uses"), str)
        and step["uses"].startswith("actions/checkout@")
    ]
    governed_indices = [
        index
        for index, step in enumerate(steps)
        if isinstance(step, dict) and step.get("name") == before_step
    ]
    if len(checkout_indices) != 1 or len(governed_indices) != 1:
        return False
    checkout_index = checkout_indices[0]
    checkout = steps[checkout_index]
    expected_configuration = {"persist-credentials": False}
    if full_history:
        expected_configuration["fetch-depth"] = 0
    return (
        checkout_index < governed_indices[0]
        and set(checkout) == {"uses", "with"}
        and checkout.get("uses") == CHECKOUT_ACTION
        and _mapping(checkout.get("with")) == expected_configuration
    )


def _uploads_are_governed(job: dict[str, Any], expected: dict[str, dict[str, Any]]) -> bool:
    for name, expected_contract in expected.items():
        expected_configuration = {
            key: value for key, value in expected_contract.items() if key != "__if__"
        }
        expected_condition = expected_contract.get("__if__", "always()")
        step = _named_step(job, name)
        configuration = _mapping(step.get("with")) if step is not None else None
        if (
            step is None
            or set(step) != {"if", "name", "uses", "with"}
            or step.get("if") != expected_condition
            or "continue-on-error" in step
            or step.get("uses") != UPLOAD_ARTIFACT_ACTION
            or configuration != expected_configuration
        ):
            return False
    return True


def _presubmit_cache_routing_is_governed(plan: dict[str, Any], workers: dict[str, Any]) -> bool:
    plan_remote_route = _named_step(plan, "Select qualified Bazel remote-cache route for topology")
    plan_matrix = _named_step(plan, "Select presubmit, fallback, or complete shard workers")
    worker_remote_route = _named_step(workers, "Select qualified Bazel remote-cache route")
    topology_check = _named_step(
        workers, "Verify worker topology still matches cache qualification"
    )
    disk_route = _named_step(workers, "Select trusted Bazel cache revision")
    disk_restore = _named_step(workers, "Restore trusted Bazel persistent action cache")
    disk_configure = _named_step(workers, "Configure bounded Bazel persistent action cache")
    governed_run = _named_step(workers, "Run event-governed Bazel validation")
    redaction = _named_step(workers, "Redact completed Bazel worker selection")
    cache_measure = _named_step(workers, "Measure bounded Bazel persistent action cache")
    plan_remote_env = (
        _mapping(plan_remote_route.get("env")) if plan_remote_route is not None else None
    )
    plan_matrix_env = _mapping(plan_matrix.get("env")) if plan_matrix is not None else None
    worker_remote_env = (
        _mapping(worker_remote_route.get("env")) if worker_remote_route is not None else None
    )
    topology_env = _mapping(topology_check.get("env")) if topology_check is not None else None
    disk_env = _mapping(disk_route.get("env")) if disk_route is not None else None
    configure_env = _mapping(disk_configure.get("env")) if disk_configure is not None else None
    governed_env = _mapping(governed_run.get("env")) if governed_run is not None else None
    return (
        plan_remote_env is not None
        and plan_remote_route is not None
        and "if" not in plan_remote_route
        and plan_remote_env.get("PR_BASE_REF") == PULL_REQUEST_CACHE_BASE_REF
        and plan_matrix_env is not None
        and plan_matrix_env.get("REMOTE_CACHE_ENABLED") == REMOTE_CACHE_ENABLED_EXPRESSION
        and worker_remote_env is not None
        and worker_remote_route is not None
        and "if" not in worker_remote_route
        and worker_remote_env.get("PR_BASE_REF") == PULL_REQUEST_CACHE_BASE_REF
        and topology_env is not None
        and topology_env.get("ACTUAL_REMOTE_CACHE_ENABLED") == REMOTE_CACHE_ENABLED_EXPRESSION
        and disk_env is not None
        and disk_route is not None
        and disk_route.get("if") == PERSISTENT_CACHE_TRUST_IF
        and disk_env.get("PR_BASE_REF") == PULL_REQUEST_CACHE_BASE_REF
        and disk_env.get("PR_BASE_SHA") == PULL_REQUEST_CACHE_BASE_SHA
        and disk_restore is not None
        and disk_restore.get("if") == PERSISTENT_CACHE_RESTORE_IF
        and configure_env is not None
        and configure_env.get("BAZEL_CACHE_ROLE") == PERSISTENT_CACHE_ROLE_EXPRESSION
        and governed_env is not None
        and governed_env.get("PR_BASE_SHA") == PULL_REQUEST_SELECTION_BASE_SHA
        and governed_env.get("REMOTE_CACHE_ENABLED") == REMOTE_CACHE_ENABLED_EXPRESSION
        and governed_env.get("BAZEL_CACHE_ROLE") == GOVERNED_CACHE_ROLE_EXPRESSION
        and redaction is not None
        and redaction.get("id") == "bazel-selection-redact"
        and redaction.get("if") == "steps.bazel-validation.outcome == 'success'"
        and cache_measure is not None
        and cache_measure.get("if") == PERSISTENT_CACHE_MEASURE_IF
    )


def _checkout_python_bytecode_is_disabled(job: dict[str, Any], *, through_step: str) -> bool:
    steps = job.get("steps")
    if not isinstance(steps, list):
        return False
    boundaries = [
        index
        for index, step in enumerate(steps)
        if isinstance(step, dict) and step.get("name") == through_step
    ]
    if len(boundaries) != 1:
        return False
    observed = False
    for step in steps[: boundaries[0] + 1]:
        if not isinstance(step, dict) or "run" not in step:
            continue
        run = step.get("run")
        if not isinstance(run, str):
            return False
        try:
            command = tuple(
                shlex.split(run.replace(".#", "mindclade-flake-"), comments=True, posix=True)
            )
        except ValueError:
            return False
        for index, token in enumerate(command):
            if token != "python3":
                continue
            observed = True
            if index + 1 >= len(command) or command[index + 1] != "-B":
                return False
    return observed


def _presubmit_workflow_errors(workflow: dict[str, Any]) -> list[str]:
    errors: list[str] = []
    if set(workflow) != {"concurrency", "jobs", "name", "on", "permissions"}:
        errors.append(_error("AFFECTED-WORKFLOW-004", "presubmit workflow keys are invalid"))
    events = _mapping(workflow.get("on"))
    if events is None or set(events) != PRESUBMIT_EVENTS:
        errors.append(_error("AFFECTED-WORKFLOW-003", "presubmit event contract is invalid"))
    else:
        push = _mapping(events.get("push"))
        if (
            push is None
            or push.get("branches") != ["main"]
            or events.get("pull_request") != {}
            or events.get("merge_group") != {}
        ):
            errors.append(_error("AFFECTED-WORKFLOW-003", "presubmit event routing is invalid"))
    if workflow.get("permissions") != {"contents": "read"}:
        errors.append(_error("AFFECTED-WORKFLOW-004", "presubmit permissions are invalid"))
    jobs = _mapping(workflow.get("jobs"))
    if not _verdict_job_is_unique(jobs, expected_id="bazel", expected_name="bazel / verdict"):
        errors.append(_error("AFFECTED-WORKFLOW-004", "presubmit verdict job is ambiguous"))
    plan = _mapping(jobs.get("bazel-worker-plan")) if jobs is not None else None
    workers = _mapping(jobs.get("bazel-workers")) if jobs is not None else None
    verdict = _mapping(jobs.get("bazel")) if jobs is not None else None
    if (
        plan is None
        or set(plan) != {"name", "outputs", "runs-on", "steps", "timeout-minutes"}
        or plan.get("name") != "bazel / worker plan"
        or plan.get("runs-on") != "ubuntu-24.04"
        or plan.get("timeout-minutes") != 5
        or plan.get("outputs")
        != {
            "workers": "${{ steps.worker-matrix.outputs.workers }}",
            "remote_cache_enabled": REMOTE_CACHE_ENABLED_EXPRESSION,
            "shard_count": "${{ steps.worker-matrix.outputs.shard_count }}",
            "topology_mode": "${{ steps.worker-matrix.outputs.mode }}",
        }
    ):
        errors.append(_error("AFFECTED-WORKFLOW-004", "presubmit Bazel plan is invalid"))
    elif not _step_contract_is_exact(plan, PRESUBMIT_PLAN_STEP_CONTRACT):
        errors.append(_error("AFFECTED-WORKFLOW-009", "presubmit Bazel plan steps drifted"))
    if plan is not None and not _steps_are_ordered(
        plan,
        (
            "actions/setup-python@ece7cb06caefa5fff74198d8649806c4678c61a1",
            "Select qualified Bazel remote-cache route for topology",
            "Select presubmit, fallback, or complete shard workers",
        ),
    ):
        errors.append(_error("AFFECTED-WORKFLOW-004", "presubmit Bazel plan toolchain is invalid"))
    if plan is not None and not _checkout_python_bytecode_is_disabled(
        plan,
        through_step="Select presubmit, fallback, or complete shard workers",
    ):
        errors.append(_error("AFFECTED-WORKFLOW-012", "presubmit Python launch is invalid"))
    if (
        workers is None
        or set(workers)
        != {
            "if",
            "name",
            "needs",
            "permissions",
            "runs-on",
            "steps",
            "strategy",
            "timeout-minutes",
        }
        or workers.get("name") != "bazel / worker ${{ matrix.worker }}"
        or workers.get("needs") != ["bazel-worker-plan"]
        or workers.get("if") != "needs.bazel-worker-plan.result == 'success'"
        or workers.get("permissions") != {"contents": "read"}
        or workers.get("runs-on") != "ubuntu-24.04"
        or workers.get("timeout-minutes") != 90
        or workers.get("strategy")
        != {
            "fail-fast": False,
            "matrix": {"worker": "${{ fromJSON(needs.bazel-worker-plan.outputs.workers) }}"},
        }
    ):
        return [*errors, _error("AFFECTED-WORKFLOW-004", "presubmit Bazel workers are invalid")]
    if not _step_contract_is_exact(workers, PRESUBMIT_BAZEL_STEP_CONTRACT):
        errors.append(_error("AFFECTED-WORKFLOW-009", "presubmit Bazel worker steps drifted"))
    if plan is None or not _presubmit_cache_routing_is_governed(plan, workers):
        errors.append(_error("AFFECTED-WORKFLOW-011", "presubmit cache routing is invalid"))
    if not _checkout_python_bytecode_is_disabled(
        workers,
        through_step="Run event-governed Bazel validation",
    ):
        errors.append(_error("AFFECTED-WORKFLOW-012", "presubmit Python launch is invalid"))
    if not _checkout_is_complete(
        workers,
        full_history=True,
        before_step="Run event-governed Bazel validation",
    ):
        errors.append(_error("AFFECTED-WORKFLOW-004", "presubmit checkout is incomplete"))
    step = _named_step(workers, "Run event-governed Bazel validation")
    if step is None:
        errors.append(_error("AFFECTED-WORKFLOW-005", "governed Bazel step is missing"))
    elif (
        set(step) != {"env", "id", "name", "run"}
        or step.get("id") != "bazel-validation"
        or step.get("env")
        != {
            "BASH_ENV": "",
            "BAZEL_CACHE_MODE": (
                "${{ steps.bazel-remote-cache.outputs.enabled == 'true' && 'remote' || 'disk' }}"
            ),
            "BAZEL_CACHE_ROLE": GOVERNED_CACHE_ROLE_EXPRESSION,
            "REMOTE_CACHE_ENABLED": REMOTE_CACHE_ENABLED_EXPRESSION,
            "PR_BASE_SHA": PULL_REQUEST_SELECTION_BASE_SHA,
            "SHARD_COUNT": "${{ needs.bazel-worker-plan.outputs.shard_count }}",
            "WORKER": "${{ matrix.worker }}",
        }
        or not isinstance(step.get("run"), str)
        or any(
            fragment not in step["run"]
            for fragment in (
                "--mode auto",
                '--ref "${GITHUB_REF}"',
                '--head "${GITHUB_SHA}"',
                '--cache-mode "${BAZEL_CACHE_MODE}"',
                '--cache-role "${BAZEL_CACHE_ROLE}"',
                '--shard-index "${WORKER}"',
                '--shard-count "${SHARD_COUNT}"',
                "BAZEL_MATRIX_FALLBACK_ROUTE_MISMATCH",
                "BAZEL_MATRIX_SHARD_ROUTE_MISMATCH",
            )
        )
        or "--mode affected" in step["run"]
        or "--static-only" in step["run"]
    ):
        errors.append(_error("AFFECTED-WORKFLOW-005", "governed Bazel command is invalid"))
    if not _uploads_are_governed(
        workers,
        {
            "Upload Bazel performance evidence": {
                "name": (
                    "bazel-performance-${{ github.run_id }}-"
                    "${{ github.run_attempt }}-${{ matrix.worker }}"
                ),
                "path": "${{ runner.temp }}/bazel-evidence/*",
                "if-no-files-found": "warn",
                "retention-days": 35,
            },
            "Upload redacted Bazel health metrics": {
                "__if__": "steps.bazel-validation.outcome == 'success'",
                "name": (
                    "bazel-health-${{ github.run_id }}-"
                    "${{ github.run_attempt }}-${{ matrix.worker }}"
                ),
                "path": (
                    "${{ runner.temp }}/bazel-evidence/run-metrics.json\n"
                    "${{ runner.temp }}/bazel-evidence/analysis.summary.json\n"
                    "${{ runner.temp }}/bazel-evidence/test.summary.json\n"
                ),
                "if-no-files-found": "error",
                "retention-days": 35,
            },
            "Upload redacted Bazel worker selection": {
                "__if__": "steps.bazel-selection-redact.outcome == 'success'",
                "name": (
                    "bazel-selection-${{ github.run_id }}-"
                    "${{ github.run_attempt }}-${{ matrix.worker }}"
                ),
                "path": "${{ runner.temp }}/bazel-worker-selection/worker-selection.json",
                "if-no-files-found": "error",
                "retention-days": 7,
            },
            "Upload Bazel latency metric": {
                "__if__": "always() && matrix.worker == -1",
                "name": (
                    "bazel-metrics-${{ github.run_id }}-"
                    "${{ github.run_attempt }}-${{ matrix.worker }}"
                ),
                "path": "${{ runner.temp }}/bazel-evidence/run-metrics.json",
                "if-no-files-found": "ignore",
                "retention-days": 35,
            },
        },
    ):
        errors.append(_error("AFFECTED-WORKFLOW-006", "Bazel evidence retention is invalid"))
    if (
        verdict is None
        or set(verdict) != {"if", "name", "needs", "runs-on", "steps", "timeout-minutes"}
        or verdict.get("name") != "bazel / verdict"
        or verdict.get("needs") != ["bazel-worker-plan", "bazel-workers"]
        or verdict.get("if") != "always()"
        or verdict.get("runs-on") != "ubuntu-24.04"
        or verdict.get("timeout-minutes") != 5
    ):
        errors.append(_error("AFFECTED-WORKFLOW-004", "presubmit Bazel verdict is invalid"))
    elif not _step_contract_is_exact(verdict, PRESUBMIT_VERDICT_STEP_CONTRACT):
        errors.append(_error("AFFECTED-WORKFLOW-009", "presubmit Bazel verdict steps drifted"))
    if verdict is not None and not _checkout_python_bytecode_is_disabled(
        verdict,
        through_step="Aggregate the complete Bazel verdict",
    ):
        errors.append(_error("AFFECTED-WORKFLOW-012", "presubmit Python launch is invalid"))
    if verdict is not None:
        download = _named_step(verdict, "Download redacted Bazel worker selections")
        health_download = _named_step(verdict, "Download redacted Bazel health metrics")
        health_generate = _named_step(verdict, "Generate the Bazel CI health dashboard")
        health_upload = _named_step(verdict, "Upload the Bazel CI health dashboard")
        aggregate = _named_step(verdict, "Aggregate the complete Bazel verdict")
        if (
            download is None
            or set(download) != {"continue-on-error", "id", "name", "uses", "with"}
            or download.get("continue-on-error") is not True
            or download.get("uses") != DOWNLOAD_ARTIFACT_ACTION
            or download.get("with")
            != {
                "pattern": ("bazel-selection-${{ github.run_id }}-${{ github.run_attempt }}-*"),
                "path": "${{ runner.temp }}/bazel-worker-selections",
            }
            or health_download is None
            or set(health_download) != {"if", "name", "uses", "with"}
            or health_download.get("if")
            != (
                "needs.bazel-worker-plan.result == 'success' && "
                "needs.bazel-workers.result == 'success'"
            )
            or health_download.get("uses") != DOWNLOAD_ARTIFACT_ACTION
            or health_download.get("with")
            != {
                "pattern": "bazel-health-${{ github.run_id }}-${{ github.run_attempt }}-*",
                "path": "${{ runner.temp }}/bazel-health-evidence",
            }
            or health_generate is None
            or health_generate.get("env")
            != {
                "EXPECTED_WORKERS": "${{ needs.bazel-worker-plan.outputs.workers }}",
                "SHARD_COUNT": "${{ needs.bazel-worker-plan.outputs.shard_count }}",
                "TOPOLOGY_MODE": "${{ needs.bazel-worker-plan.outputs.topology_mode }}",
            }
            or health_generate.get("if")
            != (
                "needs.bazel-worker-plan.result == 'success' && "
                "needs.bazel-workers.result == 'success'"
            )
            or not isinstance(health_generate.get("run"), str)
            or "ci/common/ci_health.py" not in health_generate["run"]
            or "--lane presubmit" not in health_generate["run"]
            or '--event "${GITHUB_EVENT_NAME}"' not in health_generate["run"]
            or '--head-sha "${GITHUB_SHA}"' not in health_generate["run"]
            or '--expected-workers "${EXPECTED_WORKERS}"' not in health_generate["run"]
            or health_upload is None
            or health_upload.get("uses") != UPLOAD_ARTIFACT_ACTION
            or health_upload.get("if")
            != (
                "needs.bazel-worker-plan.result == 'success' && "
                "needs.bazel-workers.result == 'success'"
            )
            or health_upload.get("with")
            != {
                "name": "ci-health-${{ github.run_id }}-${{ github.run_attempt }}",
                "path": "${{ runner.temp }}/ci-health/*",
                "if-no-files-found": "error",
                "retention-days": 35,
            }
            or aggregate is None
            or aggregate.get("if") != "always()"
            or aggregate.get("env")
            != {
                "EXPECTED_WORKERS": "${{ needs.bazel-worker-plan.outputs.workers }}",
                "PLAN_RESULT": "${{ needs.bazel-worker-plan.result }}",
                "SHARD_COUNT": "${{ needs.bazel-worker-plan.outputs.shard_count }}",
                "TOPOLOGY_MODE": "${{ needs.bazel-worker-plan.outputs.topology_mode }}",
                "WORKERS_RESULT": "${{ needs.bazel-workers.result }}",
            }
            or not isinstance(aggregate.get("run"), str)
            or "bazel_verdict.py verify" not in aggregate["run"]
            or '--expected-workers "${EXPECTED_WORKERS}"' not in aggregate["run"]
            or '--selection-root "${RUNNER_TEMP}/bazel-worker-selections"' not in aggregate["run"]
        ):
            errors.append(_error("AFFECTED-WORKFLOW-004", "presubmit Bazel evidence is invalid"))
    return errors


def _nightly_target_errors(path: Path) -> list[str]:
    def unique_object(pairs: list[tuple[str, Any]]) -> dict[str, Any]:
        result: dict[str, Any] = {}
        for key, value in pairs:
            if key in result:
                raise ValueError("duplicate key")
            result[key] = value
        return result

    try:
        lines = [
            line
            for line in path.read_text(encoding="utf-8").splitlines()
            if line.strip() and not line.lstrip().startswith("#") and line.strip() != "---"
        ]
        payload = json.loads(
            "\n".join(lines),
            object_pairs_hook=unique_object,
            parse_constant=lambda _value: (_ for _ in ()).throw(ValueError("constant")),
        )
    except (OSError, UnicodeError, json.JSONDecodeError, RecursionError, ValueError):
        return [_error("AFFECTED-WORKFLOW-007", "nightly target contract is unreadable")]
    if payload != {
        "schema_version": 2,
        "mode": "full",
        "shard_count": 4,
        "partition_contract": "ci/bazel/full_graph_shards.toml",
    }:
        return [_error("AFFECTED-WORKFLOW-007", "nightly target contract is not full graph")]
    return []


def _nightly_workflow_errors(workflow: dict[str, Any]) -> list[str]:
    errors: list[str] = []
    if set(workflow) != {"concurrency", "jobs", "name", "on", "permissions"}:
        errors.append(_error("AFFECTED-WORKFLOW-004", "nightly workflow keys are invalid"))
    events = _mapping(workflow.get("on"))
    schedule = events.get("schedule") if events is not None else None
    if (
        events is None
        or set(events) != NIGHTLY_EVENTS
        or events.get("workflow_dispatch") != {}
        or schedule != [{"cron": "17 5 * * *"}]
    ):
        errors.append(_error("AFFECTED-WORKFLOW-003", "nightly event contract is invalid"))
    if workflow.get("permissions") != {"actions": "read", "contents": "read"}:
        errors.append(_error("AFFECTED-WORKFLOW-004", "nightly permissions are invalid"))
    jobs = _mapping(workflow.get("jobs"))
    if not _verdict_job_is_unique(
        jobs,
        expected_id="bazel-nightly",
        expected_name="nightly Bazel / verdict",
    ):
        errors.append(_error("AFFECTED-WORKFLOW-004", "nightly verdict job is ambiguous"))
    plan = _mapping(jobs.get("bazel-nightly-plan")) if jobs is not None else None
    workers = _mapping(jobs.get("bazel-nightly-workers")) if jobs is not None else None
    verdict = _mapping(jobs.get("bazel-nightly")) if jobs is not None else None
    if (
        plan is None
        or set(plan)
        != {
            "if",
            "name",
            "outputs",
            "runs-on",
            "steps",
            "timeout-minutes",
        }
        or plan.get("name") != "nightly Bazel / worker plan"
        or plan.get("if") != "github.ref == 'refs/heads/main'"
        or plan.get("runs-on") != "ubuntu-24.04"
        or plan.get("timeout-minutes") != 5
        or plan.get("outputs")
        != {
            "workers": "${{ steps.worker-matrix.outputs.workers }}",
            "remote_cache_enabled": "${{ steps.bazel-remote-cache.outputs.enabled }}",
            "shard_count": "${{ steps.worker-matrix.outputs.shard_count }}",
            "topology_mode": "${{ steps.worker-matrix.outputs.mode }}",
        }
    ):
        errors.append(_error("AFFECTED-WORKFLOW-004", "nightly Bazel plan is invalid"))
    elif not _step_contract_is_exact(plan, NIGHTLY_PLAN_STEP_CONTRACT):
        errors.append(_error("AFFECTED-WORKFLOW-009", "nightly Bazel plan steps drifted"))
    if plan is not None and not _steps_are_ordered(
        plan,
        (
            "actions/setup-python@ece7cb06caefa5fff74198d8649806c4678c61a1",
            "Select qualified nightly Bazel remote-cache route for topology",
            "Select fallback or complete shard workers",
        ),
    ):
        errors.append(_error("AFFECTED-WORKFLOW-004", "nightly Bazel plan toolchain is invalid"))
    if plan is not None and not _checkout_python_bytecode_is_disabled(
        plan,
        through_step="Select fallback or complete shard workers",
    ):
        errors.append(_error("AFFECTED-WORKFLOW-012", "nightly Python launch is invalid"))
    if (
        workers is None
        or set(workers)
        != {
            "if",
            "name",
            "needs",
            "permissions",
            "runs-on",
            "steps",
            "strategy",
            "timeout-minutes",
        }
        or workers.get("name") != "nightly Bazel / worker ${{ matrix.worker }}"
        or workers.get("needs") != ["bazel-nightly-plan"]
        or workers.get("if") != "needs.bazel-nightly-plan.result == 'success'"
        or workers.get("permissions") != {"actions": "read", "contents": "read"}
        or workers.get("runs-on") != "ubuntu-24.04"
        or workers.get("timeout-minutes") != 90
        or workers.get("strategy")
        != {
            "fail-fast": False,
            "matrix": {"worker": "${{ fromJSON(needs.bazel-nightly-plan.outputs.workers) }}"},
        }
    ):
        return [*errors, _error("AFFECTED-WORKFLOW-004", "nightly Bazel workers are invalid")]
    if not _step_contract_is_exact(workers, NIGHTLY_BAZEL_STEP_CONTRACT):
        errors.append(_error("AFFECTED-WORKFLOW-009", "nightly Bazel worker steps drifted"))
    if not _checkout_python_bytecode_is_disabled(
        workers,
        through_step="Analyze and test the selected repository-graph workload",
    ):
        errors.append(_error("AFFECTED-WORKFLOW-012", "nightly Python launch is invalid"))
    if not _checkout_is_complete(
        workers,
        full_history=False,
        before_step="Analyze and test the selected repository-graph workload",
    ):
        errors.append(_error("AFFECTED-WORKFLOW-004", "nightly Bazel job can be bypassed"))
    step = _named_step(workers, "Analyze and test the selected repository-graph workload")
    if (
        step is None
        or set(step) != {"env", "name", "run"}
        or step.get("env")
        != {
            "BASH_ENV": "",
            "BAZEL_CACHE_MODE": (
                "${{ steps.bazel-remote-cache.outputs.enabled == 'true' && 'remote' || 'disk' }}"
            ),
            "BAZEL_CACHE_ROLE": (
                "${{ steps.bazel-remote-cache.outputs.enabled == 'true' "
                "&& steps.bazel-remote-cache.outputs.role "
                "|| steps.bazel-cache-trust.outputs.role }}"
            ),
            "REMOTE_CACHE_ENABLED": "${{ steps.bazel-remote-cache.outputs.enabled }}",
            "SHARD_COUNT": "${{ needs.bazel-nightly-plan.outputs.shard_count }}",
            "WORKER": "${{ matrix.worker }}",
        }
        or not isinstance(step.get("run"), str)
        or any(
            fragment not in step["run"]
            for fragment in (
                '--ref "${GITHUB_REF}"',
                '--head "${GITHUB_SHA}"',
                '--cache-mode "${BAZEL_CACHE_MODE}"',
                '--cache-role "${BAZEL_CACHE_ROLE}"',
                '--shard-index "${WORKER}"',
                '--shard-count "${SHARD_COUNT}"',
                "BAZEL_MATRIX_FALLBACK_ROUTE_MISMATCH",
                "BAZEL_MATRIX_SHARD_ROUTE_MISMATCH",
            )
        )
    ):
        errors.append(_error("AFFECTED-WORKFLOW-005", "nightly Bazel command is invalid"))
    if not _uploads_are_governed(
        workers,
        {
            "Upload nightly Bazel evidence": {
                "name": (
                    "bazel-nightly-${{ github.run_id }}-"
                    "${{ github.run_attempt }}-${{ matrix.worker }}"
                ),
                "path": "${{ runner.temp }}/bazel-evidence/*",
                "if-no-files-found": "warn",
                "retention-days": 35,
            },
            "Upload redacted nightly Bazel health metrics": {
                "__if__": "always()",
                "name": (
                    "bazel-nightly-health-${{ github.run_id }}-"
                    "${{ github.run_attempt }}-${{ matrix.worker }}"
                ),
                "path": (
                    "${{ runner.temp }}/bazel-evidence/run-metrics.json\n"
                    "${{ runner.temp }}/bazel-evidence/analysis.summary.json\n"
                    "${{ runner.temp }}/bazel-evidence/test.summary.json\n"
                ),
                "if-no-files-found": "error",
                "retention-days": 35,
            },
            "Upload redacted nightly Bazel worker selection": {
                "name": (
                    "bazel-selection-${{ github.run_id }}-"
                    "${{ github.run_attempt }}-${{ matrix.worker }}"
                ),
                "path": "${{ runner.temp }}/bazel-worker-selection/worker-selection.json",
                "if-no-files-found": "error",
                "retention-days": 7,
            },
        },
    ):
        errors.append(_error("AFFECTED-WORKFLOW-006", "nightly evidence retention is invalid"))
    if (
        verdict is None
        or set(verdict) != {"if", "name", "needs", "runs-on", "steps", "timeout-minutes"}
        or verdict.get("name") != "nightly Bazel / verdict"
        or verdict.get("needs") != ["bazel-nightly-plan", "bazel-nightly-workers"]
        or verdict.get("if") != "always() && github.ref == 'refs/heads/main'"
        or verdict.get("runs-on") != "ubuntu-24.04"
        or verdict.get("timeout-minutes") != 5
    ):
        errors.append(_error("AFFECTED-WORKFLOW-004", "nightly Bazel verdict is invalid"))
    elif not _step_contract_is_exact(verdict, NIGHTLY_VERDICT_STEP_CONTRACT):
        errors.append(_error("AFFECTED-WORKFLOW-009", "nightly Bazel verdict steps drifted"))
    if verdict is not None and not _checkout_python_bytecode_is_disabled(
        verdict,
        through_step="Aggregate the complete nightly Bazel verdict",
    ):
        errors.append(_error("AFFECTED-WORKFLOW-012", "nightly Python launch is invalid"))
    if verdict is not None:
        download = _named_step(verdict, "Download redacted nightly Bazel worker selections")
        health_download = _named_step(verdict, "Download redacted nightly Bazel health metrics")
        health_generate = _named_step(verdict, "Generate the nightly Bazel CI health dashboard")
        health_upload = _named_step(verdict, "Upload the nightly Bazel CI health dashboard")
        aggregate = _named_step(verdict, "Aggregate the complete nightly Bazel verdict")
        if (
            download is None
            or set(download) != {"continue-on-error", "id", "name", "uses", "with"}
            or download.get("continue-on-error") is not True
            or download.get("uses") != DOWNLOAD_ARTIFACT_ACTION
            or download.get("with")
            != {
                "pattern": ("bazel-selection-${{ github.run_id }}-${{ github.run_attempt }}-*"),
                "path": "${{ runner.temp }}/bazel-worker-selections",
            }
            or health_download is None
            or set(health_download) != {"if", "name", "uses", "with"}
            or health_download.get("if")
            != (
                "needs.bazel-nightly-plan.result == 'success' && "
                "needs.bazel-nightly-workers.result == 'success'"
            )
            or health_download.get("uses") != DOWNLOAD_ARTIFACT_ACTION
            or health_download.get("with")
            != {
                "pattern": (
                    "bazel-nightly-health-${{ github.run_id }}-${{ github.run_attempt }}-*"
                ),
                "path": "${{ runner.temp }}/bazel-health-evidence",
            }
            or health_generate is None
            or health_generate.get("env")
            != {
                "EXPECTED_WORKERS": "${{ needs.bazel-nightly-plan.outputs.workers }}",
                "SHARD_COUNT": "${{ needs.bazel-nightly-plan.outputs.shard_count }}",
                "TOPOLOGY_MODE": "${{ needs.bazel-nightly-plan.outputs.topology_mode }}",
            }
            or health_generate.get("if")
            != (
                "needs.bazel-nightly-plan.result == 'success' && "
                "needs.bazel-nightly-workers.result == 'success'"
            )
            or not isinstance(health_generate.get("run"), str)
            or "ci/common/ci_health.py" not in health_generate["run"]
            or "--lane nightly" not in health_generate["run"]
            or '--event "${GITHUB_EVENT_NAME}"' not in health_generate["run"]
            or '--head-sha "${GITHUB_SHA}"' not in health_generate["run"]
            or '--expected-workers "${EXPECTED_WORKERS}"' not in health_generate["run"]
            or health_upload is None
            or health_upload.get("uses") != UPLOAD_ARTIFACT_ACTION
            or health_upload.get("if")
            != (
                "needs.bazel-nightly-plan.result == 'success' && "
                "needs.bazel-nightly-workers.result == 'success'"
            )
            or health_upload.get("with")
            != {
                "name": "ci-health-nightly-${{ github.run_id }}-${{ github.run_attempt }}",
                "path": "${{ runner.temp }}/ci-health/*",
                "if-no-files-found": "error",
                "retention-days": 35,
            }
            or aggregate is None
            or aggregate.get("if") != "always()"
            or aggregate.get("env")
            != {
                "EXPECTED_WORKERS": "${{ needs.bazel-nightly-plan.outputs.workers }}",
                "PLAN_RESULT": "${{ needs.bazel-nightly-plan.result }}",
                "SHARD_COUNT": "${{ needs.bazel-nightly-plan.outputs.shard_count }}",
                "TOPOLOGY_MODE": "${{ needs.bazel-nightly-plan.outputs.topology_mode }}",
                "WORKERS_RESULT": "${{ needs.bazel-nightly-workers.result }}",
            }
            or not isinstance(aggregate.get("run"), str)
            or "bazel_verdict.py verify" not in aggregate["run"]
            or '--expected-workers "${EXPECTED_WORKERS}"' not in aggregate["run"]
            or '--selection-root "${RUNNER_TEMP}/bazel-worker-selections"' not in aggregate["run"]
        ):
            errors.append(_error("AFFECTED-WORKFLOW-004", "nightly Bazel evidence is invalid"))
    return errors


def _selection_policy_errors() -> list[str]:
    cases = (
        ("pull_request", "refs/pull/1/merge", "0" * 40, "full"),
        ("merge_group", "refs/heads/gh-readonly-queue/main/pr-1", None, "full"),
        ("push", "refs/heads/main", None, "full"),
        ("schedule", "refs/heads/main", None, "full"),
        ("workflow_dispatch", "refs/heads/main", None, "full"),
    )
    for event, ref, base_sha, expected_mode in cases:
        try:
            actual_mode = affected.resolve_selection_mode(
                "auto",
                event=event,
                ref=ref,
                base_sha=base_sha,
            )
        except affected.SelectionError:
            return [_error("AFFECTED-WORKFLOW-008", "selection event policy is invalid")]
        if actual_mode != expected_mode:
            return [_error("AFFECTED-WORKFLOW-008", "selection event policy is invalid")]

        alternate_mode = "full" if expected_mode == "affected" else "affected"
        try:
            affected.resolve_selection_mode(
                alternate_mode,
                event=event,
                ref=ref,
                base_sha=base_sha,
            )
        except affected.SelectionError as error:
            if error.code != "AFFECTED-SELECT-010":
                return [_error("AFFECTED-WORKFLOW-008", "selection event policy is invalid")]
        else:
            return [_error("AFFECTED-WORKFLOW-008", "selection event policy is invalid")]
    return []


def _presubmit_orchestration_errors() -> list[str]:
    cases = (
        ("pull_request", "refs/pull/1/merge", "0" * 40, "full", "disk", "reader"),
        ("pull_request", "refs/pull/1/merge", "0" * 40, "full", "remote", "reader"),
        (
            "merge_group",
            "refs/heads/gh-readonly-queue/main/pr-1",
            "",
            "full",
            "disk",
            "reader",
        ),
        (
            "merge_group",
            "refs/heads/gh-readonly-queue/main/pr-1",
            "",
            "full",
            "remote",
            "writer",
        ),
        ("push", "refs/heads/main", "", "full", "disk", "writer"),
        ("push", "refs/heads/main", "", "full", "remote", "writer"),
    )
    evidence = Path("/tmp/mindclade-affected-orchestration")
    runner_temp = Path("/tmp/mindclade-affected-runner")
    started_file = runner_temp / "bazel-job-started"
    started_epoch = 123
    head = "1" * 40
    try:
        for event, ref, base_sha, expected_mode, cache_mode, cache_role in cases:
            changes = (
                (affected.Change(status="M", path="pkg/source.py"),)
                if expected_mode == "affected"
                else ()
            )
            canonical_base = "2" * 40 if expected_mode == "affected" else None
            selection = mock.Mock(
                mode=expected_mode,
                reason="orchestration_contract",
                analysis_targets=(),
                test_targets=(),
            )
            resolver = mock.Mock(return_value=expected_mode)
            bazelrc_authority = object()
            clean_checkout = mock.Mock(return_value=bazelrc_authority)
            started_loader = mock.Mock(return_value=started_epoch)
            revision = mock.Mock(return_value=canonical_base)
            changed = mock.Mock(return_value=changes)
            selector = mock.Mock(return_value=selection)
            executor = mock.Mock(return_value=0)
            failure_writer = mock.Mock()
            argv = [
                "pipeline.py",
                "--bazel-only",
                "--mode",
                "auto",
                "--base",
                base_sha,
                "--event",
                event,
                "--ref",
                ref,
                "--head",
                head,
                "--evidence-dir",
                str(evidence),
                "--job-started-at-file",
                str(started_file),
                "--runner-temp",
                str(runner_temp),
                "--cache-mode",
                cache_mode,
                "--cache-role",
                cache_role,
            ]
            with (
                mock.patch.object(sys, "argv", argv),
                mock.patch.object(
                    presubmit_pipeline.affected,
                    "resolve_selection_mode",
                    resolver,
                ),
                mock.patch.object(
                    presubmit_pipeline.affected,
                    "assert_clean_checkout",
                    clean_checkout,
                ),
                mock.patch.object(
                    presubmit_pipeline.affected,
                    "load_job_started_epoch",
                    started_loader,
                ),
                mock.patch.object(presubmit_pipeline.affected, "git_revision", revision),
                mock.patch.object(presubmit_pipeline.affected, "git_changed", changed),
                mock.patch.object(presubmit_pipeline.affected, "select", selector),
                mock.patch.object(
                    presubmit_pipeline.affected,
                    "execute_selection",
                    executor,
                ),
                mock.patch.object(
                    presubmit_pipeline.affected,
                    "write_failure_evidence",
                    failure_writer,
                ),
                contextlib.redirect_stdout(io.StringIO()),
            ):
                status = presubmit_pipeline.main()
            if status != 0:
                raise AssertionError("status")
            resolver.assert_called_once_with("auto", event=event, ref=ref, base_sha=base_sha)
            clean_checkout.assert_called_once_with(
                head,
                event=event,
                runner_temp=runner_temp,
                cache_mode=cache_mode,
                cache_role=cache_role,
            )
            started_loader.assert_called_once_with(started_file, runner_temp=runner_temp)
            if expected_mode == "affected":
                revision.assert_called_once_with(base_sha)
                changed.assert_called_once_with(canonical_base)
            else:
                revision.assert_not_called()
                changed.assert_not_called()
            selector.assert_called_once_with(
                changes,
                mode=expected_mode,
                base_sha=canonical_base,
                event=event,
            )
            executor.assert_called_once_with(
                selection,
                evidence,
                bazelrc_authority=bazelrc_authority,
                job_started_epoch=started_epoch,
            )
            failure_writer.assert_not_called()

        failure_writer = mock.Mock()
        argv = [
            "pipeline.py",
            "--bazel-only",
            "--mode",
            "auto",
            "--base",
            "0" * 40,
            "--event",
            "pull_request",
            "--ref",
            "refs/pull/1/merge",
            "--head",
            head,
            "--evidence-dir",
            str(evidence),
        ]
        with (
            mock.patch.object(sys, "argv", argv),
            mock.patch.object(
                presubmit_pipeline.affected,
                "resolve_selection_mode",
                side_effect=affected.SelectionError(
                    "AFFECTED-SELECT-010", "selection mode conflicts with workflow policy"
                ),
            ),
            mock.patch.object(
                presubmit_pipeline.affected,
                "write_failure_evidence",
                failure_writer,
            ),
            contextlib.redirect_stderr(io.StringIO()),
        ):
            status = presubmit_pipeline.main()
        if status != 2 or failure_writer.call_count != 1:
            raise AssertionError("failure evidence")
    except Exception:
        return [_error("AFFECTED-CODE-006", "presubmit orchestration contract is invalid")]
    return []


def _nightly_orchestration_errors() -> list[str]:
    evidence = Path("/tmp/mindclade-nightly-orchestration")
    runner_temp = Path("/tmp/mindclade-nightly-runner")
    started_file = runner_temp / "bazel-job-started"
    started_epoch = 123
    head = "1" * 40
    contract = nightly_pipeline.NightlyContract(
        mode="full",
        shard_count=4,
        partition_contract="ci/bazel/full_graph_shards.toml",
    )
    bazelrc_authority = mock.Mock()
    try:
        for event, cache_mode, cache_role in (
            ("schedule", "disk", "writer"),
            ("schedule", "remote", "writer"),
            ("workflow_dispatch", "disk", "writer"),
        ):
            selection = mock.Mock(
                analysis_targets=("//...",),
                test_targets=("//...",),
            )
            loader = mock.Mock(return_value=contract)
            resolver = mock.Mock(return_value="full")
            bazelrc_authority = object()
            clean_checkout = mock.Mock(return_value=bazelrc_authority)
            started_loader = mock.Mock(return_value=started_epoch)
            selector = mock.Mock(return_value=selection)
            executor = mock.Mock(return_value=0)
            failure_writer = mock.Mock()
            argv = [
                "pipeline.py",
                "--event",
                event,
                "--ref",
                "refs/heads/main",
                "--head",
                head,
                "--evidence-dir",
                str(evidence),
                "--job-started-at-file",
                str(started_file),
                "--runner-temp",
                str(runner_temp),
                "--cache-mode",
                cache_mode,
                "--cache-role",
                cache_role,
            ]
            with (
                mock.patch.object(sys, "argv", argv),
                mock.patch.object(nightly_pipeline, "load_contract", loader),
                mock.patch.object(
                    nightly_pipeline.affected,
                    "resolve_selection_mode",
                    resolver,
                ),
                mock.patch.object(
                    nightly_pipeline.affected,
                    "assert_clean_checkout",
                    clean_checkout,
                ),
                mock.patch.object(
                    nightly_pipeline.affected,
                    "load_job_started_epoch",
                    started_loader,
                ),
                mock.patch.object(nightly_pipeline.affected, "select", selector),
                mock.patch.object(
                    nightly_pipeline.affected,
                    "execute_selection",
                    executor,
                ),
                mock.patch.object(
                    nightly_pipeline.affected,
                    "write_failure_evidence",
                    failure_writer,
                ),
                contextlib.redirect_stdout(io.StringIO()),
            ):
                status = nightly_pipeline.main()
            if status != 0:
                raise AssertionError("status")
            resolver.assert_called_once_with(
                "full", event=event, ref="refs/heads/main", base_sha=None
            )
            clean_checkout.assert_called_once_with(
                head,
                event=event,
                runner_temp=runner_temp,
                cache_mode=cache_mode,
                cache_role=cache_role,
            )
            started_loader.assert_called_once_with(started_file, runner_temp=runner_temp)
            selector.assert_called_once_with([], mode="full", event=event)
            executor.assert_called_once_with(
                selection,
                evidence,
                bazelrc_authority=bazelrc_authority,
                job_started_epoch=started_epoch,
            )
            failure_writer.assert_not_called()
    except Exception:
        return [_error("AFFECTED-CODE-007", "nightly orchestration contract is invalid")]
    return []


def check(root: Path) -> list[str]:
    errors: list[str] = []
    affected_path = root / "ci/common/affected.py"
    pipeline_path = root / "ci/presubmit/pipeline.py"
    try:
        affected_symbols = _top_level_symbols(affected_path)
        affected_assignments = _top_level_assignments(affected_path)
        affected_imports = _imports(affected_path)
        pipeline_symbols = _top_level_symbols(pipeline_path)
    except (OSError, UnicodeError, SyntaxError):
        return [_error("AFFECTED-CODE-001", "affected-selection source is unreadable")]
    for symbol in (
        "Change",
        "Selection",
        "SelectionError",
        "assert_bazelrc_contract",
        "assert_clean_checkout",
        "bazel_query",
        "execute_selection",
        "git_changed",
        "load_global_input_contract",
        "load_job_started_epoch",
        "resolve_selection_mode",
        "run_git",
        "rust_qualification_required",
        "select",
        "trusted_git_launcher",
        "write_failure_evidence",
    ):
        if symbol not in affected_symbols:
            errors.append(_error("AFFECTED-CODE-002", "affected selector interface is incomplete"))
            break
    if "re" in affected_imports:
        errors.append(_error("AFFECTED-CODE-003", "affected selector uses a forbidden parser"))
    if {"GLOBAL_EXACT_PATHS", "GLOBAL_PREFIXES"} & affected_assignments:
        errors.append(_error("AFFECTED-CODE-004", "selector embeds mutable global inputs"))
    if affected.GRAPH_NATIVE_AFFECTED_ACTIVE is not False:
        errors.append(_error("AFFECTED-CODE-008", "affected workflow activation is premature"))
    if "main" not in pipeline_symbols:
        errors.append(_error("AFFECTED-CODE-005", "presubmit pipeline entry point is missing"))

    contract_path = root / "ci/common/affected_global_inputs.json"
    try:
        contract = load_global_input_contract(contract_path)
        errors.extend(_review_boundary_errors(contract, _tracked_paths(root)))
    except ContractError as error:
        errors.append(str(error))
    errors.extend(_activation_errors(contract_path))

    workflows: dict[str, dict[str, Any]] = {}
    try:
        for path in sorted((root / ".github/workflows").glob("*.y*ml")):
            workflows[path.relative_to(root).as_posix()] = parse_workflow(path)
        presubmit = workflows[".github/workflows/presubmit.yml"]
        errors.extend(_presubmit_workflow_errors(presubmit))
        nightly = workflows[".github/workflows/nightly.yml"]
        errors.extend(_nightly_workflow_errors(nightly))
        errors.extend(_verdict_context_errors(workflows))
    except (KeyError, WorkflowYamlError) as error:
        if isinstance(error, WorkflowYamlError):
            errors.append(str(error))
        else:
            errors.append(_error("AFFECTED-WORKFLOW-001", "workflow source is unreadable"))
    errors.extend(_nightly_target_errors(root / "ci/nightly/targets.yaml"))
    errors.extend(_selection_policy_errors())
    errors.extend(_presubmit_orchestration_errors())
    errors.extend(_nightly_orchestration_errors())
    return errors


def main() -> int:
    errors = check(ROOT)
    for error in errors:
        print(error)
    if errors:
        return 1
    print("affected presubmit contract passed")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
