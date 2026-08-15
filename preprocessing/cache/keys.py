# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Content-bound preprocessing cache keys."""

from __future__ import annotations

import hashlib
import json
from collections.abc import Mapping, Sequence


def cache_key(
    namespace: str,
    *,
    digests: Sequence[str] = (),
    fields: Mapping[str, str | int | bool] | None = None,
) -> str:
    if not namespace:
        raise ValueError("cache namespace required")
    payload = {
        "namespace": namespace,
        "digests": sorted(digests),
        "fields": dict(sorted((fields or {}).items())),
    }
    raw = json.dumps(payload, sort_keys=True, separators=(",", ":"), ensure_ascii=True).encode()
    return "sha256:" + hashlib.sha256(raw).hexdigest()


def msa_search_key(
    entity_digest: str,
    tool: str,
    tool_version: str,
    database_snapshot_digest: str,
    parameters_digest: str,
) -> str:
    return cache_key(
        "msa-search/v1",
        digests=[entity_digest, database_snapshot_digest, parameters_digest],
        fields={"tool": tool, "tool_version": tool_version},
    )


def template_search_key(
    entity_digest: str,
    profile_digest: str,
    database_snapshot_digest: str,
    max_template_date: str,
    policy_digest: str,
    tool_version: str,
) -> str:
    return cache_key(
        "template-search/v1",
        digests=[entity_digest, profile_digest, database_snapshot_digest, policy_digest],
        fields={"max_template_date": max_template_date, "tool_version": tool_version},
    )


def feature_bundle_key(
    complex_digest: str,
    artifact_digests: Sequence[str],
    feature_schema: str,
    model_input_contract: str,
    policy_digest: str,
) -> str:
    return cache_key(
        "feature-bundle/v1",
        digests=[complex_digest, *artifact_digests, policy_digest],
        fields={"feature_schema": feature_schema, "model_input_contract": model_input_contract},
    )
