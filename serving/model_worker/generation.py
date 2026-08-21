# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Bounded generation result vocabulary independent of a tokenizer/model."""

from dataclasses import dataclass
from enum import StrEnum


class StopReason(StrEnum):
    END_TOKEN = "end_token"
    LENGTH = "length"
    STOP_SEQUENCE = "stop_sequence"
    CANCELED = "canceled"


@dataclass(frozen=True, slots=True)
class GenerationResult:
    token_ids: tuple[int, ...]
    stop_reason: StopReason
    seed: int

    def validate(self, *, maximum_tokens: int) -> None:
        if isinstance(maximum_tokens, bool) or maximum_tokens <= 0:
            raise ValueError("generation token bound is invalid")
        if len(self.token_ids) > maximum_tokens:
            raise ValueError("generation output exceeds token bound")
        if any(isinstance(token, bool) or not 0 <= token < 2**32 for token in self.token_ids):
            raise ValueError("generation token id is invalid")
        if not isinstance(self.stop_reason, StopReason):
            raise ValueError("generation stop reason is invalid")
        if isinstance(self.seed, bool) or not 0 <= self.seed < 2**64:
            raise ValueError("generation seed is invalid")
