"""Deterministic preprocessing DAG contract."""
from __future__ import annotations
from dataclasses import dataclass
from .stage import StageInput
@dataclass(frozen=True)
class PlannedStage:
    spec: StageInput
    dependencies: tuple[str,...]=()
@dataclass(frozen=True)
class PipelinePlan:
    stages: tuple[PlannedStage,...]
    def validate(self)->None:
        ids={s.spec.stage_id for s in self.stages}
        if len(ids)!=len(self.stages): raise ValueError("duplicate preprocessing stage id")
        state={}
        graph={s.spec.stage_id:s.dependencies for s in self.stages}
        def visit(i):
            if state.get(i)==1: raise ValueError("preprocessing pipeline contains a cycle")
            if state.get(i)==2:return
            if i not in graph: raise ValueError(f"unknown stage dependency {i}")
            state[i]=1
            for d in graph[i]: visit(d)
            state[i]=2
        for i in graph: visit(i)
