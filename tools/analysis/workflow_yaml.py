# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

"""Fail-closed GitHub workflow parsing for architecture contract checks."""

from __future__ import annotations

import copy
import re
from pathlib import Path
from typing import Any

import yaml
from yaml.nodes import MappingNode, ScalarNode
from yaml.tokens import AliasToken, AnchorToken, TagToken


class WorkflowYamlError(RuntimeError):
    """A workflow could not be parsed into the governed YAML data model."""

    def __init__(self, code: str, message: str) -> None:
        self.code = code
        self.public_message = message
        super().__init__(f"[{code}] {message}")


class _WorkflowLoader(yaml.SafeLoader):
    pass


_WorkflowLoader.yaml_implicit_resolvers = copy.deepcopy(yaml.SafeLoader.yaml_implicit_resolvers)
for first_character, resolvers in tuple(_WorkflowLoader.yaml_implicit_resolvers.items()):
    _WorkflowLoader.yaml_implicit_resolvers[first_character] = [
        (tag, resolver) for tag, resolver in resolvers if tag != "tag:yaml.org,2002:bool"
    ]
_WorkflowLoader.add_implicit_resolver(
    "tag:yaml.org,2002:bool",
    re.compile(r"^(?:true|True|TRUE|false|False|FALSE)$"),
    list("tTfF"),
)


def _construct_mapping(
    loader: _WorkflowLoader, node: MappingNode, deep: bool = False
) -> dict[str, Any]:
    if not isinstance(node, MappingNode):
        raise WorkflowYamlError("AFFECTED-WORKFLOW-001", "workflow YAML mapping is invalid")
    result: dict[str, Any] = {}
    for key_node, value_node in node.value:
        key = loader.construct_object(key_node, deep=deep)
        if not isinstance(key, str) or not key or key == "<<":
            raise WorkflowYamlError("AFFECTED-WORKFLOW-001", "workflow YAML key is invalid")
        if key in result:
            raise WorkflowYamlError(
                "AFFECTED-WORKFLOW-002", "workflow YAML contains a duplicate key"
            )
        result[key] = loader.construct_object(value_node, deep=deep)
    return result


def _construct_null(loader: _WorkflowLoader, node: ScalarNode) -> object:
    if isinstance(node, ScalarNode) and node.value == "":
        return {}
    return None


_WorkflowLoader.add_constructor("tag:yaml.org,2002:map", _construct_mapping)
_WorkflowLoader.add_constructor("tag:yaml.org,2002:null", _construct_null)


def _reject_indirection(text: str) -> None:
    try:
        tokens = yaml.scan(text, Loader=_WorkflowLoader)
        if any(isinstance(token, (AliasToken, AnchorToken, TagToken)) for token in tokens):
            raise WorkflowYamlError(
                "AFFECTED-WORKFLOW-001", "workflow YAML aliases and tags are forbidden"
            )
    except WorkflowYamlError:
        raise
    except yaml.YAMLError as error:
        raise WorkflowYamlError("AFFECTED-WORKFLOW-001", "workflow YAML is invalid") from error


def parse_workflow_text(text: str) -> dict[str, Any]:
    """Parse one GitHub workflow with authoritative YAML scalar semantics."""

    _reject_indirection(text)
    try:
        payload = yaml.load(text, Loader=_WorkflowLoader)
    except WorkflowYamlError:
        raise
    except (RecursionError, yaml.YAMLError) as error:
        raise WorkflowYamlError("AFFECTED-WORKFLOW-001", "workflow YAML is invalid") from error
    if not isinstance(payload, dict) or not all(isinstance(key, str) for key in payload):
        raise WorkflowYamlError("AFFECTED-WORKFLOW-001", "workflow YAML root is invalid")
    return payload


def parse_workflow(path: Path) -> dict[str, Any]:
    """Read and parse a workflow without exposing filesystem details on failure."""

    try:
        if path.is_symlink():
            raise OSError("symbolic link")
        text = path.read_text(encoding="utf-8")
    except (OSError, UnicodeError) as error:
        raise WorkflowYamlError("AFFECTED-WORKFLOW-001", "workflow YAML is unreadable") from error
    return parse_workflow_text(text)
