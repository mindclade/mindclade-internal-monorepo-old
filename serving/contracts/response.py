"""Bounded immutable inference result contract."""

from __future__ import annotations

from dataclasses import dataclass


@dataclass(frozen=True, slots=True)
class InferenceResult:
    request_id: str
    output_digest: str
    output_bytes: int
    model_bundle_digest: str
    diagnostics: tuple[tuple[str, str], ...] = ()

    def validate(self) -> None:
        if not self.request_id or not self.output_digest.startswith("sha256:"):
            raise ValueError("inference result identity is invalid")
        if self.output_bytes <= 0:
            raise ValueError("inference result is empty")
        if not self.model_bundle_digest.startswith("sha256:"):
            raise ValueError("inference result model bundle digest is invalid")
        if len(self.diagnostics) > 256:
            raise ValueError("inference diagnostics exceed bounds")
