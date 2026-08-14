"""Process-local projection of the canonical orchestration workload envelope."""
from __future__ import annotations
from dataclasses import dataclass
import re
from .contracts import StageEnvelope
_ID=re.compile(r"^[a-z][a-z0-9]{1,23}_[0-9a-f]{32}$")
@dataclass(frozen=True,slots=True)
class WorkloadEnvelope:
    workload_id:str; run_id:str; job_id:str; tenant_id:str; workspace_id:str
    execution_ticket_id:str; stage:StageEnvelope; resource_class:str; created_unix_millis:int
    def validate(self)->None:
        for value in (self.workload_id,self.run_id,self.job_id,self.tenant_id,self.workspace_id,self.execution_ticket_id):
            if not _ID.fullmatch(value): raise ValueError("workload identity must use canonical resource IDs")
        if not self.resource_class or len(self.resource_class)>128 or self.created_unix_millis<=0: raise ValueError("workload resource class/creation time is invalid")
        self.stage.validate()
