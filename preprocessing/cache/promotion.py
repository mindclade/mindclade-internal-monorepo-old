# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

from .store import Entry


def promote(entry: Entry) -> Entry:
    return Entry(entry.key, entry.artifact, entry.producer_version, True)
