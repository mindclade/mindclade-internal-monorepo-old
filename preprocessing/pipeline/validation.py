from preprocessing.contracts import PipelinePlan


def validate_plan(plan: PipelinePlan) -> PipelinePlan:
    plan.validate()
    return plan
