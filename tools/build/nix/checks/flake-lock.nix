# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

{ pkgs, root, ... }:
pkgs.runCommand "mindclade-flake-lock" {} ''
  test -s ${root}/flake.lock || { echo "flake.lock is missing or empty" >&2; exit 1; }
  touch "$out"
