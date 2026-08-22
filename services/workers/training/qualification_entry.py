#!/usr/bin/env python3
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Executable entry point for the provider-free reference-training source check."""

from services.workers.training.qualification import main

if __name__ == "__main__":
    raise SystemExit(main())
