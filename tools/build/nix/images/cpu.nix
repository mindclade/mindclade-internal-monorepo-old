# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

{
  pkgs,
  ccToolchain,
  system,
}:

import ../lib/mk-exec-image.nix {
  inherit pkgs ccToolchain system;
  name = "mindclade-bazel-exec-base";
}
