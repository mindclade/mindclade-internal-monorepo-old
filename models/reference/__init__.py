# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Implemented reference models that exercise Mindclade model boundaries."""

from models.reference.affine import (
    DEFAULT_MAXIMUM_INPUT_ELEMENTS,
    MAXIMUM_REFERENCE_CHECKPOINT_BYTES,
    MAXIMUM_REFERENCE_CONFIG_BYTES,
    REFERENCE_AFFINE_CONFIG_SCHEMA_VERSION,
    REFERENCE_AFFINE_DTYPE,
    REFERENCE_AFFINE_MODEL_NAME,
    REFERENCE_AFFINE_OPERATION,
    ReferenceAffine,
    ReferenceAffineConfig,
    load_reference_affine,
    parse_reference_affine_config,
    reference_affine_config_bytes,
    reference_affine_config_document,
    save_reference_affine,
)

__all__ = [
    "DEFAULT_MAXIMUM_INPUT_ELEMENTS",
    "MAXIMUM_REFERENCE_CHECKPOINT_BYTES",
    "MAXIMUM_REFERENCE_CONFIG_BYTES",
    "REFERENCE_AFFINE_CONFIG_SCHEMA_VERSION",
    "REFERENCE_AFFINE_DTYPE",
    "REFERENCE_AFFINE_MODEL_NAME",
    "REFERENCE_AFFINE_OPERATION",
    "ReferenceAffine",
    "ReferenceAffineConfig",
    "load_reference_affine",
    "parse_reference_affine_config",
    "reference_affine_config_bytes",
    "reference_affine_config_document",
    "save_reference_affine",
]
