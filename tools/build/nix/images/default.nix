# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

{
  pkgs,
  ccToolchain,
  system,
}:
{
  cpu = import ./cpu.nix { inherit pkgs ccToolchain system; };
}
