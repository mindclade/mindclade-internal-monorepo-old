# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Generated-source assertions used as qualification evidence, not runtime dispatch."""

from __future__ import annotations

from kernels.tilelang.compiler.ir import KernelSourceArtifact

MAXIMUM_CONTRACT_TOKENS = 128
MAXIMUM_TOKEN_BYTES = 256


def require_codegen_tokens(
    artifact: KernelSourceArtifact, *, required: tuple[str, ...], forbidden: tuple[str, ...] = ()
) -> None:
    """Check a bounded lexical token contract after excluding C-style comments.

    This is deliberately only a generated-source heuristic. It does not prove that a
    matching instruction survives compilation or executes at runtime.
    """

    _validate_tokens("required", required)
    _validate_tokens("forbidden", forbidden)
    overlap = sorted(set(required).intersection(forbidden))
    if overlap:
        raise ValueError(f"tokens cannot be both required and forbidden: {overlap!r}")

    inspected_source = _without_c_style_comments(artifact.source)
    missing = [token for token in required if not _contains_token(inspected_source, token)]
    present = [token for token in forbidden if _contains_token(inspected_source, token)]
    if missing or present:
        raise ValueError(
            f"generated source contract failed: missing={missing!r}, forbidden_present={present!r}"
        )


def _validate_tokens(name: str, tokens: tuple[str, ...]) -> None:
    if not isinstance(tokens, tuple):
        raise TypeError(f"{name} tokens must be a tuple")
    if len(tokens) > MAXIMUM_CONTRACT_TOKENS:
        raise ValueError(f"{name} tokens exceed the {MAXIMUM_CONTRACT_TOKENS}-token limit")
    if len(tokens) != len(set(tokens)):
        raise ValueError(f"{name} tokens must be unique")
    for token in tokens:
        if not isinstance(token, str):
            raise TypeError(f"{name} tokens must be text")
        if not token or any(character.isspace() for character in token):
            raise ValueError(f"{name} tokens must be non-empty and cannot contain whitespace")
        if len(token.encode("utf-8")) > MAXIMUM_TOKEN_BYTES:
            raise ValueError(f"{name} tokens exceed the {MAXIMUM_TOKEN_BYTES}-byte limit")


def _contains_token(source: str, token: str) -> bool:
    """Return whether *token* appears outside a larger identifier."""

    offset = 0
    while (index := source.find(token, offset)) >= 0:
        before = source[index - 1] if index else ""
        end = index + len(token)
        after = source[end] if end < len(source) else ""
        if not _identifier_character(before) and not _identifier_character(after):
            return True
        offset = index + 1
    return False


def _identifier_character(character: str) -> bool:
    return bool(character) and (character.isalnum() or character == "_")


def _without_c_style_comments(source: str) -> str:
    """Replace // and /* */ comment contents with spaces, preserving literals and lines."""

    # C and C++ remove escaped newlines before recognizing comments. Apply that
    # translation phase first so `/\\\n/ token` cannot spoof a non-comment token.
    source = source.replace("\\\r\n", "").replace("\\\n", "")
    output: list[str] = []
    index = 0
    length = len(source)
    quote = ""
    escaped = False
    while index < length:
        character = source[index]
        if quote:
            output.append(character)
            index += 1
            if escaped:
                escaped = False
            elif character == "\\":
                escaped = True
            elif character == quote:
                quote = ""
            continue
        raw_string_end = _raw_string_end(source, index)
        if raw_string_end is not None:
            output.append(source[index:raw_string_end])
            index = raw_string_end
            continue
        if character in {'"', "'"}:
            quote = character
            output.append(character)
            index += 1
            continue
        if source.startswith("//", index):
            output.extend((" ", " "))
            index += 2
            while index < length:
                character = source[index]
                if character == "\n":
                    preceding_backslashes = 0
                    cursor = index - 1
                    while cursor >= 0 and source[cursor] == "\\":
                        preceding_backslashes += 1
                        cursor -= 1
                    output.append("\n")
                    index += 1
                    if preceding_backslashes % 2 == 0:
                        break
                else:
                    output.append(" ")
                    index += 1
            continue
        if source.startswith("/*", index):
            output.extend((" ", " "))
            index += 2
            while index < length and not source.startswith("*/", index):
                output.append("\n" if source[index] == "\n" else " ")
                index += 1
            if index < length:
                output.extend((" ", " "))
                index += 2
            continue
        output.append(character)
        index += 1
    return "".join(output)


def _raw_string_end(source: str, index: int) -> int | None:
    """Return the end of a C++ raw string starting at *index*, if present."""

    if index and _identifier_character(source[index - 1]):
        return None
    prefix = next(
        (
            candidate
            for candidate in ('u8R"', 'uR"', 'UR"', 'LR"', 'R"')
            if source.startswith(candidate, index)
        ),
        None,
    )
    if prefix is None:
        return None
    delimiter_start = index + len(prefix)
    opening_parenthesis = source.find("(", delimiter_start, delimiter_start + 17)
    if opening_parenthesis < 0:
        return None
    delimiter = source[delimiter_start:opening_parenthesis]
    if any(character.isspace() or character in "()\\" for character in delimiter):
        return None
    terminator = f'){delimiter}"'
    closing = source.find(terminator, opening_parenthesis + 1)
    return len(source) if closing < 0 else closing + len(terminator)
