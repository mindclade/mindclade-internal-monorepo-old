# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Mindclade scaffold package for training/distributed."""

from __future__ import annotations

from .lifecycle import initialize, teardown

__all__ = ["initialize", "teardown"]
