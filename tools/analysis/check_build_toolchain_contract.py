#!/usr/bin/env python3
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

from __future__ import annotations

import argparse
import ast
import json
import re
import tomllib
from pathlib import Path

FORBIDDEN = [
    re.compile(r"\bpip\s+install\b"),
    re.compile(r"\bnpm\s+install\b"),
    re.compile(r"\bcargo\s+fetch\b"),
    re.compile(r"curl\b[^\n|]*\|\s*(?:bash|sh)\b"),
    re.compile(r"(?<![\w.-])/usr/bin/(?!env\b)"),
]
SCAN = {".bzl", ".bazel", ".sh", ".py", ".yml", ".yaml", ".nix"}
REQUIRED_REPO_IGNORES = frozenset(
    {
        "**/.codex-worktrees",
        "**/.mypy_cache",
        "**/.pytest_cache",
        "**/.ruff_cache",
        "**/.terraform",
        "**/__pycache__",
        "**/node_modules",
    }
)
BOOTSTRAP_SYNTAX_TARGET = "py313"
PYTEST_MACRO = "tools/build/pytest.bzl"
ROOT_PYPI_HUB = "pypi"
ROOT_PYPI_INDEX = "https://pypi.org/simple"
ROOT_TORCH_INDEX = "https://download.pytorch.org/whl/cpu"
ROOT_PYPI_PLATFORMS = frozenset({"linux_aarch64", "linux_x86_64", "osx_aarch64"})
ROOT_PYPI_REQUIREMENTS = {
    "//:requirements.darwin.lock.txt": "osx_aarch64",
    "//:requirements.lock.txt": "linux_*",
}
PYTHON_PLATFORM_LOCKS = {
    "requirements.lock.txt": ("linux", "linux_*", "+cpu"),
    "requirements.darwin.lock.txt": ("aarch64-apple-darwin", "osx_aarch64", ""),
}
PYTHON_TOOLCHAIN_MANIFEST = "tools/build/nix/toolchain-manifest.json"
PYTHON_STANDALONE_PLATFORMS = frozenset(
    {
        "aarch64-apple-darwin",
        "aarch64-unknown-linux-gnu",
        "x86_64-unknown-linux-gnu",
    }
)
PYTHON_STANDALONE_ORIGIN = "https://github.com/astral-sh/python-build-standalone/releases/download/"

# Files that ENFORCE this same contract, and therefore contain the forbidden strings as
# pattern literals rather than as commands.
#
# This file has always exempted itself for that reason — see the `p.resolve() ==
# Path(__file__).resolve()` test below. The exemption was written when this was the only
# implementation; it is not any more. tools/build/nix/checks/no-host-tools.nix enforces the
# same ADR-0002 clause ("CI rejects host-tool leakage") for `nix flake check`, which runs where
# Python does not, and it names /usr/bin and `pip install` for the same reason this file does.
#
# Without the entry the two checks are mutually exclusive: adding the Nix one turns this one
# red, and the only way to a green run is to delete a control.
#
# Scoped to exact paths, not to the directory. tools/build/nix/checks/ holds other checks that
# have no business naming a host path, and skipping all of them to fix one would be trading a
# false positive for a blind spot. What is safe to exempt is a file that DEFINES patterns and
# runs no build action; a new one has to be added here deliberately.
CONTRACT_IMPLEMENTATIONS = frozenset(
    {
        "tools/analysis/verify_cc_toolchain_selection.py",
        "tools/build/nix/checks/no-host-tools.nix",
    }
)


