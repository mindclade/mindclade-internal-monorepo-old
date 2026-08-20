# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

{ pkgs, versions, ... }:
let
  # The default rustup profile includes the offline HTML documentation, which adds almost a
  # gigabyte to every development closure. The repository needs the compiler, Cargo, standard
  # library, formatter, linter, and sources for editor analysis; the minimal profile plus these
  # explicit extensions is exactly that set.
  toolchain = pkgs.rust-bin.stable.${versions.rust}.minimal.override {
    extensions = [
      "clippy"
      "rust-src"
      "rustfmt"
    ];
  };
in
{
  inherit toolchain;
  packages = [ toolchain ];
  version = versions.rust;
}
