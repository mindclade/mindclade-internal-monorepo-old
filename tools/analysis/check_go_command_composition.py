#!/usr/bin/env python3
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Enforce the production composition mandate across EVERY Go command in services/.

`libs/go/CONSUMPTION.md` states the rule repository-wide: every production Go
executable composes through `servicekit/production.Builder`, and direct use of
`servicekit.New`, ad hoc signal handling, detached goroutines, process-local
retry loops, or service-local health frameworks is prohibited in production
commands.

Until this checker existed the rule was enforced for exactly one directory.
`check_control_plane_commands.py` and `services/control_plane/internal/bootstrap`
`promotion_test.go` both scan `services/control_plane/cmd` and nothing else, so
`services/studio/cmd/studio` and `services/go_vanity/cmd/go_vanity` sat outside
every guard -- and both were in fact violating it. A rule enforced in one of
several places is not a rule; it is a rule plus a silent exemption for everyone
who did not happen to be in the scanned directory. This checker's scope is the
mandate's scope: every Go `main` package under `services/`.

Two properties are deliberate.

EVERY NON-TEST FILE IN THE PACKAGE IS READ, not just `main.go`. A command
package is one Go package: a sibling `wiring.go` compiles into the same binary
and can bypass the lifecycle exactly as `main.go` can, so a guard that opens
only `main.go` reports clean on a binary that violates the rule. `_test.go`
files are excluded because the mandate exempts them -- tests and low-level
conformance suites may use the lower-level APIs directly.

COMMENTS AND LITERALS ARE REMOVED BEFORE MATCHING. Without that, this very
docstring's rule names would trip a grep, `//go:build` reads as a goroutine, and
the import path `go.mindclade.dev/...` reads as `go <call>`. The scrubber blanks
comments, interpreted strings, raw strings, and rune literals in place, keeping
newlines so reported line numbers stay true to the file.

WHAT THIS DOES NOT COVER: the mandate's "process-local retry loops" and
"service-local health frameworks" clauses have no spelling that can be
recognized from source text without a heuristic loose enough to need its own
allowlist -- which is the failure mode this checker exists to remove. They stay
a review obligation, named here so the boundary is visible rather than implied
by silence.
"""

from __future__ import annotations

import argparse
import re
import sys
from dataclasses import dataclass
from pathlib import Path

sys.dont_write_bytecode = True

COMMAND_ROOT = "services"

# `signal.Notify` is a prefix on purpose: it catches `signal.NotifyContext` too,
# which is the spelling a command actually reaches for.
#
# The goroutine pattern is statement-anchored rather than a substring, and
# accepts BOTH spellings: `go func(` for an inline closure and `go start(` for a
# named call. Anchoring on start-of-line or a preceding `{`, `}`, or `;` is what
# separates the `go` keyword from every other appearance of those two letters.
RULES: tuple[tuple[str, re.Pattern[str], str], ...] = (
    (
        "servicekit.New(",
        re.compile(re.escape("servicekit.New(")),
        "constructs a service directly instead of composing through servicekit/production.Builder",
    ),
    (
        "servicekit.NewAssembly(",
        re.compile(re.escape("servicekit.NewAssembly(")),
        "assembles its own service instead of composing through servicekit/production.Builder",
    ),
    (
        "signal.Notify",
        re.compile(re.escape("signal.Notify")),
        "takes signal ownership from servicekit instead of letting the lifecycle own termination",
    ),
    (
        "go-statement",
        re.compile(
            r"(?:^|[{};])[ \t]*go[ \t]+(?:func[ \t]*\(|[A-Za-z_][A-Za-z0-9_.]*[ \t]*\()",
            re.MULTILINE,
        ),
        "starts a detached goroutine instead of registering a servicekit component",
    ),
)

RULE_NAMES = frozenset(name for name, _, _ in RULES)


@dataclass(frozen=True)
class Exemption:
    """One reviewed, narrowly pinned carve-out.

    `rules` names the exact violations the command is excused for, so a NEW kind
    of bypass in an exempt command still fails. A stale entry -- one whose
    command no longer trips the named rule -- is itself reported as an error, so
    this table can only shrink without someone editing this file. That is what
    keeps it a ratchet rather than an allowlist.
    """

    rules: frozenset[str]
    reason: str


# Both entries below record PRE-EXISTING violations that this checker's widened
# scope newly makes visible. Neither is fixable in place today, and the blocker
# is the same for both: `libs/go/servicekit/production.Role` is a CLOSED enum of
# twelve control-plane roles, and every role profile in `profile.go` requires at
# minimum CapabilityDatabase and CapabilityTransactions. `production.NewBuilder`
# rejects an unknown role outright, so there is no valid way for a browser-plane
# or a static-content command to enter the sanctioned path until a role that
# describes it is admitted to that enum. Closing these means changing
# `libs/go/servicekit/production`, not the commands.
EXEMPTIONS: dict[str, Exemption] = {
    "services/studio/cmd/studio": Exemption(
        rules=frozenset({"servicekit.New("}),
        reason=(
            "The browser plane has no production.Role. Its four roles (web, bff, bff-stream, "
            "embed) are not in the closed twelve-role control-plane enum, and every existing "
            "role profile mandates a database and transactions -- capabilities the embed and "
            "web roles are deliberately built without (see the comment at main.go:89). The "
            "command does compose a staged servicekit lifecycle with a drain-then-stop ordering "
            "and a single injected clock; what it cannot do is declare a role profile that does "
            "not exist. Admitting a browser-plane role to libs/go/servicekit/production is the "
            "fix, and it is a change to that package rather than to this command."
        ),
    ),
    "services/go_vanity/cmd/go_vanity": Exemption(
        rules=frozenset({"signal.Notify"}),
        reason=(
            "Serves go-import meta tags and owns no durable state. It has no production.Role "
            "either, and takes signal ownership through signal.NotifyContext rather than "
            "through the lifecycle, so its shutdown is not bounded by the servicekit budget. "
            "Same blocker as studio: there is no role in the closed enum this command could "
            "declare, and every existing profile demands a database it does not have."
        ),
    ),
}


def _blank(chunk: str) -> str:
    """Replace a lexical element with spaces, preserving newlines.

    Line numbers in the report have to match the real file, so removed regions
    keep their line structure instead of collapsing.
    """
    return "".join("\n" if character == "\n" else " " for character in chunk)


def scrub(source: str) -> str:
    """Blank Go comments and string/rune literals so patterns match real code."""
    output: list[str] = []
    index = 0
    length = len(source)
    while index < length:
        character = source[index]
        following = source[index + 1] if index + 1 < length else ""
        if character == "/" and following == "/":
            end = source.find("\n", index)
            end = length if end < 0 else end
            output.append(_blank(source[index:end]))
            index = end
        elif character == "/" and following == "*":
            end = source.find("*/", index + 2)
            end = length if end < 0 else end + 2
            output.append(_blank(source[index:end]))
            index = end
        elif character == "`":
            end = source.find("`", index + 1)
            end = length if end < 0 else end + 1
            output.append(_blank(source[index:end]))
            index = end
        elif character in ('"', "'"):
            end = index + 1
            while end < length and source[end] != character:
                if source[end] == "\\":
                    end += 2
                    continue
                # An interpreted string or rune literal cannot span a newline.
                # Stopping here keeps a malformed file from blanking the rest of
                # the source and hiding every violation after it.
                if source[end] == "\n":
                    break
                end += 1
            end = min(end + 1, length)
            output.append(_blank(source[index:end]))
            index = end
        else:
            output.append(character)
            index += 1
    return "".join(output)


def _line_of(source: str, offset: int) -> int:
    return source.count("\n", 0, offset) + 1


def package_sources(directory: Path) -> list[Path]:
    """Every non-test Go file that compiles into this command's binary."""
    return sorted(
        path
        for path in directory.glob("*.go")
        if path.is_file() and not path.name.endswith("_test.go")
    )