def rust_version_contract(root: Path) -> list[str]:
    errors = []
    cargo = tomllib.loads((root / "Cargo.toml").read_text())
    expected = cargo.get("workspace", {}).get("package", {}).get("rust-version")
    nix = (root / "tools/build/nix/versions.nix").read_text(errors="replace")
    flake = (root / "flake.nix").read_text(errors="replace")
    module = (root / "MODULE.bazel").read_text(errors="replace")
    qualify = (
        (root / "tools/qualification/rust/common.py").read_text(errors="replace")
        if (root / "tools/qualification/rust/common.py").exists()
        else ""
    )
    if not expected:
        errors.append("Cargo workspace rust-version is missing")
        return errors
    if f'rust = "{expected}"' not in nix:
        errors.append("Nix Rust version does not match Cargo rust-version")
    if "rust-overlay" not in flake or "toolchains/rust.nix" not in flake:
        errors.append("flake.nix must construct the pinned Rust toolchain through rust-overlay")
    if expected not in qualify:
        errors.append("Rust qualification expected-version does not match Cargo rust-version")

    extension = re.search(
        r"(?P<name>[A-Za-z_][A-Za-z0-9_]*)\s*=\s*use_extension\(\s*"
        r'"@rules_rust//rust:extensions\.bzl"\s*,\s*"rust"\s*\)',
        module,
    )
    if extension is None:
        errors.append("MODULE.bazel must configure the root rules_rust toolchain extension")
        return errors

    extension_name = extension.group("name")
    toolchain = re.search(
        rf"\b{re.escape(extension_name)}\.toolchain\s*\((?P<body>.*?)\n\)",
        module,
        re.DOTALL,
    )
    if toolchain is None:
        errors.append("MODULE.bazel must declare the root rules_rust toolchain")
        return errors

    body = toolchain.group("body")
    if not re.search(
        rf'versions\s*=\s*\[\s*"{re.escape(expected)}"\s*,?\s*\]',
        body,
    ):
        errors.append("Bazel rules_rust version does not match Cargo rust-version")
    if not re.search(r'edition\s*=\s*"2024"', body):
        errors.append("Bazel rules_rust default edition must be 2024")
    if not re.search(
        rf'use_repo\(\s*{re.escape(extension_name)}\s*,\s*"rust_toolchains"\s*,?\s*\)',
        module,
    ):
        errors.append("MODULE.bazel must import the pinned rules_rust toolchain repository")
    if not re.search(
        r'register_toolchains\(\s*"@rust_toolchains//:all"\s*,?\s*\)',
        module,
    ):
        errors.append("MODULE.bazel must register the pinned rules_rust toolchains")
    return errors


def repository_traversal_contract(root: Path) -> list[str]:
    repo_policy = root / "REPO.bazel"
    if not repo_policy.is_file():
        return ["REPO.bazel is required for globbed generated-directory ignores"]

    text = repo_policy.read_text(errors="replace")
    errors = []
    if "ignore_directories(" not in text:
        errors.append("REPO.bazel must declare ignore_directories()")
    for pattern in sorted(REQUIRED_REPO_IGNORES):
        if f'"{pattern}"' not in text and f"'{pattern}'" not in text:
            errors.append(f"REPO.bazel must ignore generated directory pattern {pattern}")
    return errors


