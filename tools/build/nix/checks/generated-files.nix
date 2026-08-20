# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#
# ADR-0002: "Compatibility version files are generated from the Nix-owned source."
#
# Every language toolchain in this tree insists on reading its own version file — cargo reads
# rust-toolchain.toml, go reads the `go` directive, uv/ruff/mypy read pyproject.toml, node reads
# engines.node. They cannot be deleted in favour of versions.nix, so the next best thing is to
# make divergence fail the build. This check is that assertion for every compat file EXCEPT
# `.bazelversion`, which checks/bazel-version.nix owns, and Cargo.toml's `rust-version`, which
# checks/version-drift.nix owns.
#
# Comparisons are deliberately not all exact-equality:
#
#   * go.mod and pyproject pin a language LEVEL, and their files legitimately carry more
#     precision than versions.nix does (`go 1.26.0` against `go = "1.26"`). Those compare on
#     major.minor, so a nixpkgs patch bump does not fail a check about language compatibility.
#   * rust-toolchain.toml pins an exact toolchain and compares exactly. A rustc that is merely
#     "new enough" is not what that file means.
#
# Regex rather than a TOML/JSON parser for the TOML files: pulling tomllib in would be fine, but
# the failure mode worth catching is a hand-edited line, and matching the line means the error
# can quote it back. package.json is parsed as JSON because it is JSON.

