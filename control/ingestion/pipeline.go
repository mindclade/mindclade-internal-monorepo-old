package ingestion

import (
	"mindclade.internal/control/artifacts"
	"mindclade.internal/control/orchestration"
	"mindclade.internal/control/runtime_authority"
	"mindclade.internal/libs/go/identifiers"
	"time"
)

type Plan struct {
	RunID, JobID, TenantID, WorkspaceID string
	Stages                              []orchestration.StageSpec
}

func (p Plan) Validate() error {
	expected := map[string]string{"run_id": "run", "job_id": "job", "tenant_id": "tenant", "workspace_id": "workspace"}
	values := map[string]string{"run_id": p.RunID, "job_id": p.JobID, "tenant_id": p.TenantID, "workspace_id": p.WorkspaceID}
	for name, kind := range expected {
		id, err := identifiers.ParseID(values[name])
		if err != nil || id.Kind().String() != kind {
			return invalid(name+"_invalid", name+" is invalid", err)
		}
	}
	for _, stage := range p.Stages {
		if !ValidIngestionStage(stage.Kind) {
			return invalid("ingestion_stage_kind_invalid", "ingestion plan contains a non-data stage", nil)
		}
	}
	return (orchestration.Workflow{Stages: p.Stages}).Validate()
}

// CompileWorkload binds a durable ingestion stage attempt to the canonical
// cross-language WorkloadEnvelope. It never invents ticket authority; all
// identity/config/resource claims must already match the Go-issued ticket.
func (p Plan) CompileWorkload(
	workloadID string,
	attempt orchestration.StageAttempt,
	ticket runtime_authority.ExecutionTicket,
	expectedOutputs []artifacts.Ref,
	resourceClass string,
	createdAt time.Time,
) (orchestration.WorkloadEnvelope, error) {
	if err := p.Validate(); err != nil {
		return orchestration.WorkloadEnvelope{}, err
	}
	if err := attempt.Validate(); err != nil {
		return orchestration.WorkloadEnvelope{}, err
	}
	if attempt.RunID != p.RunID || attempt.JobID != p.JobID || !ValidIngestionStage(attempt.Spec.Kind) {
		return orchestration.WorkloadEnvelope{}, invalid("stage_attempt_plan_mismatch", "stage attempt does not belong to ingestion plan", nil)
	}
	workload := orchestration.WorkloadEnvelope{
		WorkloadID: workloadID, RunID: p.RunID, JobID: p.JobID, StageID: attempt.Spec.StageID, Attempt: attempt.Attempt,
		TenantID: p.TenantID, WorkspaceID: p.WorkspaceID, ExecutionTicket: ticket, Inputs: attempt.Spec.Inputs, ExpectedOutputs: expectedOutputs,
		ResolvedConfigDigest: attempt.Spec.ResolvedConfigDigest, ResourceClass: resourceClass, CreatedAt: createdAt, Deadline: createdAt.Add(attempt.Spec.Timeout),
		StageKind: attempt.Spec.Kind, Operation: attempt.Spec.Operation,
	}
	if err := workload.Validate(createdAt); err != nil {
		return orchestration.WorkloadEnvelope{}, err
	}
	return workload, nil
}
