# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

{ pkgs, root, rustToolchain, versions, ... }:
{
  rust-version = import ./rust-version.nix { inherit pkgs rustToolchain versions; };
  flake-lock = import ./flake-lock.nix { inherit pkgs root; };
  version-drift = import ./version-drift.nix { inherit pkgs root versions; };
}
