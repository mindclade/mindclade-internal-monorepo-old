# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

"""Small fail-closed YAML parser for GitHub workflow contract checks.

The static architecture lane intentionally installs no third-party packages. This
parser therefore implements the constrained YAML surface used by the repository's
workflow files: indentation-based mappings and sequences, quoted/plain scalars,
flow sequences, and literal/folded block scalars. Unsupported YAML features fail
closed instead of being interpreted approximately.
"""

from __future__ import annotations

import json
from dataclasses import dataclass
from pathlib import Path
from typing import Any


class WorkflowYamlError(RuntimeError):
    """A workflow could not be parsed into the governed YAML data model."""

    def __init__(self, code: str, message: str) -> None:
        self.code = code
        self.public_message = message
        super().__init__(f"[{code}] {message}")


@dataclass(frozen=True)
class _Line:
    indent: int
    content: str


def _strip_comment(value: str) -> str:
    single_quoted = False
    double_quoted = False
    escaped = False
    for index, character in enumerate(value):
        if escaped:
            escaped = False
            continue
        if character == "\\" and double_quoted:
            escaped = True
            continue
        if character == "'" and not double_quoted:
            single_quoted = not single_quoted
            continue
        if character == '"' and not single_quoted:
            double_quoted = not double_quoted
            continue
        if (
            character == "#"
            and not single_quoted
            and not double_quoted
            and (index == 0 or value[index - 1].isspace())
        ):
            return value[:index].rstrip()
    if single_quoted or double_quoted or escaped:
        raise WorkflowYamlError("AFFECTED-WORKFLOW-001", "workflow YAML quoting is invalid")
    return value.rstrip()


def _tokenize(text: str) -> tuple[_Line, ...]:
    lines: list[_Line] = []
    for raw in text.splitlines():
        if "\t" in raw[: len(raw) - len(raw.lstrip(" \t"))]:
            raise WorkflowYamlError("AFFECTED-WORKFLOW-001", "workflow YAML indentation is invalid")
        indent = len(raw) - len(raw.lstrip(" "))
        content = _strip_comment(raw[indent:])
        if not content or content == "---":
            continue
        if indent % 2:
            raise WorkflowYamlError("AFFECTED-WORKFLOW-001", "workflow YAML indentation is invalid")
        lines.append(_Line(indent=indent, content=content))
    if not lines:
        raise WorkflowYamlError("AFFECTED-WORKFLOW-001", "workflow YAML is empty")
    return tuple(lines)


def _split_mapping(value: str) -> tuple[str, str]:
    if ":" not in value:
        raise WorkflowYamlError("AFFECTED-WORKFLOW-001", "workflow YAML mapping is invalid")
    key, remainder = value.split(":", 1)
    key = key.strip()
    if not key or key.startswith(("&", "*", "!", "'", '"')):
        raise WorkflowYamlError("AFFECTED-WORKFLOW-001", "workflow YAML key is invalid")
    return key, remainder.strip()


def _flow_items(value: str) -> list[str]:
    items: list[str] = []
    current: list[str] = []
    single_quoted = False
    double_quoted = False
    escaped = False
    for character in value:
        if escaped:
            current.append(character)
            escaped = False
            continue
        if character == "\\" and double_quoted:
            current.append(character)
            escaped = True
            continue
        if character == "'" and not double_quoted:
            single_quoted = not single_quoted
        elif character == '"' and not single_quoted:
            double_quoted = not double_quoted
        if character == "," and not single_quoted and not double_quoted:
            items.append("".join(current).strip())
            current = []
        else:
            current.append(character)
    if single_quoted or double_quoted or escaped:
        raise WorkflowYamlError("AFFECTED-WORKFLOW-001", "workflow YAML flow value is invalid")
    tail = "".join(current).strip()
    if tail:
        items.append(tail)
    return items


def _parse_scalar(value: str) -> Any:
    if value.startswith(("&", "*", "!")):
        raise WorkflowYamlError(
            "AFFECTED-WORKFLOW-001", "workflow YAML aliases and tags are forbidden"
        )
    if value.startswith("["):
        if not value.endswith("]"):
            raise WorkflowYamlError("AFFECTED-WORKFLOW-001", "workflow YAML sequence is invalid")
        inner = value[1:-1].strip()
        return [] if not inner else [_parse_scalar(item) for item in _flow_items(inner)]
    if value.startswith("{"):
        if value != "{}":
            raise WorkflowYamlError(
                "AFFECTED-WORKFLOW-001", "workflow YAML flow mappings are unsupported"
            )
        return {}
    if value.startswith('"'):
        try:
            parsed = json.loads(value)
        except json.JSONDecodeError as error:
            raise WorkflowYamlError(
                "AFFECTED-WORKFLOW-001", "workflow YAML scalar is invalid"
            ) from error
        if not isinstance(parsed, str):
            raise WorkflowYamlError("AFFECTED-WORKFLOW-001", "workflow YAML scalar is invalid")
        return parsed
    if value.startswith("'"):
        if len(value) < 2 or not value.endswith("'"):
            raise WorkflowYamlError("AFFECTED-WORKFLOW-001", "workflow YAML scalar is invalid")
        return value[1:-1].replace("''", "'")
    lowered = value.lower()
    if lowered in {"true", "false"}:
        return lowered == "true"
    if lowered in {"null", "~"}:
        return None
    if value.isdecimal():
        return int(value)
    return value


