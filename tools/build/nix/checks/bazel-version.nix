# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#
# ADR-0002: "Compatibility version files are generated from the Nix-owned source."
#
# `.bazelversion` is the compatibility file with the most leverage in this repository, because
# bazelisk reads it and nothing else does. If it disagrees with tools/build/nix/versions.nix,
# the Bazel that developers and CI actually run is not the Bazel the toolchain source claims to
# pin, and every other hermeticity guarantee in the tree is asserted against the wrong binary.
# That is not a hypothetical: versions.nix said 9.2.0 while .bazelversion said 8.4.2, and the
# only reason nobody noticed is that this check was `{ ... }: { }`.
#
# Deliberately a pure text comparison rather than an invocation of Bazel. Running
# `bazel --version` inside a check would need network access to fetch the release, which
# `nix flake check` correctly does not have, and would make a build-graph tool a dependency of
# the check that guards its version.

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
    echo "bazel-version: $file does not exist; bazelisk would fall back to its own default" >&2
    exit 1
  }

  # tr rather than $(cat) alone: bazelisk tolerates a trailing newline and so must this, but a
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

  touch "$out"
''
