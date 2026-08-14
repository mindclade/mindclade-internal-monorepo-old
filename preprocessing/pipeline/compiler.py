"""Compile scientific pipeline plans into declarative stage descriptors. Durable tickets are minted by Go."""
from preprocessing.contracts import PipelinePlan
def compile_plan(plan:PipelinePlan)->tuple[dict,...]:
    plan.validate();return tuple({"stage_id":s.spec.stage_id,"kind":str(s.spec.kind),"dependencies":s.dependencies,"output_namespace":s.spec.output_namespace,"config_digest":s.spec.config_digest,"reference_snapshot_digest":s.spec.reference_snapshot_digest} for s in plan.stages)
