# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Reserved boundary, and the design it named was deliberately not adopted.

ADR-0002 says compatibility version files are generated from the Nix-owned
source. This path reserved a renderer that would WRITE .bazelversion,
rust-toolchain.toml, the go directive, requires-python and engines.node from
tools/build/nix/versions.nix.

What exists instead is the assertion rather than the generation:
``tools/build/nix/checks/generated-files.nix`` and
``tools/build/nix/checks/bazel-version.nix`` fail the build when a compat file
disagrees with versions.nix, naming both sides and which one is the source.

The difference matters in review. A generator produces a diff nobody reads and
turns a toolchain change into a mechanical commit; a check makes the person
changing the pin state both halves and shows a reviewer that they agree. The
files are also not uniformly generatable — Cargo.toml's rust-version and go.mod's
go directive carry meaning beyond the pin.

This file stays because docs/blueprint/production-monorepo-paths.txt reserves
the path and tools/analysis/check_blueprint_scaffold.py fails on a reserved path
that does not exist.
"""

from __future__ import annotations

SCAFFOLD_PATH: str = "tools/build/nix/scripts/render-compat-files.py"
IMPLEMENTED_BY: str = "tools/build/nix/checks/generated-files.nix"
