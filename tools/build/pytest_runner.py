# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Entry point that lets a Bazel `py_test` run pytest-style tests.

A `py_test` executes its `main` as a script. Test files in this tree are pytest modules —
bare `test_*` functions, `pytest.raises`, `@pytest.mark.scaffold` — and running one as a
script executes no test at all and exits 0. That is how //serving/model_worker/tests targets
came to exist and pass vacuously while importing a pytest that was not even a dependency.

So the target's `main` is this file, and the test modules are `srcs` handed to pytest.

CONFIGURATION IS PASSED EXPLICITLY, not discovered. pytest walks up from rootdir looking for
pyproject.toml, and under Bazel the runfiles tree either does not contain it or contains it at
a path that makes rootdir wrong. Rather than depend on that, the BUILD macro puts pyproject.toml
in `data` and this runner points `-c` at it, so the Bazel run and `uv run pytest` apply the same
markers, the same import mode, and the same nightly deselection.
"""

from __future__ import annotations

import os
import sys

import pytest


def _config_path() -> str | None:
    """Locate the repository pyproject.toml inside the runfiles tree."""
    here = os.path.dirname(os.path.abspath(__file__))
    # Walk up out of tools/build/ to the runfiles root that holds pyproject.toml.
    while True:
        candidate = os.path.join(here, "pyproject.toml")
        if os.path.isfile(candidate):
            return candidate
        parent = os.path.dirname(here)
        if parent == here:
            return None
        here = parent


def main(argv: list[str]) -> int:
    tests = [arg for arg in argv if arg.endswith(".py")]
    if not tests:
        print("pytest_runner: no test modules were passed", file=sys.stderr)
        return 2

    args = ["-ra", "-p", "no:cacheprovider"]
    config = _config_path()
    if config is not None:
        args += ["-c", config, "--rootdir", os.path.dirname(config)]
    args += [arg for arg in argv if not arg.endswith(".py")]
    args += tests
    return int(pytest.main(args))


if __name__ == "__main__":
    raise SystemExit(main(sys.argv[1:]))
