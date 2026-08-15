"""Make this directory importable by its own test modules.

`test_reference_training.py` imports `ReferenceTrainingEngine` from the sibling
`reference_training` module by bare name. Under pytest's legacy `prepend` import mode that
resolved implicitly; under `--import-mode=importlib`, which imports by path and does not
mutate `sys.path`, it does not.

See the matching conftest in tests/integration/cross_language/ — same reason, same shape.
"""

from __future__ import annotations

import sys
from pathlib import Path

_HERE = str(Path(__file__).resolve().parent)
if _HERE not in sys.path:
    sys.path.insert(0, _HERE)
