# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Reader protocol keeps object clients worker-local and provider-owned."""

from __future__ import annotations

from collections.abc import Iterator
from typing import Protocol

from data.sample import Sample


class SampleReader(Protocol):
    def open(self, *, start_index: int = 0) -> Iterator[Sample]: ...

    def close(self) -> None: ...
