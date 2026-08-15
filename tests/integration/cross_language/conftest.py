# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Make this directory importable by its own test modules.

Several tests here import a sibling helper by bare name — `test_execution_tickets.py` pulls
`claims` from `test_execution_ticket_golden`. Under pytest's legacy `prepend` import mode that
worked implicitly, because prepend put each test file's directory on `sys.path` as a side
effect of collection. The suite now runs under `--import-mode=importlib`, which imports by path
and deliberately does not mutate `sys.path`, so those sibling imports need this.

Local to the directory on purpose: the alternative is naming every such directory in
`[tool.pytest.ini_options] pythonpath`, which turns a directory-local fact into a root-level
list that nobody updates when a directory moves.
"""

from __future__ import annotations

import sys
from pathlib import Path

_HERE = str(Path(__file__).resolve().parent)
if _HERE not in sys.path:
    sys.path.insert(0, _HERE)
