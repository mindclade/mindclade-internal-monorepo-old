# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Small fixtures that restore all process-global state they modify."""

from __future__ import annotations

import os
from collections.abc import Iterator, Mapping
from contextlib import contextmanager

from libs.python.errors import InvalidArgument


@contextmanager
def temporary_environ(updates: Mapping[str, str | None]) -> Iterator[None]:
    """Temporarily set/unset environment variables and restore the exact snapshot.

    Environment mutation is process-global and therefore unsuitable for tests that
    execute concurrently in the same process.
    """
    if not isinstance(updates, Mapping):
        raise InvalidArgument("environment updates must be a mapping", reason="testing_environment")
    checked: dict[str, str | None] = {}
    for key, value in updates.items():
        if not isinstance(key, str) or not key or "=" in key or "\x00" in key:
            raise InvalidArgument(
                "environment variable name is invalid", reason="testing_environment_key"
            )
        if value is not None and (not isinstance(value, str) or "\x00" in value):
            raise InvalidArgument(
                "environment variable value is invalid", reason="testing_environment_value"
            )
        checked[key] = value
    snapshot = os.environ.copy()
    try:
        for key, value in checked.items():
            if value is None:
                os.environ.pop(key, None)
            else:
                os.environ[key] = value
        yield
    finally:
        os.environ.clear()
        os.environ.update(snapshot)
