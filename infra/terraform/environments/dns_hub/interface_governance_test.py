#!/usr/bin/env python3
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

from __future__ import annotations

import importlib
import sys
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[4]
sys.dont_write_bytecode = True
sys.path.insert(0, str(ROOT))

verify_scope = importlib.import_module(
    "infra.terraform.governance.interface_governance"
).verify_scope


class DnsHubInterfaceGovernanceTest(unittest.TestCase):
    def test_committed_dns_hub_scope_matches_hcl(self) -> None:
        self.assertEqual([], verify_scope(ROOT, "dns_hub"))


if __name__ == "__main__":
    unittest.main()
