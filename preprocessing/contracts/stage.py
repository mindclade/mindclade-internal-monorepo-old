"""Scientific-stage contracts; durable leasing/tickets remain control-plane concerns."""
from __future__ import annotations
from dataclasses import dataclass
from enum import StrEnum
from typing import Mapping, Sequence
class StageKind(StrEnum):
    ENTITY_CANONICALIZE="entity_canonicalize"; MSA_SEARCH="msa_search"; TEMPLATE_SEARCH="template_search"; MSA_PAIR="msa_pair"; LIGAND_PREPARE="ligand_prepare"; FEATURIZE="featurize"
@dataclass(frozen=True)
class ArtifactRef:
    digest: str; size_bytes: int; media_type: str; logical_kind: str; schema_version: int
    def __post_init__(self):
        if not self.digest.startswith("sha256:") or self.size_bytes < 0 or "/" not in self.media_type or not self.logical_kind or self.schema_version<=0: raise ValueError("invalid artifact reference")
@dataclass(frozen=True)
class StageInput:
    stage_id: str; kind: StageKind; inputs: Sequence[ArtifactRef]; output_namespace: str; config_digest: str; reference_snapshot_digest: str|None=None; parameters: Mapping[str,str]|None=None
