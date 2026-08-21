# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#
# ADR-0002: "Compatibility version files are generated from the Nix-owned source."
#
# `.bazelversion` is the Bazel/Bazelisk compatibility file. If it disagrees with
# tools/build/nix/versions.nix, developers outside Nix and the Nix-owned Bazel package can run
# different launchers, and every other hermeticity guarantee is asserted against the wrong binary.
# That is not a hypothetical: versions.nix said 9.2.0 while .bazelversion said 8.4.2, and the
# only reason nobody noticed is that this check was `{ ... }: { }`.
#
# Deliberately a pure text comparison. The resolved `pkgs.bazel_9` version is also recorded in
# the toolchain manifest, where `nix flake check` compares it with committed evidence.

{
  pkgs,
  root,
  versions,
  ...
}:
pkgs.runCommand "mindclade-bazel-version" { } ''
  declared="${versions.bazel}"
  file="${root}/.bazelversion"

  test -f "$file" || {
    echo "bazel-version: $file does not exist; external Bazelisk would fall back to its own default" >&2
    exit 1
  }

  # tr rather than $(cat) alone: Bazelisk tolerates a trailing newline and so must this, but a
  # file containing only whitespace must fail rather than compare equal to an empty pin.
  actual="$(tr -d '[:space:]' < "$file")"

  test -n "$actual" || {
    echo "bazel-version: $file is empty" >&2
    exit 1
  }

  if [ "$actual" != "$declared" ]; then
    echo "bazel-version: Bazel pin drift." >&2
    echo "  tools/build/nix/versions.nix  bazel = \"$declared\"" >&2
    echo "  .bazelversion                 $actual" >&2
    echo "versions.nix is the source (ADR-0002); update .bazelversion to match it." >&2
    exit 1
  fi

  # The blueprint states the Bazel version in its header, and prose drifts in a way a pin
  # file does not: nothing loads a Markdown heading, so nothing objects when it goes stale.
  # It said 9.2.0 while versions.nix said 9.1.1, which is how a reader ends up reasoning
  # about a Bazel this repository has never run.
  blueprint="${root}/docs/blueprint/production-monorepo-blueprint.md"

  if [ -f "$blueprint" ]; then
    stated="$(sed -n 's/^\*\*Build graph:\*\* Bazel \([0-9.]*\) .*/\1/p' "$blueprint" | head -1)"

    test -n "$stated" || {
      echo "bazel-version: could not read the Bazel version from $blueprint." >&2
      echo "Expected a line like: **Build graph:** Bazel $declared with Bzlmod" >&2
      exit 1
    }

    if [ "$stated" != "$declared" ]; then
      echo "bazel-version: the blueprint states a Bazel version nothing else in the tree uses." >&2
      echo "  tools/build/nix/versions.nix  bazel = \"$declared\"" >&2
      echo "  $blueprint  Bazel $stated" >&2
      echo "versions.nix is the source (ADR-0002); update the blueprint header to match it." >&2
      exit 1
    fi
  fi

  touch "$out"
''
