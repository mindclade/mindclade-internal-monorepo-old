# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Reserved boundary. The capability it named is implemented in Nix.

Manifest verification is ``tools/build/nix/checks/toolchain-manifest.nix``, run
by ``nix flake check`` and by the presubmit lint lane. It validates the manifest
against tools/qualification/schemas/toolchain-manifest.schema.json and then
diffs the committed copy against what the flake resolves, which is the
"CI rejects toolchain-manifest drift" half of ADR-0002.

Verification has to happen where the resolution happens, for the same reason
rendering does: the value being verified is a property of the evaluated flake.

This file stays because docs/blueprint/production-monorepo-paths.txt reserves
the path and tools/analysis/check_blueprint_scaffold.py fails on a reserved path
that does not exist.
"""

from __future__ import annotations

SCAFFOLD_PATH: str = "tools/build/nix/scripts/verify-manifest.py"
IMPLEMENTED_BY: str = "tools/build/nix/checks/toolchain-manifest.nix"
