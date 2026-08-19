# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#
# ADR-0002 enforcement: "CI rejects host-tool leakage."
# Blueprint: "No undeclared host tools or network package installation occur in Bazel actions."
#
# The devShells in flake.nix put the pinned toolchain on PATH; they cannot stop a Bazel action
# from reaching around them. Three things do, and this check asserts all three are in place and
# that nothing in the build graph defeats them:
#
#   1. .bazelrc keeps the hermeticity flags. --incompatible_strict_action_env is what stops the
#      client's PATH being inherited into actions; without it an action that happens to find
#      /opt/homebrew/bin/protoc on a developer laptop passes there and fails on a runner that
#      has no Homebrew, months later, in an unrelated change.
#   2. Nothing re-opens the door: an --action_env=PATH, a standalone spawn strategy, or a
#      re-enabled action network puts the host back in the action environment, and each of
#      those reads as a local convenience in review rather than as an ADR violation.
#   3. No action COMMAND names a host path or installs from the network at build time.
#
# Scope note on (3). This matches command-position attributes only — `cmd`, `cmd_bash`,
# `cmd_bat`, `cmd_ps`, `executable` — rather than every string in every BUILD file, because
# absolute paths are legitimate elsewhere and flagging them would train people to ignore this
# check. //services/go_vanity is the working example: its `entrypoint` and pkg_tar mapping name
# /usr/local/bin/go_vanity, which is a path INSIDE the produced container image, not a tool the
# action runs. A rule attribute that executes is the only place a /usr/bin means what this
# check is about.
#
# Regex rather than a Starlark parser: Bazel is not available inside `nix flake check` (it would
# need the network to fetch itself), so a real parse would mean vendoring one. The patterns are
# anchored on the attribute name and quoted value, which is the form these files actually use.

{ pkgs, root, ... }:
pkgs.runCommand "mindclade-no-host-tools"
  {
    nativeBuildInputs = [ pkgs.python3 ];
  }
  ''
    python3 - <<'PY'
    import re
    import sys
    from pathlib import Path

    root = Path("${root}")
    failures: list[str] = []


    # --- 1 and 2: the .bazelrc contract ---------------------------------------------------
    bazelrc = root / ".bazelrc"
    if not bazelrc.is_file():
        failures.append(
            ".bazelrc\n"
            "    missing: without it every invocation runs with Bazel's defaults, which do not\n"
            "    include strict action env."
        )
    else:
        text = bazelrc.read_text()

        # Comments in this file discuss the flags at length, so match only real rc lines: a
        # directive, whitespace, then the flag.
        def has_flag(flag: str) -> bool:
            return re.search(rf'^\s*[a-z:]+\s+{re.escape(flag)}(\s|=|$)', text, re.MULTILINE) is not None

        required = [
            (
                "--incompatible_strict_action_env",
                "actions would inherit the client PATH, so a host tool can satisfy a build",
            ),
            (
                "--sandbox_default_allow_network=false",
                "actions could fetch from the network, which is how undeclared inputs enter",
            ),
        ]
        for flag, why in required:
            if not has_flag(flag):
                failures.append(f".bazelrc\n    missing flag: {flag}\n    without it, {why}.")

        # Anything that re-opens what the required flags close. --action_env=PATH is matched
        # specifically rather than all --action_env, which has legitimate uses.
        forbidden = [
            (r'--(host_)?action_env=PATH', "puts the host PATH back into the action environment"),
            (r'--noincompatible_strict_action_env', "disables the strict action environment"),
            (r'--sandbox_default_allow_network=true', "re-enables the network for actions"),
            (r'--spawn_strategy=(standalone|local)\b', "runs actions unsandboxed against the host"),
        ]
        for pattern, why in forbidden:
            for match in re.finditer(rf'^\s*[a-z:]+\s+.*{pattern}.*$', text, re.MULTILINE):
                line = match.group(0).strip()
                failures.append(f".bazelrc\n    forbidden: {line}\n    it {why}.")

    # --- 2 continued: overrides that point Bazel at a host checkout ------------------------
    module = root / "MODULE.bazel"
    if module.is_file():
        for match in re.finditer(r'^\s*local_path_override\s*\(', module.read_text(), re.MULTILINE):
            failures.append(
                "MODULE.bazel\n"
                "    forbidden: local_path_override resolves a dependency from a path on the\n"
                "    machine running the build, which is host leakage by definition."
            )

    # --- 3: action commands ---------------------------------------------------------------
    # Attribute value forms: a single-quoted "..." value and a triple-double-quoted block. The
    # select()/concatenation cases fall out of this because the scan is per quoted value rather
    # than per attribute.
    COMMAND_ATTRS = r'(?:cmd|cmd_bash|cmd_bat|cmd_ps|executable)'
    VALUE = (
        rf'^\s*{COMMAND_ATTRS}\s*=\s*'
        r'(?:"""(?P<triple>.*?)"""|"(?P<single>[^"\n]*)")'
    )

    HOST_PATHS = [
        (r'(?<![\w/])/usr/bin/', "/usr/bin"),
        (r'(?<![\w/])/usr/local/bin/', "/usr/local/bin"),
        (r'(?<![\w/])/opt/homebrew/', "/opt/homebrew"),
        (r'(?<![\w/])/opt/local/', "/opt/local (MacPorts)"),
        (r'(?<![\w/])~/', "the invoking user's home directory"),
        (r'\$HOME\b', "the invoking user's home directory"),
    ]

    NETWORK_INSTALLS = [
        (r'\bpip3?\s+install\b', "pip install"),
        (r'\b(npm|pnpm|yarn)\s+(install|add)\b', "a node package install"),
        (r'\bcargo\s+install\b', "cargo install"),
        (r'\bgo\s+install\b', "go install"),
        (r'\b(curl|wget)\b', "an ad-hoc download"),
        (r'\b(apt-get|apk|yum|dnf|brew)\s+', "a system package manager"),
    ]

    for path in sorted(root.rglob("*")):
        if not path.is_file():
            continue
        if path.name != "BUILD.bazel" and path.suffix != ".bzl":
            continue

        rel = path.relative_to(root)
        try:
            source = path.read_text()
        except UnicodeDecodeError:
            continue

        for match in re.finditer(VALUE, source, re.MULTILINE | re.DOTALL):
            command = match.group("triple") or match.group("single") or ""
            line_no = source[: match.start()].count("\n") + 1

            for pattern, label in HOST_PATHS:
                if re.search(pattern, command):
                    failures.append(
                        f"{rel}:{line_no}\n"
                        f"    host tool: the command names {label}\n"
                        f"    the action must run a tool from the pinned toolchain or a\n"
                        f"    declared Bazel dependency, not one the machine happens to have."
                    )

            for pattern, label in NETWORK_INSTALLS:
                if re.search(pattern, command):
                    failures.append(
                        f"{rel}:{line_no}\n"
                        f"    network install: the command runs {label}\n"
                        f"    build-time dependencies must be declared, not fetched."
                    )

    if failures:
        print("no-host-tools: host-tool leakage (ADR-0002).", file=sys.stderr)
        print("", file=sys.stderr)
        for failure in failures:
            print(f"  {failure}", file=sys.stderr)
            print("", file=sys.stderr)
        raise SystemExit(1)
    PY

    touch "$out"
  ''
