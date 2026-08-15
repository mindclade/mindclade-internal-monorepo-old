"""Resolved Python worker runtime manifest."""

from __future__ import annotations

import hashlib
import json
from dataclasses import dataclass


@dataclass(frozen=True, slots=True)
class RuntimeManifest:
    runtime_digest: str
    python_abi: str
    pytorch_version: str
    accelerator: str
    kernel_manifest_digest: str

    def canonical_bytes(self) -> bytes:
        payload = {
            "accelerator": self.accelerator,
            "kernel_manifest_digest": self.kernel_manifest_digest,
            "python_abi": self.python_abi,
            "pytorch_version": self.pytorch_version,
            "runtime_digest": self.runtime_digest,
        }
        return json.dumps(payload, sort_keys=True, separators=(",", ":")).encode("ascii")

    @property
    def fingerprint(self) -> str:
        return "sha256:" + hashlib.sha256(self.canonical_bytes()).hexdigest()
