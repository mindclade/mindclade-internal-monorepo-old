#!/usr/bin/env python3
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

"""Reject Go test files that provide no executable failure signal."""

from __future__ import annotations

import os
from collections.abc import Iterator
from pathlib import Path

SIGNAL_METHODS = frozenset({"Error", "Errorf", "Fail", "FailNow", "Fatal", "Fatalf"})
IGNORED_PARTS = frozenset(
    {
        ".claude",
        ".codex-worktrees",
        ".git",
        ".terraform",
        "bazel-bin",
        "bazel-out",
        "bazel-testlogs",
        "node_modules",
    }
)
ERROR_CODE = "GO_TEST_SIGNAL_MISSING"


def _tokens(source: str) -> Iterator[str]:
    index = 0
    length = len(source)
    while index < length:
        char = source[index]
        if char.isspace():
            index += 1
            continue
        if source.startswith("//", index):
            newline = source.find("\n", index + 2)
            index = length if newline < 0 else newline + 1
            continue
        if source.startswith("/*", index):
            close = source.find("*/", index + 2)
            index = length if close < 0 else close + 2
            continue
        if char in {'"', "'", "`"}:
            delimiter = char
            index += 1
            while index < length:
                if delimiter != "`" and source[index] == "\\":
                    index += 2
                    continue
                if source[index] == delimiter:
                    index += 1
                    break
                index += 1
            continue
        if char == "_" or char.isalpha():
            end = index + 1
            while end < length and (source[end] == "_" or source[end].isalnum()):
                end += 1
            yield source[index:end]
            index = end
            continue
        yield char
        index += 1


def has_test_signal(source: str) -> bool:
    tokens = tuple(_tokens(source))
    return any(
        tokens[index] == "." and tokens[index + 1] in SIGNAL_METHODS and tokens[index + 2] == "("
        for index in range(len(tokens) - 2)
    )


def _ignored(path: Path) -> bool:
    return any(part in IGNORED_PARTS or part.startswith("bazel-") for part in path.parts)


def check(root: Path) -> list[str]:
    errors: list[str] = []
    for path in sorted(root.rglob("*_test.go")):
        relative = path.relative_to(root)
        if _ignored(relative):
            continue
        try:
            source = path.read_text(encoding="utf-8")
        except OSError as error:
            errors.append(f"GO_TEST_SIGNAL_UNREADABLE {relative.as_posix()}: {error}")
            continue
        if not has_test_signal(source):
            errors.append(f"{ERROR_CODE} {relative.as_posix()}")
    return errors


def main() -> int:
    root = Path(os.environ.get("BUILD_WORKSPACE_DIRECTORY", Path(__file__).parents[2])).resolve()
    errors = check(root)
    for error in errors:
        print(error)
    if errors:
        return 1
    print("Go test signal contract passed")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
