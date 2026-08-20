# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
"""Safely materialize a Bazel-built validation input archive as real files."""

from __future__ import annotations

import argparse
import pathlib
import tarfile


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("archive", type=pathlib.Path)
    parser.add_argument("destination", type=pathlib.Path)
    args = parser.parse_args()

    args.destination.mkdir(parents=True, exist_ok=True)
    with tarfile.open(args.archive, mode="r:") as archive:
        for member in archive.getmembers():
            path = pathlib.PurePosixPath(member.name)
            if path.is_absolute() or ".." in path.parts:
                raise SystemExit(f"unsafe archive member: {member.name}")
            if member.issym() or member.islnk():
                raise SystemExit(f"validation archive must contain real files: {member.name}")
        archive.extractall(args.destination, filter="data")


if __name__ == "__main__":
    main()
