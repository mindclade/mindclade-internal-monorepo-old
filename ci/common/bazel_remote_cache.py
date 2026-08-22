# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Staged Bazel remote-cache configuration. Inert until activation is qualified.

WHY THIS IS STAGED RATHER THAN WIRED. The infrastructure exists --
infra/terraform/modules/bazel_remote_cache provisions a CMEK Cloud Storage bucket and outputs
the authenticated `https_uri` Bazel would read -- and there are Terragrunt units for ci,
development, staging and production. What is NOT established is that a bucket has been applied,
that the CI workload identity carries the object permissions, or which identity may write. Those
are apply-time and governance facts, not source facts, so this file carries the client contract
and refuses to emit anything until they are recorded.

WHAT ACTIVATION CHANGES, and why it is worth doing. The persistent disk cache added alongside
this is bounded at 4 GiB, keyed per runner OS and architecture, and warmed only by protected-main
pushes, so a pull request touching a widely-depended-on target still rebuilds most of the graph.
A shared remote cache is what closes that; the disk cache is a floor, not a substitute.

Activation also repairs a flag that currently governs nothing.
bazel_disk_cache.configure emits `--remote_upload_local_results`, which applies to a REMOTE
cache; with only `--disk_cache` set, Bazel 9 writes the disk cache unconditionally and that flag
has no effect. The read-only property pull requests rely on is held entirely by the workflow's
`actions/cache/save` gate. Point the flag at a real remote cache and it starts doing the job its
presence implies -- but until then nobody should read it as the enforcement.

FAIL-CLOSED CONTRACT. With activation disabled, or with no endpoint supplied, remote_lines()
returns the empty string and the generated user.bazelrc is byte-identical to what it was before
this module existed. There is no partial state: an endpoint without activation is refused rather
than quietly ignored, because a half-configured cache is the shape that silently stops being
verified.
"""

from __future__ import annotations

import json
from collections.abc import Mapping
from pathlib import Path
from typing import Any
from urllib.parse import urlsplit

DEFAULT_CONTRACT = Path(__file__).with_name("bazel_remote_cache.json")
EXPECTED_FIELDS = {
    "activation",
    "bucket_output",
    "endpoint_variable",
    "schema_version",
    "terraform_module",
    "trusted_writer_events",
}
EXPECTED_MODULE = "infra/terraform/modules/bazel_remote_cache"
EXPECTED_BUCKET_OUTPUT = "https_uri"
EXPECTED_WRITER_EVENTS = ("push", "schedule", "workflow_dispatch")
GCS_HOST = "storage.googleapis.com"


class RemoteCacheError(ValueError):
    """A fail-closed remote-cache contract violation."""


def load_contract(path: Path = DEFAULT_CONTRACT) -> dict[str, Any]:
    try:
        payload = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as error:
        raise RemoteCacheError(f"remote cache contract is unreadable: {error}") from error
    if not isinstance(payload, dict) or set(payload) != EXPECTED_FIELDS:
        raise RemoteCacheError("remote cache contract field inventory is not exact")
    activation = payload["activation"]
    if not isinstance(activation, dict) or set(activation) != {"enabled", "reason"}:
        raise RemoteCacheError("remote cache activation inventory is not exact")
    if not isinstance(activation["enabled"], bool):
        raise RemoteCacheError("remote cache activation.enabled must be boolean")
    if not isinstance(activation["reason"], str) or not activation["reason"]:
        raise RemoteCacheError("remote cache activation.reason must be non-empty")
    if payload["schema_version"] != 1:
        raise RemoteCacheError("only remote cache contract v1 is supported")
    if payload["terraform_module"] != EXPECTED_MODULE:
        raise RemoteCacheError("remote cache provisioning module identity drifted")
    if payload["bucket_output"] != EXPECTED_BUCKET_OUTPUT:
        raise RemoteCacheError("remote cache endpoint output identity drifted")
    if tuple(payload["trusted_writer_events"]) != EXPECTED_WRITER_EVENTS:
        raise RemoteCacheError("remote cache trusted writer events drifted")
    return payload


def validate_endpoint(raw: str) -> str:
    """The endpoint must be the module's authenticated bucket URL and nothing else.

    Bazel reads `--remote_cache=https://storage.googleapis.com/<bucket>` and authenticates with
    the ambient Google credentials; the URL itself carries no secret. Rejecting embedded
    credentials, queries and fragments keeps it that way, and pinning the host stops a
    misconfigured variable pointing the whole build's action cache at an unrelated origin.
    """
    endpoint = urlsplit(raw)
    if (
        endpoint.scheme != "https"
        or endpoint.hostname != GCS_HOST
        or endpoint.username is not None
        or endpoint.password is not None
        or endpoint.query
        or endpoint.fragment
        or not endpoint.path.startswith("/")
        or endpoint.path.strip("/") == ""
        or "/" in endpoint.path.strip("/")
    ):
        raise RemoteCacheError(
            "remote cache endpoint must be https://storage.googleapis.com/<bucket>"
        )
    return raw.rstrip("/")


def remote_lines(
    contract: Mapping[str, Any],
    *,
    role: str,
    endpoint: str = "",
) -> str:
    """The .bazelrc lines for the remote cache, or "" when it is not activated.

    Returning "" rather than a disabled configuration is deliberate: an inert stage should leave
    no trace in the generated file, so `--remote_cache=` appearing there means the cache is
    genuinely on.
    """
    if role not in {"reader", "writer"}:
        raise RemoteCacheError(f"invalid Bazel cache role: {role}")
    enabled = bool(contract["activation"]["enabled"])
    endpoint = endpoint.strip()
    if not enabled:
        if endpoint:
            # Refused rather than ignored. An endpoint present while activation is off means the
            # two halves of this decision have diverged, and the safe reading of that is "stop",
            # not "use the half I trust".
            raise RemoteCacheError(
                "remote cache endpoint supplied while activation is disabled: "
                + str(contract["activation"]["reason"])
            )
        return ""
    if not endpoint:
        raise RemoteCacheError(
            f"remote cache activation requires an endpoint from {contract['endpoint_variable']}"
        )
    validated = validate_endpoint(endpoint)
    # Readers never upload. This is the same role select_trust already derives for the disk
    # cache, so a pull request cannot write to the shared cache any more than it can persist the
    # local one -- and unlike the disk cache, here the flag genuinely enforces it.
    upload = "true" if role == "writer" else "false"
    return (
        f"build --remote_cache={validated}\n"
        "build --google_default_credentials\n"
        f"build --remote_upload_local_results={upload}\n"
    )
