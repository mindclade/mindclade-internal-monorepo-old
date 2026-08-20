# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

{ pkgs, versions, ... }:
let
  toolchain = pkgs.rust-bin.stable.${versions.rust}.default.override {
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