class _Parser:
    def __init__(self, lines: tuple[_Line, ...]) -> None:
        self.lines = lines

    def parse(self) -> dict[str, Any]:
        value, index = self._block(0, self.lines[0].indent)
        if index != len(self.lines) or not isinstance(value, dict) or self.lines[0].indent != 0:
            raise WorkflowYamlError("AFFECTED-WORKFLOW-001", "workflow YAML root is invalid")
        return value

    def _block(self, index: int, indent: int) -> tuple[Any, int]:
        if index >= len(self.lines) or self.lines[index].indent != indent:
            raise WorkflowYamlError("AFFECTED-WORKFLOW-001", "workflow YAML nesting is invalid")
        if self.lines[index].content.startswith("- ") or self.lines[index].content == "-":
            return self._sequence(index, indent)
        return self._mapping(index, indent)

    def _mapping(
        self, index: int, indent: int, initial: dict[str, Any] | None = None
    ) -> tuple[dict[str, Any], int]:
        result = {} if initial is None else initial
        while index < len(self.lines):
            line = self.lines[index]
            if line.indent < indent:
                break
            if line.indent != indent or line.content.startswith("-"):
                break
            key, remainder = _split_mapping(line.content)
            if key in result:
                raise WorkflowYamlError(
                    "AFFECTED-WORKFLOW-002", "workflow YAML contains a duplicate key"
                )
            if remainder in {"|", "|-", "|+", ">", ">-", ">+"}:
                value, index = self._block_scalar(index + 1, indent, remainder)
                result[key] = value
                continue
            if remainder:
                result[key] = _parse_scalar(remainder)
                index += 1
                continue
            index += 1
            if index < len(self.lines) and self.lines[index].indent > indent:
                result[key], index = self._block(index, self.lines[index].indent)
            else:
                result[key] = {}
        return result, index

    def _sequence(self, index: int, indent: int) -> tuple[list[Any], int]:
        result: list[Any] = []
        while index < len(self.lines):
            line = self.lines[index]
            if line.indent < indent:
                break
            if line.indent != indent or not (line.content.startswith("- ") or line.content == "-"):
                break
            remainder = line.content[1:].strip()
            index += 1
            if not remainder:
                if index >= len(self.lines) or self.lines[index].indent <= indent:
                    raise WorkflowYamlError(
                        "AFFECTED-WORKFLOW-001", "workflow YAML sequence item is empty"
                    )
                value, index = self._block(index, self.lines[index].indent)
                result.append(value)
                continue
            if ":" not in remainder:
                result.append(_parse_scalar(remainder))
                continue
            key, value_text = _split_mapping(remainder)
            item: dict[str, Any] = {}
            if value_text in {"|", "|-", "|+", ">", ">-", ">+"}:
                item[key], index = self._block_scalar(index, indent, value_text)
            elif value_text:
                item[key] = _parse_scalar(value_text)
            elif index < len(self.lines) and self.lines[index].indent > indent:
                item[key], index = self._block(index, self.lines[index].indent)
            else:
                item[key] = {}
            if index < len(self.lines) and self.lines[index].indent > indent:
                continuation_indent = self.lines[index].indent
                continuation, index = self._mapping(index, continuation_indent, item)
                item = continuation
            result.append(item)
        return result, index

    def _block_scalar(self, index: int, parent_indent: int, style: str) -> tuple[str, int]:
        values: list[str] = []
        while index < len(self.lines) and self.lines[index].indent > parent_indent:
            values.append(self.lines[index].content)
            index += 1
        if not values:
            return "", index
        separator = " " if style.startswith(">") else "\n"
        value = separator.join(values)
        if style.endswith("-"):
            return value, index
        return value + "\n", index


def parse_workflow_text(text: str) -> dict[str, Any]:
    """Parse supported GitHub workflow YAML into ordinary Python data."""

    return _Parser(_tokenize(text)).parse()


def parse_workflow(path: Path) -> dict[str, Any]:
    """Read and parse a workflow without exposing filesystem details on failure."""

    try:
        if path.is_symlink():
            raise OSError("symbolic link")
        text = path.read_text(encoding="utf-8")
    except (OSError, UnicodeError) as error:
        raise WorkflowYamlError("AFFECTED-WORKFLOW-001", "workflow YAML is unreadable") from error
    return parse_workflow_text(text)
