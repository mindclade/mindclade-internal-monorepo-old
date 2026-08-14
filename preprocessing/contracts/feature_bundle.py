"""Immutable model-ready preprocessing output."""
from dataclasses import dataclass
from .stage import ArtifactRef
@dataclass(frozen=True)
class FeatureBundle:
    artifact: ArtifactRef
    schema: str
    model_input_contract: str
    entity_digests: tuple[str,...]
    msa_digests: tuple[str,...]=()
    template_digests: tuple[str,...]=()
    reference_snapshot_digests: tuple[str,...]=()