def python_repository_resolution_contract(root: Path) -> list[str]:
    """Keep Bazel's hashed Python lock target-aware and wheel-only.

    An unconfigured ``bazel query`` traverses every PEP 508 marker fork. If rules_python falls
    back to host pip, Linux attempts to resolve the Darwin Torch fork and macOS attempts the
    Linux CPU fork. Parse MODULE.bazel structurally so comments or dead strings cannot satisfy
    the control.
    """

    path = root / "MODULE.bazel"
    if not path.is_file():
        return ["MODULE.bazel is required for Python repository governance"]
    try:
        module = ast.parse(path.read_text(errors="replace"), filename="MODULE.bazel")
    except SyntaxError as error:
        return [f"MODULE.bazel is not parseable for Python repository governance: {error.msg}"]

    extensions = {
        target.id
        for node in module.body
        if isinstance(node, ast.Assign)
        and len(node.targets) == 1
        and isinstance((target := node.targets[0]), ast.Name)
        and isinstance(node.value, ast.Call)
        and isinstance(node.value.func, ast.Name)
        and node.value.func.id == "use_extension"
        and len(node.value.args) >= 2
        and isinstance(node.value.args[0], ast.Constant)
        and node.value.args[0].value == "@rules_python//python/extensions:pip.bzl"
        and isinstance(node.value.args[1], ast.Constant)
        and node.value.args[1].value == "pip"
    }
    parse_calls = [
        node
        for node in ast.walk(module)
        if isinstance(node, ast.Call)
        and isinstance(node.func, ast.Attribute)
        and node.func.attr == "parse"
        and isinstance(node.func.value, ast.Name)
        and node.func.value.id in extensions
        and any(
            keyword.arg == "hub_name"
            and isinstance(keyword.value, ast.Constant)
            and keyword.value.value == ROOT_PYPI_HUB
            for keyword in node.keywords
        )
    ]
    if len(parse_calls) != 1:
        return ["MODULE.bazel must declare exactly one root pypi pip.parse repository"]

    kwargs = {keyword.arg: keyword.value for keyword in parse_calls[0].keywords if keyword.arg}

    def literal(name: str):
        value = kwargs.get(name)
        if value is None:
            return None
        try:
            return ast.literal_eval(value)
        except (ValueError, TypeError):
            return None

    errors = []
    if literal("requirements_by_platform") != ROOT_PYPI_REQUIREMENTS:
        errors.append("root pypi repository must consume the exact platform-specific locks")
    if literal("download_only") is not True:
        errors.append("root pypi repository must be wheel-only")
    if literal("experimental_index_url") != ROOT_PYPI_INDEX:
        errors.append("root pypi repository must use the canonical PyPI simple index")
    if literal("experimental_index_url_overrides") != {"torch": ROOT_TORCH_INDEX}:
        errors.append("root pypi repository must route Torch exclusively to the CPU index")
    platforms = literal("experimental_target_platforms")
    if not isinstance(platforms, (list, tuple)) or set(platforms) != ROOT_PYPI_PLATFORMS:
        errors.append("root pypi repository must declare the supported Linux and Apple targets")
    return errors


