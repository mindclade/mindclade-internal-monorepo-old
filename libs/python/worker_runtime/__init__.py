"""Reusable Python stage-worker contracts; no policy or distributed control plane."""
from .contracts import ArtifactRef, StageEnvelope, StageKind, StageResult
from .executor import StageEngine, StageExecutor
from .workload import WorkloadEnvelope
__all__ = ["ArtifactRef", "StageEnvelope", "StageKind", "StageResult", "StageEngine", "StageExecutor", "WorkloadEnvelope"]
