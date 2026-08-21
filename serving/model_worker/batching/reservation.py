# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Exact request/unit reservation accounting for final tensor batches."""

from threading import Lock


class BatchReservationLedger:
    def __init__(self, maximum_requests: int, maximum_units: int) -> None:
        if min(maximum_requests, maximum_units) <= 0:
            raise ValueError("batch reservation limits must be positive")
        self._maximum_requests = maximum_requests
        self._maximum_units = maximum_units
        self._requests = 0
        self._units = 0
        self._lock = Lock()

    def reserve(self, requests: int, units: int) -> bool:
        if min(requests, units) <= 0:
            raise ValueError("batch reservation must be positive")
        with self._lock:
            if (
                self._requests + requests > self._maximum_requests
                or self._units + units > self._maximum_units
            ):
                return False
            self._requests += requests
            self._units += units
            return True

    def release(self, requests: int, units: int) -> None:
        with self._lock:
            if min(requests, units) <= 0 or requests > self._requests or units > self._units:
                raise ValueError("batch reservation release is invalid")
            self._requests -= requests
            self._units -= units
