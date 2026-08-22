# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

from __future__ import annotations

import copy
import unittest

from check_remote_execution import compare


def evidence(mode: str) -> dict:
    value = {
        "schemaVersion": 1,
        "mode": mode,
        "bazelVersion": "9.1.1",
        "platform": "linux/amd64",
        "executionImage": "sha256:" + "a" * 64,
        "toolchainManifest": "sha256:" + "b" * 64,
        "targets": {"//services/go_vanity:image": "sha256:" + "c" * 64},
        "networkAccess": False,
        "hostPathInputs": [],
        "remoteExecution": None,
    }
    if mode == "remote":
        value["remoteExecution"] = {
            "backend": "buildfarm-2.17.0",
            "endpoint": "grpcs://buildfarm.example.internal:8980",
            "executedActions": 4,
            "cacheOnly": False,
            "invocationId": "018f-example",
        }
    return value


class RemoteExecutionTest(unittest.TestCase):
    def test_parity_passes(self) -> None:
        self.assertEqual(compare(evidence("local"), evidence("remote"))["verdict"], "pass")

    def test_output_difference_fails(self) -> None:
        remote = copy.deepcopy(evidence("remote"))
        remote["targets"]["//services/go_vanity:image"] = "sha256:" + "d" * 64
        with self.assertRaisesRegex(ValueError, "targets"):
            compare(evidence("local"), remote)

    def test_cache_only_fails(self) -> None:
        remote = evidence("remote")
        remote["remoteExecution"]["cacheOnly"] = True
        with self.assertRaisesRegex(ValueError, "executed action"):
            compare(evidence("local"), remote)

    def test_host_path_fails(self) -> None:
        remote = evidence("remote")
        remote["hostPathInputs"] = ["/" + "usr/bin/clang"]
        with self.assertRaisesRegex(ValueError, "host-path"):
            compare(evidence("local"), remote)


if __name__ == "__main__":
    unittest.main()