def python_toolchain_version_contract(root: Path) -> list[str]:
    """Keep Bazel on the exact patched Python recorded by the Nix toolchain evidence."""

    manifest_path = root / PYTHON_TOOLCHAIN_MANIFEST
    if not manifest_path.is_file():
        return [f"{PYTHON_TOOLCHAIN_MANIFEST} is required for Python toolchain governance"]
    try:
        manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
        expected = manifest["tools"]["python"]
    except (json.JSONDecodeError, KeyError, TypeError) as error:
        return [f"{PYTHON_TOOLCHAIN_MANIFEST} has no valid tools.python version: {error}"]
    if not isinstance(expected, str) or re.fullmatch(r"[0-9]+\.[0-9]+\.[0-9]+", expected) is None:
        return [f"{PYTHON_TOOLCHAIN_MANIFEST} tools.python must be an X.Y.Z version"]
    minor = expected.rsplit(".", 1)[0]

    module_path = root / "MODULE.bazel"
    if not module_path.is_file():
        return ["MODULE.bazel is required for Python toolchain governance"]
    try:
        module = ast.parse(module_path.read_text(errors="replace"), filename="MODULE.bazel")
    except SyntaxError as error:
        return [f"MODULE.bazel is not parseable for Python toolchain governance: {error.msg}"]

    extensions = {
        target.id
        for node in module.body
        if isinstance(node, ast.Assign)
        and len(node.targets) == 1
        and isinstance((target := node.targets[0]), ast.Name)
        and isinstance(node.value, ast.Call)
        and isinstance(node.value.func, ast.Name)
        and node.value.func.id == "use_extension"
        and len(node.value.args) >= 2
        and isinstance(node.value.args[0], ast.Constant)
        and node.value.args[0].value == "@rules_python//python/extensions:python.bzl"
        and isinstance(node.value.args[1], ast.Constant)
        and node.value.args[1].value == "python"
    }
    if len(extensions) != 1:
        return ["MODULE.bazel must declare exactly one root rules_python toolchain extension"]
    extension = next(iter(extensions))

    def calls(name: str) -> list[ast.Call]:
        return [
            node
            for node in ast.walk(module)
            if isinstance(node, ast.Call)
            and isinstance(node.func, ast.Attribute)
            and node.func.attr == name
            and isinstance(node.func.value, ast.Name)
            and node.func.value.id == extension
        ]

    def kwargs(call: ast.Call) -> dict[str, object]:
        result: dict[str, object] = {}
        for keyword in call.keywords:
            if keyword.arg is None:
                continue
            try:
                result[keyword.arg] = ast.literal_eval(keyword.value)
            except (ValueError, TypeError):
                result[keyword.arg] = None
        return result

    errors: list[str] = []
    runtime_overrides = [
        values
        for call in calls("single_version_override")
        if (values := kwargs(call)).get("python_version") == expected
    ]
    if len(runtime_overrides) != 1:
        errors.append(f"Bazel must declare exactly one Python {expected} standalone runtime")
    else:
        runtime = runtime_overrides[0]
        checksums = runtime.get("sha256")
        if not isinstance(checksums, dict) or set(checksums) != PYTHON_STANDALONE_PLATFORMS:
            errors.append("Bazel Python runtime must checksum every supported host platform")
        elif any(
            not isinstance(digest, str) or re.fullmatch(r"[0-9a-f]{64}", digest) is None
            for digest in checksums.values()
        ):
            errors.append("Bazel Python runtime checksums must be lowercase SHA-256 digests")
        urls = runtime.get("urls")
        valid_url = (
            isinstance(urls, list)
            and len(urls) == 1
            and isinstance(urls[0], str)
            and urls[0].startswith(PYTHON_STANDALONE_ORIGIN)
            and "{python_version}" in urls[0]
            and "{platform}" in urls[0]
            and urls[0].endswith("-install_only_stripped.tar.gz")
        )
        if not valid_url:
            errors.append("Bazel Python runtime must use the pinned upstream standalone archive")

    mappings = [kwargs(call).get("minor_mapping") for call in calls("override")]
    if mappings != [{minor: expected}]:
        errors.append(f"Bazel Python {minor} must resolve to the Nix patch version {expected}")

    defaults = [kwargs(call) for call in calls("toolchain")]
    if len(defaults) != 1 or defaults[0] != {"is_default": True, "python_version": minor}:
        errors.append(f"Bazel must register Python {minor} as its only default toolchain")
    return errors


def python_bootstrap_syntax_contract(root: Path) -> list[str]:
    """Keep formatter output parseable before Nix establishes Python 3.14."""

    path = root / "pyproject.toml"
    if not path.is_file():
        return ["pyproject.toml is required for Python syntax governance"]
    try:
        document = tomllib.loads(path.read_text(encoding="utf-8"))
        target = document["tool"]["ruff"]["target-version"]
    except (OSError, tomllib.TOMLDecodeError, KeyError, TypeError) as error:
        return [f"pyproject.toml has no valid Ruff target-version: {type(error).__name__}"]
    if target != BOOTSTRAP_SYNTAX_TARGET:
        return [
            "Ruff must preserve Python 3.13 syntax for pre-Nix repository checks; "
            f"expected {BOOTSTRAP_SYNTAX_TARGET}, got {target!r}"
        ]
    return []


