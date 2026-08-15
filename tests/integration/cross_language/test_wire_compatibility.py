# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Release-blocking source/wire field compatibility for runtime v1.

The baseline freezes message names, field tags, field names, labels, and declared
protobuf types. A field may be added with a new tag, but an existing tag may not
be removed, renamed, relabeled, or retyped without an explicit version bump.
Nested enum/oneof declarations are excluded from the enclosing message field
set and parsed independently where relevant.
"""

from __future__ import annotations

import json
import re
from pathlib import Path

ROOT = Path(__file__).resolve().parents[3]
BASELINE_PATH = ROOT / "protocols/compatibility/runtime_v1_fields.json"
BASELINE = json.loads(BASELINE_PATH.read_text())
PROTO_ROOT = ROOT / "protocols/proto/mindclade/runtime/v1"
MESSAGE_START = re.compile(r"\bmessage\s+(\w+)\s*\{")
FIELD = re.compile(r"^\s*(?:(repeated|optional)\s+)?([.\w<>]+)\s+(\w+)\s*=\s*(\d+)\s*$")


def message_bodies(text: str) -> list[tuple[str, str]]:
    output: list[tuple[str, str]] = []
    cursor = 0
    while match := MESSAGE_START.search(text, cursor):
        name = match.group(1)
        opening = text.find("{", match.start())
        depth = 1
        index = opening + 1
        while index < len(text) and depth:
            if text[index] == "{":
                depth += 1
            elif text[index] == "}":
                depth -= 1
            index += 1
        if depth != 0:
            raise AssertionError(f"unbalanced message braces for {name}")
        output.append((name, text[opening + 1 : index - 1]))
        cursor = index
    return output


def top_level_statements(body: str) -> list[str]:
    statements: list[str] = []
    current: list[str] = []
    depth = 0
    for character in body:
        if character == "{":
            depth += 1
            continue
        if character == "}":
            depth -= 1
            continue
        if depth != 0:
            continue
        if character == ";":
            statement = "".join(current).strip()
            if statement:
                statements.append(statement)
            current.clear()
        else:
            current.append(character)
    return statements


def current_fields() -> dict[str, dict[str, dict[str, str]]]:
    messages: dict[str, dict[str, dict[str, str]]] = {}
    for path in sorted(PROTO_ROOT.glob("*.proto")):
        text = re.sub(r"//.*", "", path.read_text())
        package = re.search(r"package\s+([\w.]+)\s*;", text)
        if not package:
            continue
        for name, body in message_bodies(text):
            fields: dict[str, dict[str, str]] = {}
            for statement in top_level_statements(body):
                match = FIELD.match(statement)
                if not match:
                    continue
                label, declared_type, field_name, number = match.groups()
                fields[number] = {
                    "name": field_name,
                    "type": declared_type,
                    "label": label or "singular",
                }
            messages[f"{package.group(1)}.{name}"] = fields
    return messages


def test_runtime_v1_is_backward_compatible() -> None:
    current = current_fields()
    for message, fields in BASELINE.items():
        assert message in current, f"removed message: {message}"
        for number, expected in fields.items():
            assert number in current[message], f"removed field tag {number} from {message}"
            assert current[message][number] == expected, (
                f"incompatible field change in {message} tag {number}: "
                f"expected {expected!r}, got {current[message][number]!r}"
            )


if __name__ == "__main__":
    print(json.dumps(current_fields(), indent=2, sort_keys=True))
