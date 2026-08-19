# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#
# Every check reachable from `nix flake check`. A check that exists as a file but is not in this
# attrset does not run — which is how bazel-version, generated-files, no-host-tools and
# toolchain-manifest sat as `{ ... }: { }` stubs while the pins they were meant to guard drifted.

{ pkgs, root, rustToolchain, versions, ... }:
{
  # ADR-0002: compat files are generated from the Nix-owned source.
  bazel-version = import ./bazel-version.nix { inherit pkgs root versions; };
  generated-files = import ./generated-files.nix { inherit pkgs root versions; };
  version-drift = import ./version-drift.nix { inherit pkgs root versions; };

  # ADR-0002: flake evaluation stays pure and version-pinned.
  flake-lock = import ./flake-lock.nix { inherit pkgs root; };
  rust-version = import ./rust-version.nix { inherit pkgs rustToolchain versions; };

  # ADR-0002: CI rejects host-tool leakage and toolchain-manifest drift.
  no-host-tools = import ./no-host-tools.nix { inherit pkgs root; };
  toolchain-manifest = import ./toolchain-manifest.nix { inherit pkgs root versions; };
}