def pytest_init_contract(root: Path) -> list[str]:
    """Keep pytest runfiles source-authoritative.

    rules_python's legacy initializer synthesis can create empty ``__init__.py`` files at
    first-party runfiles paths. In a persistent runfiles tree, one of those paths can still be
    a source symlink from an earlier action, so materializing the empty file can truncate the
    tracked source. The shared pytest macro must therefore default synthesis off and pass that
    decision explicitly to ``py_test``.

    Parse the current Starlark as Python syntax instead of looking for strings: the macro is
    deliberately Python-compatible, and structural checks cannot be satisfied by a comment or
    docstring that merely describes the safe setting.
    """

    path = root / PYTEST_MACRO
    if not path.is_file():
        return [f"{PYTEST_MACRO} is required for pytest package-initializer governance"]

    try:
        module = ast.parse(path.read_text(errors="replace"), filename=PYTEST_MACRO)
    except SyntaxError as error:
        return [f"{PYTEST_MACRO} is not parseable for package-initializer governance: {error.msg}"]

    definitions = [
        node
        for node in module.body
        if isinstance(node, (ast.FunctionDef, ast.AsyncFunctionDef)) and node.name == "pytest_test"
    ]
    if len(definitions) != 1:
        return [f"{PYTEST_MACRO} must define exactly one top-level pytest_test macro"]

    macro = definitions[0]
    positional = [*macro.args.posonlyargs, *macro.args.args]
    defaults = {
        argument.arg: default
        for argument, default in zip(
            positional[-len(macro.args.defaults) :], macro.args.defaults, strict=True
        )
    }
    initializer_default = defaults.get("legacy_create_init")
    errors = []
    if not (isinstance(initializer_default, ast.Constant) and initializer_default.value is False):
        errors.append(f"{PYTEST_MACRO}: pytest_test must default legacy_create_init to False")

    py_test_calls = [
        node
        for node in ast.walk(macro)
        if isinstance(node, ast.Call)
        and isinstance(node.func, ast.Name)
        and node.func.id == "py_test"
    ]
    forwards_initializer = False
    if len(py_test_calls) == 1:
        initializer_keywords = [
            keyword.value
            for keyword in py_test_calls[0].keywords
            if keyword.arg == "legacy_create_init"
        ]
        forwards_initializer = (
            len(initializer_keywords) == 1
            and isinstance(initializer_keywords[0], ast.Name)
            and initializer_keywords[0].id == "legacy_create_init"
        )
    if not forwards_initializer:
        errors.append(
            f"{PYTEST_MACRO}: pytest_test must forward legacy_create_init explicitly to py_test"
        )
    return errors


def python_platform_lock_contract(root: Path) -> list[str]:
    """Keep rules_python from merging incompatible Torch local versions.

    The CPU index uses a local-version suffix on Linux but not on macOS. A universal
    requirements file consequently has two normalized ``torch`` entries, and rules_python can
    combine one platform's requirement with the other platform's hashes. Platform-specific
    generated locks make the repository rule input unambiguous.
    """

    module_path = root / "MODULE.bazel"
    if not module_path.is_file():
        return ["MODULE.bazel is required for Python platform-lock governance"]
    module = module_path.read_text(errors="replace")
    errors = []
    for relative, (platform, bazel_platform, local_suffix) in PYTHON_PLATFORM_LOCKS.items():
        mapping = f'"//:{relative}": "{bazel_platform}"'
        if mapping not in module:
            errors.append(f"MODULE.bazel must map {relative} to its rules_python target platform")

        path = root / relative
        if not path.is_file():
            errors.append(f"{relative} is required for rules_python platform resolution")
            continue
        text = path.read_text(errors="replace")
        if f"--python-platform {platform}" not in text.partition("\n\n")[0]:
            errors.append(f"{relative} must record its uv {platform} generation command")
        if "--index-url" in text or "--extra-index-url" in text:
            errors.append(f"{relative} must not expose package indexes to every requirement")

        torch_lines = re.findall(r"(?m)^torch==([^\\\s;]+)([^\n]*)$", text)
        if len(torch_lines) != 1:
            errors.append(f"{relative} must contain exactly one unambiguous torch requirement")
            continue
        version, tail = torch_lines[0]
        if ";" in tail:
            errors.append(f"{relative} torch requirement must not carry a platform marker")
        if local_suffix and not version.endswith(local_suffix):
            errors.append(f"{relative} must select the Linux Torch CPU local version")
        if not local_suffix and "+" in version:
            errors.append(f"{relative} must select the Darwin Torch version without a local suffix")
    return errors


