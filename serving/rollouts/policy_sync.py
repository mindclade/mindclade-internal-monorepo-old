# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Monotonic policy snapshot synchronization."""

from __future__ import annotations

from dataclasses import dataclass
from threading import Lock


@dataclass(frozen=True, slots=True)
class PolicySnapshot:
    revision: int
    policy_digest: str
    expires_unix_millis: int

    def validate(self, *, now_unix_millis: int) -> None:
        if isinstance(self.revision, bool) or self.revision <= 0:
            raise ValueError("policy revision must be positive")
        if not self.policy_digest.startswith("sha256:") or len(self.policy_digest) != 71:
            raise ValueError("policy snapshot digest is invalid")
        if self.expires_unix_millis <= now_unix_millis:
            raise ValueError("policy snapshot has expired")


class PolicySynchronizer:
    def __init__(self) -> None:
        self._snapshot: PolicySnapshot | None = None
        self._lock = Lock()

    def update(self, snapshot: PolicySnapshot, *, now_unix_millis: int) -> bool:
        snapshot.validate(now_unix_millis=now_unix_millis)
        with self._lock:
            if self._snapshot is not None and snapshot.revision <= self._snapshot.revision:
                return False
            self._snapshot = snapshot
            return True

    def current(self, *, now_unix_millis: int) -> PolicySnapshot | None:
        with self._lock:
            snapshot = self._snapshot
        if snapshot is None or snapshot.expires_unix_millis <= now_unix_millis:
            return None
        return snapshot
