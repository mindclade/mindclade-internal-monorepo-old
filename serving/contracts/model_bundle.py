"""Model/runtime bundle compatibility presented to the Python worker."""

from __future__ import annotations

from dataclasses import dataclass


@dataclass(frozen=True, slots=True)
class ModelBundle:
    model_digest: str
    runtime_digest: str
    capabilities: tuple[str, ...]
    schema_version: int = 1

    def validate(self) -> None:
        for digest in (self.model_digest, self.runtime_digest):
            if not digest.startswith("sha256:") or len(digest) != 71:
                raise ValueError("model/runtime bundle digest is invalid")
        if self.schema_version <= 0:
            raise ValueError("model bundle schema version must be positive")
        if tuple(sorted(set(self.capabilities))) != self.capabilities:
            raise ValueError("model bundle capabilities must be sorted and unique")
