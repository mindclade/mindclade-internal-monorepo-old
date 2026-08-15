# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Deterministic reference training plumbing used by platform qualification.

This module deliberately does *not* qualify model numerics.  It produces a
small, deterministic training/checkpoint/evaluation evidence chain so release
qualification can prove artifact identity, checkpoint lineage, configuration
binding, and evidence publication independently of a frontier model family.
"""

from __future__ import annotations

import hashlib
import json
from dataclasses import dataclass


def _digest(payload: bytes) -> str:
    return "sha256:" + hashlib.sha256(payload).hexdigest()


@dataclass(frozen=True)
class ReferenceCheckpoint:
    step: int
    state_digest: str
    config_digest: str
    dataset_digest: str

    def canonical_bytes(self) -> bytes:
        return json.dumps(
            {
                "config_digest": self.config_digest,
                "dataset_digest": self.dataset_digest,
                "schema_version": 1,
                "state_digest": self.state_digest,
                "step": self.step,
            },
            sort_keys=True,
            separators=(",", ":"),
        ).encode("ascii")

    @property
    def digest(self) -> str:
        return _digest(self.canonical_bytes())


@dataclass(frozen=True)
class ReferenceTrainingEvidence:
    training_run_digest: str
    checkpoint: ReferenceCheckpoint
    evaluation_digest: str
    model_bundle_digest: str


class ReferenceTrainingEngine:
    """Tiny deterministic state transition for release-plumbing qualification."""

    def run(
        self,
        *,
        config_digest: str,
        dataset_digest: str,
        initial_state: int = 7,
        steps: int = 4,
    ) -> ReferenceTrainingEvidence:
        if not config_digest.startswith("sha256:"):
            raise ValueError("config digest must be content addressed")
        if not dataset_digest.startswith("sha256:"):
            raise ValueError("dataset digest must be content addressed")
        if steps <= 0 or steps > 1024:
            raise ValueError("reference steps out of bounds")

        state = initial_state
        transcript: list[int] = []
        for step in range(1, steps + 1):
            # Intentionally simple and deterministic: this is a state/checkpoint
            # plumbing probe, not a numerical training benchmark.
            state = (state * 31 + step * 17) % 1_000_003
            transcript.append(state)

        state_bytes = json.dumps(transcript, separators=(",", ":")).encode("ascii")
        checkpoint = ReferenceCheckpoint(
            step=steps,
            state_digest=_digest(state_bytes),
            config_digest=config_digest,
            dataset_digest=dataset_digest,
        )
        run_digest = _digest(
            b"reference-training-v1\x00"
            + config_digest.encode("ascii")
            + b"\x00"
            + dataset_digest.encode("ascii")
            + b"\x00"
            + checkpoint.digest.encode("ascii")
        )
        evaluation_digest = _digest(
            b"reference-evaluation-v1\x00" + checkpoint.state_digest.encode("ascii")
        )
        model_bundle_digest = _digest(
            b"reference-model-bundle-v1\x00" + checkpoint.digest.encode("ascii")
        )
        return ReferenceTrainingEvidence(
            training_run_digest=run_digest,
            checkpoint=checkpoint,
            evaluation_digest=evaluation_digest,
            model_bundle_digest=model_bundle_digest,
        )
