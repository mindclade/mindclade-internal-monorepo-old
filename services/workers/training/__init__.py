# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Concrete reference training composition root and shared worker adapter."""

from .artifacts import ArtifactIO
from .checkpoint_publication import (
    CHECKPOINT_COMMIT_LOGICAL_KIND,
    CHECKPOINT_COMMIT_MEDIA_TYPE,
    CheckpointCommitPlan,
    CheckpointCommitReceipt,
    CheckpointCommitRequest,
    CheckpointCommitter,
    CheckpointProvenance,
)
from .config import worker_limits
from .executor import build_executor
from .lifecycle import Lifecycle, State
from .main import build_worker
from .reference_affine import (
    CHECKPOINT_LOGICAL_KIND,
    CHECKPOINT_MEDIA_TYPE,
    CONFIG_LOGICAL_KIND,
    CONFIG_MEDIA_TYPE,
    DATASET_LOGICAL_KIND,
    DATASET_MEDIA_TYPE,
    RUN_EVIDENCE_LOGICAL_KIND,
    RUN_EVIDENCE_MEDIA_TYPE,
    TRAINING_OPERATION,
    ReferenceAffineTrainingConfig,
    ReferenceAffineTrainingEngine,
    load_reference_affine_export,
    reference_topology_digest,
)

__all__ = [
    "CHECKPOINT_COMMIT_LOGICAL_KIND",
    "CHECKPOINT_COMMIT_MEDIA_TYPE",
    "CHECKPOINT_LOGICAL_KIND",
    "CHECKPOINT_MEDIA_TYPE",
    "CONFIG_LOGICAL_KIND",
    "CONFIG_MEDIA_TYPE",
    "DATASET_LOGICAL_KIND",
    "DATASET_MEDIA_TYPE",
    "RUN_EVIDENCE_LOGICAL_KIND",
    "RUN_EVIDENCE_MEDIA_TYPE",
    "TRAINING_OPERATION",
    "ArtifactIO",
    "CheckpointCommitPlan",
    "CheckpointCommitReceipt",
    "CheckpointCommitRequest",
    "CheckpointCommitter",
    "CheckpointProvenance",
    "Lifecycle",
    "ReferenceAffineTrainingConfig",
    "ReferenceAffineTrainingEngine",
    "State",
    "build_executor",
    "build_worker",
    "load_reference_affine_export",
    "reference_topology_digest",
    "worker_limits",
]
