# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Scaffold test for data/contracts/tests/test_contracts.py."""


import pytest


# SKIPPED, not passing.
#
# A placeholder for tests that do not exist yet. It used to `assert True` — which pytest
# reports as a pass, so the suite was green and the number it printed was not the number of
# things actually verified. A vacuous gate is worse than no gate: it manufactures confidence.
#
# Write real tests here when there is an implementation to test, and lower SCAFFOLD_BASELINE
# in tests/integration/test_python_scaffold.py in the same commit.
@pytest.mark.scaffold
def test_scaffold_contract() -> None:
    pytest.skip("scaffold: no implementation to test yet")
