# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Reserved boundary. The capability it named is implemented in Nix.

The toolchain manifest ADR-0002 calls release evidence is rendered by
``tools/build/nix/manifest.nix`` and produced by the flake:

    nix build .#toolchain-manifest
    install -m 0644 result tools/build/nix/toolchain-manifest.json

That path is deliberate rather than incidental. Exporting the manifest means
reading the versions nixpkgs resolved for the pinned revision, which is an
evaluation of the flake — a Python process outside it would have to shell back
into Nix to learn anything, and would then be a second source of truth for the
same data.

This file stays because docs/blueprint/production-monorepo-paths.txt reserves
the path and tools/analysis/check_blueprint_scaffold.py fails on a reserved path
that does not exist. It is not a stub waiting to be filled in: implementing it
would mean moving manifest rendering OUT of the flake, which is an ADR decision.
"""

from __future__ import annotations

SCAFFOLD_PATH: str = "tools/build/nix/scripts/export-manifest.py"
IMPLEMENTED_BY: str = "tools/build/nix/manifest.nix"
