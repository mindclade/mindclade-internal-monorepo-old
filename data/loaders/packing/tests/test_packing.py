# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

from __future__ import annotations

import pytest

from data.loaders.packing import bucket_for, pack_lengths, pad_sequences


def test_bin_packing_preserves_every_index_without_overflow() -> None:
    bins = pack_lengths((4, 3, 2, 1), 5)
    assert sorted(index for packed in bins for index in packed.indices) == [0, 1, 2, 3]
    assert all(packed.used <= packed.capacity for packed in bins)
    assert bins == pack_lengths((4, 3, 2, 1), 5)


def test_padding_and_boundaries_are_explicit() -> None:
    tokens, mask = pad_sequences(((1, 2), (3,)), pad_token=0)
    assert tokens.tolist() == [[1, 2], [3, 0]]
    assert mask.tolist() == [[True, True], [True, False]]
    assert bucket_for(5, (4, 8, 16)) == 8
    with pytest.raises(ValueError, match="exceeds"):
        bucket_for(17, (4, 8, 16))