def is_command_package(directory: Path) -> bool:
    return any(
        re.search(r"(?m)^package\s+main\s*$", scrub(path.read_text(encoding="utf-8")))
        for path in package_sources(directory)
    )


def command_packages(root: Path) -> list[Path]:
    command_root = root / COMMAND_ROOT
    if not command_root.is_dir():
        return []
    return sorted(
        directory
        for directory in command_root.rglob("*")
        if directory.is_dir() and is_command_package(directory)
    )


def violations(directory: Path, relative: str) -> dict[str, list[str]]:
    """Report every rule this command package trips, keyed by rule name."""
    found: dict[str, list[str]] = {}
    for path in package_sources(directory):
        source = path.read_text(encoding="utf-8")
        scrubbed = scrub(source)
        for name, pattern, message in RULES:
            for match in pattern.finditer(scrubbed):
                line = _line_of(scrubbed, match.start())
                found.setdefault(name, []).append(f"{relative}/{path.name}:{line}: {message}")
    return found


def check(root: Path) -> list[str]:
    errors: list[str] = []
    packages = command_packages(root)
    if not packages:
        # Fail closed. An empty scan is indistinguishable from a passing one, and
        # a checker that reports success on zero inputs is the exact fail-open
        # shape this file was written to remove.
        return [f"{COMMAND_ROOT}: no Go command packages were found"]

    for name, exemption in sorted(EXEMPTIONS.items()):
        unknown = exemption.rules - RULE_NAMES
        if unknown:
            errors.append(f"{name}: exemption names unknown rule(s) {', '.join(sorted(unknown))}")

    scanned: set[str] = set()
    for directory in packages:
        relative = directory.relative_to(root).as_posix()
        scanned.add(relative)
        exemption = EXEMPTIONS.get(relative)
        excused = exemption.rules if exemption else frozenset()
        found = violations(directory, relative)
        for name in sorted(found):
            if name in excused:
                continue
            errors.extend(found[name])
        for name in sorted(excused):
            if name not in found:
                errors.append(
                    f"{relative}: exemption for {name} is stale -- the command no longer "
                    f"trips that rule, so remove the entry from EXEMPTIONS"
                )

    for name in sorted(set(EXEMPTIONS) - scanned):
        errors.append(f"{name}: exemption names a command package that does not exist")
    return errors


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--repo", type=Path, default=Path(__file__).resolve().parents[2])
    args = parser.parse_args(argv)
    errors = check(args.repo.resolve())
    for error in errors:
        print(error, file=sys.stderr)
    if errors:
        print(
            f"Go command composition check failed with {len(errors)} finding(s)",
            file=sys.stderr,
        )
        return 1
    print("Go command composition check passed")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
