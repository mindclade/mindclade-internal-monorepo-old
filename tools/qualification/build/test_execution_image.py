# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

from __future__ import annotations

import json
import unittest
from pathlib import Path

from verify_execution_image import validate_attestation, validate_lock

ROOT = Path(__file__).resolve().parents[3]


class ExecutionImageTest(unittest.TestCase):
    def test_committed_lock(self) -> None:
        validate_lock(
            json.loads(
                (ROOT / "infra/build/remote_execution/images.lock.json").read_text(encoding="utf-8")
            )
        )

    def test_reproducible_attestation(self) -> None:
        amd = "sha256:" + "a" * 64
        arm = "sha256:" + "b" * 64
        validate_attestation(
            {
                "schemaVersion": 1,
                "image": "registry.example/mindclade/executor@" + "sha256:" + "c" * 64,
                "user": "65532:65532",
                "bazelVersion": "9.1.1",
                "platforms": {"linux/amd64": amd, "linux/arm64": arm},
                "rebuilds": {"linux/amd64": [amd, amd], "linux/arm64": [arm, arm]},
            }
        )

    def test_non_reproducible_attestation_fails(self) -> None:
        digest = "sha256:" + "a" * 64
        with self.assertRaisesRegex(ValueError, "identical"):
            validate_attestation(
                {
                    "schemaVersion": 1,
                    "image": "registry.example/mindclade/executor@" + digest,
                    "user": "65532:65532",
                    "bazelVersion": "9.1.1",
                    "platforms": {"linux/amd64": digest, "linux/arm64": digest},
                    "rebuilds": {
                        "linux/amd64": [digest, "sha256:" + "b" * 64],
                        "linux/arm64": [digest, digest],
                    },
                }
            )


if __name__ == "__main__":
    unittest.main()