def check(root: Path):
    errors = []
    if (root / "WORKSPACE").exists() or (root / "WORKSPACE.bazel").exists():
        errors.append("legacy WORKSPACE is forbidden; Bzlmod owns Bazel dependencies")
    errors.extend(rust_version_contract(root))
    errors.extend(repository_traversal_contract(root))
    errors.extend(python_toolchain_version_contract(root))
    errors.extend(python_bootstrap_syntax_contract(root))
    errors.extend(python_repository_resolution_contract(root))
    errors.extend(pytest_init_contract(root))
    errors.extend(python_platform_lock_contract(root))
    for p in root.rglob("*"):
        relative = p.relative_to(root)
        if (
            not p.is_file()
            or p.resolve() == Path(__file__).resolve()
            or relative.as_posix() in CONTRACT_IMPLEMENTATIONS
            # Build and tool output, not source. The list previously stopped at node_modules,
            # which was complete when only JS had a vendor directory. It now misses .venv in
            # particular: `uv sync` writes one, so running this check after the Python lane
            # scanned pip invocations inside site-packages and reported ruff's own __main__.py
            # as a forbidden host package-manager call.
            #
            # Matched against the path relative to root, for the reason spelled out below the
            # list. Matching absolute parts also tested the repository's own location, so a
            # checkout that itself lived under a directory named .claude -- which is exactly
            # where agent worktrees are created -- excluded every file in the tree and this
            # whole-tree scan silently inspected nothing at all. A gate that reports no
            # findings because it saw no files is worse than one that fails loudly.
            or any(
                x in relative.parts
                for x in (
                    ".git",
                    "node_modules",
                    # Agent worktrees: full COPIES of this repository, so every checker that
                    # rglobs the tree finds a second (third, twelfth) set of every file and
                    # reports each one. They are ephemeral and not part of the source.
                    ".claude",
                    ".codex-worktrees",
                    "__pycache__",
                    ".venv",
                    "target",
                    ".ruff_cache",
                    ".pytest_cache",
                    ".mypy_cache",
                )
            )
            # Relative to the repository, not absolute: `root.rglob` yields absolute paths, so
            # p.parts[0] is "/" and this never matched. The convenience symlinks Bazel writes
            # (bazel-out, bazel-bin, bazel-<workspace>) only ever sit at the root.
            or relative.parts[0].startswith("bazel-")
        ):
            continue
        if p.name == "Dockerfile" or p.name.startswith("Dockerfile."):
            text = p.read_text(errors="replace")
            if "MINDCLADE_DEV_ONLY=1" not in text:
                errors.append(
                    f"{relative}: production Dockerfiles are forbidden; Bazel OCI owns images"
                )
        if p.suffix in SCAN or p.name in {"BUILD", "BUILD.bazel"}:
            text = p.read_text(errors="replace")
            for rx in FORBIDDEN:
                if rx.search(text):
                    errors.append(
                        f"{relative}: forbidden host/package-manager pattern: {rx.pattern}"
                    )
    return sorted(set(errors))


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--repo", type=Path, default=Path(__file__).resolve().parents[2])
    a = ap.parse_args()
    e = check(a.repo.resolve())
    [print(x) for x in e]
    print(
        "Bazel/Nix ownership check passed"
        if not e
        else f"Bazel/Nix ownership check failed: {len(e)}"
    )
    return 1 if e else 0


if __name__ == "__main__":
    raise SystemExit(main())
