# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

from __future__ import annotations

import torch

from kernels.api.capabilities import PORTABLE_CPU, DeviceCapabilities
from kernels.providers.tilelang.capabilities import detect_capabilities


def pytorch_capabilities(device: torch.device | str) -> DeviceCapabilities:
    selected = torch.device(device)
    return PORTABLE_CPU if selected.type == "cpu" else detect_capabilities(selected)
