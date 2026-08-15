# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

from reference_training import ReferenceTrainingEngine

CONFIG = "sha256:" + "1" * 64
DATASET = "sha256:" + "2" * 64


def test_reference_training_evidence_is_deterministic_and_content_bound() -> None:
    engine = ReferenceTrainingEngine()
    first = engine.run(config_digest=CONFIG, dataset_digest=DATASET)
    second = engine.run(config_digest=CONFIG, dataset_digest=DATASET)
    assert first == second
    assert first.checkpoint.config_digest == CONFIG
    assert first.checkpoint.dataset_digest == DATASET
    assert first.checkpoint.digest.startswith("sha256:")
    assert first.training_run_digest.startswith("sha256:")
    assert first.evaluation_digest.startswith("sha256:")
    assert first.model_bundle_digest.startswith("sha256:")


def test_reference_training_changes_when_config_changes() -> None:
    engine = ReferenceTrainingEngine()
    first = engine.run(config_digest=CONFIG, dataset_digest=DATASET)
    second = engine.run(config_digest="sha256:" + "3" * 64, dataset_digest=DATASET)
    assert first.training_run_digest != second.training_run_digest
    assert first.checkpoint.digest != second.checkpoint.digest
