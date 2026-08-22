#!/usr/bin/env python3
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

from __future__ import annotations

import subprocess
import tempfile
import unittest
from pathlib import Path

from protocols.qualification.canary import (
    EXPECTED_SERVICES,
    READ_CANARIES,
    CanaryError,
    run_canary,
)


class CanaryTest(unittest.TestCase):
    def setUp(self) -> None:
        self.temporary = tempfile.TemporaryDirectory()
        self.schema = Path(self.temporary.name)
        (self.schema / "buf.yaml").write_text("version: v2\n", encoding="utf-8")

    def tearDown(self) -> None:
        self.temporary.cleanup()

    def test_authenticated_reads_and_negative_auth_are_required(self) -> None:
        calls: list[list[str]] = []

        def runner(argv: list[str], **_: object) -> subprocess.CompletedProcess[str]:
            calls.append(argv)
            if "--list-services" in argv:
                return subprocess.CompletedProcess(argv, 0, "\n".join(EXPECTED_SERVICES), "")
            if "--header" not in argv:
                return subprocess.CompletedProcess(argv, 128, "", "Unauthenticated")
            header = Path(argv[argv.index("--header") + 1].removeprefix("@"))
            self.assertEqual(
                header.read_text(encoding="utf-8"),
                "Authorization: Bearer opaque-token\n",
            )
            return subprocess.CompletedProcess(argv, 0, "{}\n", "")

        evidence = run_canary(
            "https://control.example.internal",
            "opaque-token",
            self.schema,
            runner=runner,
        )
        self.assertEqual(len(calls), len(READ_CANARIES) + 2)
        self.assertEqual(
            evidence["authentication"],
            {"authenticatedReads": "pass", "anonymousRead": "UNAUTHENTICATED"},
        )
        self.assertNotIn("opaque-token", repr(evidence))

    def test_missing_reflected_service_fails_closed(self) -> None:
        def runner(argv: list[str], **_: object) -> subprocess.CompletedProcess[str]:
            return subprocess.CompletedProcess(argv, 0, "", "")

        with self.assertRaisesRegex(CanaryError, "missing promoted services"):
            run_canary("https://control.example.internal", "token", self.schema, runner=runner)

    def test_endpoint_must_be_origin_only_tls(self) -> None:
        for endpoint in (
            "http://control.example.internal",
            "https://user@control.example.internal",
            "https://control.example.internal/path",
            "https://control.example.internal?debug=true",
        ):
            with self.subTest(endpoint=endpoint), self.assertRaises(CanaryError):
                run_canary(endpoint, "token", self.schema)


if __name__ == "__main__":
    unittest.main()
