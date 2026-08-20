#!/usr/bin/env python3
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#
"""Project a staged bundle manifest into OCI manifest annotations.

rules_oci takes `annotations` as a FILE of `name=value` lines, not a dict, which is the right
shape here: half the S8 provenance tuple is not knowable at analysis time.

A Python script rather than sed inside a genrule `cmd`. The manifest is JSON, and parsing JSON
with a regex in a Makefile-quoted shell string inside a Starlark string is three levels of
escaping deep — the kind of thing that works until a digest contains a character somebody did
not think about, and then fails as a malformed annotation rather than as an error.

PROJECTION, NOT SOURCE OF TRUTH. Per ADR-0022 the platform manifest is authoritative for
artifact identity; these annotations exist so an admission controller and a registry browser
can see the same facts without unpacking the artifact.
"""

from __future__ import annotations

import argparse
import json
from pathlib import Path


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--manifest", type=Path, required=True)
    ap.add_argument("--out", type=Path, required=True)
    args = ap.parse_args()

    manifest = json.loads(args.manifest.read_text())

    annotations = {
        "org.opencontainers.image.title": manifest["name"],
        "org.opencontainers.image.description": "Mindclade model weights bundle.",
        "dev.mindclade.artifact.logical-kind": manifest["logical_kind"],
        "dev.mindclade.artifact.schema-version": str(manifest["schema_version"]),
        "dev.mindclade.artifact.media-type": manifest["media_type"],
        # The bundle digest from the platform manifest — content-addressed over the member
        # ArtifactRefs, so it is stable across repacking in a way an OCI layer digest is not.
        "dev.mindclade.artifact.bundle-digest": manifest["digest"],
        "dev.mindclade.artifact.size-bytes": str(manifest["size_bytes"]),
        "dev.mindclade.model.member-count": str(len(manifest["members"])),
    }

    # A newline or '=' in a value would produce a second, attacker-chosen annotation line.
    # Nothing in the current manifest can contain one, which is exactly why it is worth
    # checking now rather than after a field is added that can.
    for key, value in annotations.items():
        if "\n" in value or "\r" in value:
            raise SystemExit(f"emit_annotations: {key} contains a newline; refusing to emit.")

    args.out.write_text("".join(f"{k}={v}\n" for k, v in sorted(annotations.items())))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