{
  pkgs,
  root,
  versions,
  ...
}:
pkgs.runCommand "mindclade-generated-files"
  {
    nativeBuildInputs = [ pkgs.python3 ];
  }
  ''
    python3 - <<'PY'
    import json
    import re
    import sys
    from pathlib import Path

    root = Path("${root}")

    rust = "${versions.rust}"
    go = "${versions.go}"
    python_version = "${versions.python}"
    node_major = "${toString versions.nodeMajor}"
    pnpm_major = "${toString versions.pnpmMajor}"

    failures: list[str] = []


    def fail(path: str, expected: str, actual: str, note: str = "") -> None:
        message = f"  {path}\n      expected: {expected}\n      actual:   {actual}"
        if note:
            message += f"\n      {note}"
        failures.append(message)


    def read(rel: str) -> str | None:
        path = root / rel
        if not path.is_file():
            failures.append(f"  {rel}\n      missing: no such file in the flake source")
            return None
        return path.read_text()


    def search(pattern: str, text: str, rel: str, what: str) -> str | None:
        match = re.search(pattern, text, re.MULTILINE)
        if match is None:
            failures.append(f"  {rel}\n      unreadable: no {what} line matched {pattern!r}")
            return None
        return match.group(1)


    def major_minor(version: str) -> str:
        return ".".join(version.split(".")[:2])


    # --- rust-toolchain.toml: exact -------------------------------------------------------
    text = read("rust-toolchain.toml")
    if text is not None:
        channel = search(r'^\s*channel\s*=\s*"([^"]+)"', text, "rust-toolchain.toml", "channel")
        if channel is not None and channel != rust:
            fail("rust-toolchain.toml [toolchain] channel", rust, channel)

    # --- go.mod: major.minor --------------------------------------------------------------
    text = read("go.mod")
    if text is not None:
        directive = search(r'^go\s+(\S+)', text, "go.mod", "go directive")
        if directive is not None and major_minor(directive) != go:
            fail(
                "go.mod go directive",
                f"{go}.x",
                directive,
                "versions.nix pins major.minor; the patch component is free.",
            )

    # --- pyproject.toml: three spellings of one version -----------------------------------
    text = read("pyproject.toml")
    if text is not None:
        requires = search(
            r'^requires-python\s*=\s*"([^"]+)"', text, "pyproject.toml", "requires-python"
        )
        if requires is not None and requires != f">={python_version}":
            fail("pyproject.toml [project] requires-python", f">={python_version}", requires)

        # ruff spells it pyXY, with no separator.
        target = search(
            r'^target-version\s*=\s*"(py\d+)"', text, "pyproject.toml", "[tool.ruff] target-version"
        )
        expected_target = "py" + python_version.replace(".", "")
        if target is not None and target != expected_target:
            fail("pyproject.toml [tool.ruff] target-version", expected_target, target)

        mypy = search(
            r'^python_version\s*=\s*"([^"]+)"', text, "pyproject.toml", "[tool.mypy] python_version"
        )
        if mypy is not None and mypy != python_version:
            fail("pyproject.toml [tool.mypy] python_version", python_version, mypy)

    # --- package.json: engines.node -------------------------------------------------------
    text = read("package.json")
    if text is not None:
        try:
            manifest = json.loads(text)
        except json.JSONDecodeError as exc:
            failures.append(f"  package.json\n      unreadable: {exc}")
        else:
            node = manifest.get("engines", {}).get("node")
            if node is None:
                failures.append("  package.json\n      missing: engines.node")
            elif node != f">={node_major}":
                fail("package.json engines.node", f">={node_major}", node)

            # corepack reads this to decide which pnpm to run, so it is the file that decides
            # what a developer OUTSIDE the devShell gets against the same lockfile.
            manager = manifest.get("packageManager")
            if manager is None:
                failures.append("  package.json\n      missing: packageManager")
            elif manager != f"pnpm@{pnpm_major}":
                fail("package.json packageManager", f"pnpm@{pnpm_major}", manager)

    # --- .github/workflows/*.yml ----------------------------------------------------------
    # The CI lanes pin their own toolchains, and those pins are the same compatibility surface
    # as the files above: a lane pinned below what the repository requires is a gate that tests
    # a toolchain nobody uses. `go-version: "1.25.12"` sat under `go 1.26.0` in go.mod for long
    # enough to acquire a comment explaining the bug, which is the argument for asserting it
    # here rather than describing it there.
    #
    # Workflow pins carry a patch and versions.nix pins major.minor, so most of these compare by
    # prefix. Rust is exact because versions.rust is exact.
    #
    # The value pattern requires a digit, which is what separates `toolchain: "1.97.1"` (a pin)
    # from `toolchain:` (a job id — presubmit.yml has one).
    WORKFLOW_PINS = [
        ("go-version", go, "prefix"),
        ("python-version", python_version, "prefix"),
        ("node-version", node_major, "prefix"),
        ("toolchain", rust, "exact"),
    ]

    workflows = sorted((root / ".github" / "workflows").glob("*.yml"))
    if not workflows:
        failures.append(
            "  .github/workflows/\n"
            "      missing: no *.yml files, so the lane pins were not checked at all"
        )

    pins_seen = 0
    for workflow in workflows:
        source = workflow.read_text()
        rel = workflow.relative_to(root)

        for key, expected, mode in WORKFLOW_PINS:
            pattern = rf'^\s*{re.escape(key)}:\s*"?(\d[^"\s#]*)"?'
            for match in re.finditer(pattern, source, re.MULTILINE):
                pins_seen += 1
                actual = match.group(1)
                line_no = source[: match.start()].count("\n") + 1

                if mode == "exact":
                    ok = actual == expected
                    wanted = expected
                else:
                    ok = actual == expected or actual.startswith(f"{expected}.")
                    wanted = f"{expected}.x"

                if not ok:
                    fail(
                        f"{rel}:{line_no} {key}",
                        wanted,
                        actual,
                        "a lane pinned off the repository's toolchain gates the wrong version.",
                    )

    # A check that silently stops finding anything is worse than no check: if the workflows are
    # restructured so these keys move, this says so instead of passing.
    if workflows and pins_seen == 0:
        failures.append(
            "  .github/workflows/\n"
            "      unreadable: no toolchain pins matched, but the workflows exist.\n"
            "      Either the pins moved or the key names changed; update WORKFLOW_PINS."
        )

    if failures:
        print("generated-files: compatibility files disagree with the Nix-owned source.", file=sys.stderr)
        print("", file=sys.stderr)
        print("\n".join(failures), file=sys.stderr)
        print("", file=sys.stderr)
        print(
            "tools/build/nix/versions.nix is the source (ADR-0002). Either update the file above\n"
            "to match it, or change versions.nix and every file that mirrors the changed pin.",
            file=sys.stderr,
        )
        raise SystemExit(1)
    PY

    touch "$out"
  ''
