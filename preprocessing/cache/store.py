"""Provider-neutral immutable preprocessing cache metadata."""
from __future__ import annotations
from dataclasses import dataclass
from typing import Protocol
from preprocessing.contracts import ArtifactRef
@dataclass(frozen=True)
class Entry:
    key: str; artifact: ArtifactRef; producer_version: str; qualified: bool
class Store(Protocol):
    def get(self,key:str)->Entry|None: ...
    def put_if_absent(self,entry:Entry)->Entry: ...
class MemoryStore:
    def __init__(self): self._entries={}
    def get(self,key): return self._entries.get(key)
    def put_if_absent(self,entry): self._entries.setdefault(entry.key,entry); return self._entries[entry.key]
