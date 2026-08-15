# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Immutable Python inference-request contract after Rust admission."""

from __future__ import annotations

from dataclasses import dataclass

MAX_DESCRIPTORS = 4096
MAX_CAPABILITIES = 256
MAX_REQUEST_KEY_BYTES = 16 * 1024


@dataclass(frozen=True, slots=True)
class InputDescriptor:
    segment_id: str
    digest: str
    locator: str
    length_bytes: int
    generation: int
    lease_expires_unix_millis: int

    def validate(self, now_unix_millis: int) -> None:
        if not self.segment_id or len(self.segment_id) > 256:
            raise ValueError("input descriptor segment id is invalid")
        if not self.digest.startswith("sha256:") or len(self.digest) != 71:
            raise ValueError("input descriptor digest is invalid")
        if not self.locator or len(self.locator) > 4096:
            raise ValueError("input descriptor locator is invalid")
        if self.length_bytes <= 0 or self.generation <= 0:
            raise ValueError("input descriptor bounds are invalid")
        if self.lease_expires_unix_millis <= now_unix_millis:
            raise ValueError("input descriptor lease has expired")


@dataclass(frozen=True, slots=True)
class InferenceRequest:
    request_id: str
    model_bundle_digest: str
    request_key: bytes
    inputs: tuple[InputDescriptor, ...]
    required_capabilities: tuple[str, ...]
    input_units: int
    output_units: int
    deadline_unix_millis: int

    def validate(self, now_unix_millis: int) -> None:
        if not self.request_id or len(self.request_id) > 256:
            raise ValueError("request id is invalid")
        if (
            not self.model_bundle_digest.startswith("sha256:")
            or len(self.model_bundle_digest) != 71
        ):
            raise ValueError("model bundle digest is invalid")
        if len(self.request_key) > MAX_REQUEST_KEY_BYTES:
            raise ValueError("request key exceeds bound")
        if not self.inputs or len(self.inputs) > MAX_DESCRIPTORS:
            raise ValueError("input descriptor count is outside bounds")
        if len(self.required_capabilities) > MAX_CAPABILITIES:
            raise ValueError("required capability count exceeds bound")
        if tuple(sorted(set(self.required_capabilities))) != self.required_capabilities:
            raise ValueError("required capabilities must be sorted and unique")
        if self.input_units <= 0 or self.output_units <= 0:
            raise ValueError("request input/output units must be positive")
        if self.deadline_unix_millis <= now_unix_millis:
            raise ValueError("request deadline has expired")
        for descriptor in self.inputs:
            descriptor.validate(now_unix_millis)
