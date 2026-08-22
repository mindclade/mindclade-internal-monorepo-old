# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Implemented data-parallel wrappers."""

from .ddp import wrap_ddp

__all__ = ["wrap_ddp"]
