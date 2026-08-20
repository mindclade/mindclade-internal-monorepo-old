# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#
# The Nix-owned version source. ADR-0002 makes Nix the authority for toolchains and says
# compatibility version files are GENERATED FROM it — `.bazelversion`, `rust-toolchain.toml`,
# the `go` directive in go.mod, `requires-python`, `engines.node`. Those files exist because
# bazelisk, cargo, go and node each insist on reading their own; this attrset is what they are
# all supposed to agree with, and `checks/generated-files.nix` plus `checks/bazel-version.nix`
# are what make disagreement a build failure rather than a surprise months later.
#
# Changing a version here means changing the compat file that mirrors it in the same commit.
# The checks name both sides when they diverge, so a mismatch is a two-line fix rather than a
# hunt.

{
  # Consumed by .bazelversion — bazelisk reads that file to decide which Bazel to fetch — and
  # asserted by checks/bazel-version.nix.
  bazel = "9.1.1";

  # Consumed by rust-toolchain.toml's `channel` and Cargo.toml's `rust-version`, and asserted
  # three ways: checks/rust-version.nix runs the pinned rustc, checks/version-drift.nix reads
  # Cargo.toml, checks/generated-files.nix reads rust-toolchain.toml.
  rust = "1.97.1";

  # Language-level pin, major.minor only: go.mod carries a patch ("go 1.26.0") because the go
  # directive is a language-version floor, while the toolchain nixpkgs resolves is newer still.
  # Comparing only major.minor is what keeps a nixpkgs patch bump from failing the check.
  #
  # WAS "1.24" while go.mod said 1.26.0 and the devShell shipped go 1.26.5 — inert, because
  # nothing read this attribute and no check existed to notice. That is the exact failure mode
  # these checks are for.
  go = "1.26";

  # Consumed by pyproject.toml's requires-python, [tool.ruff] target-version, and mypy's
  # python_version. Major.minor for the same reason as go.
  python = "3.12";

  # Consumed by package.json's engines.node. Major only: the devShell pins nodejs_22 and the
  # patch level follows nixpkgs.
  nodeMajor = 22;

  # Defines the compatible pnpm release line. The generated-files check also requires
  # package.json's `packageManager` field to equal the exact pnpm version resolved by nixpkgs,
  # because Corepack needs an exact version and CI installs from that field.
  #
  # WAS absent while package.json said pnpm@10 and the devShell shipped 11.21.0 — so a developer
  # inside `nix develop` ran pnpm 11 and corepack outside it ran pnpm 10, against one lockfile.
  pnpmMajor = 11;

  # The oldest macOS version produced binaries may require. This belongs beside the compiler
  # pin because it changes object and linker semantics, not application behavior.
  darwinDeploymentTarget = "14.0";
}
