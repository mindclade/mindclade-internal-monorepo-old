"""Canonical provenance manifest for a preprocessed input bundle."""
from __future__ import annotations
from dataclasses import asdict,dataclass
import hashlib,json
from .database_snapshot import DatabaseSnapshot
from .search_record import SearchRecord
from .toolchain import ToolchainRecord
@dataclass(frozen=True)
class Manifest:
    schema_version:int;pipeline_version:str;resolved_config_digest:str;entity_digests:tuple[str,...];reference_databases:tuple[DatabaseSnapshot,...];searches:tuple[SearchRecord,...];tools:tuple[ToolchainRecord,...];output_artifact_digest:str
    def canonical_bytes(self)->bytes:return json.dumps(asdict(self),sort_keys=True,separators=(",",":"),ensure_ascii=True).encode()
    @property
    def digest(self)->str:return "sha256:"+hashlib.sha256(self.canonical_bytes()).hexdigest()
