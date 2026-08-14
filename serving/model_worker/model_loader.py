"""Model loading contracts; concrete PyTorch loaders live with model adapters."""
from __future__ import annotations

from dataclasses import dataclass
from typing import Protocol


@dataclass(frozen=True, slots=True)
class LoadedModel:
    bundle_digest: str
    implementation: object


class ModelLoader(Protocol):
    def load(self, bundle_digest: str) -> LoadedModel: ...


class ModelRegistry:
    def __init__(self, loader: ModelLoader) -> None:
        self._loader = loader
        self._models: dict[str, LoadedModel] = {}

    def get_or_load(self, bundle_digest: str) -> LoadedModel:
        if not bundle_digest.startswith("sha256:") or len(bundle_digest) != 71:
            raise ValueError("model bundle digest is invalid")
        model = self._models.get(bundle_digest)
        if model is None:
            model = self._loader.load(bundle_digest)
            if model.bundle_digest != bundle_digest:
                raise ValueError("model loader returned a different bundle digest")
            self._models[bundle_digest] = model
        return model
