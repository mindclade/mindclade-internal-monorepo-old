# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

from .policy import CachePolicy
from .store import Entry, Store


def lookup(store: Store, key: str, policy: CachePolicy) -> Entry | None:
    entry = store.get(key)
    return entry if entry is not None and policy.accepts(entry) else None
