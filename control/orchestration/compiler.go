// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package orchestration

import (
	"sort"
	"strconv"

	"go.mindclade.dev/control/artifacts"
	"go.mindclade.dev/control/runtime_authority"
	"go.mindclade.dev/libs/go/identifiers"
	"time"
)

// WorkflowSchemaVersion is the schema this package compiles. It is written into
// every compiled workflow so a plan decoded by a newer binary can tell whether
// the shape it is reading is the shape it understands.
const WorkflowSchemaVersion uint32 = 1

// CompileRequest is the untrusted half of a compilation: a name and an ordered
// stage list. Identity and digest are server-owned and are computed here, never
// accepted from a caller — a client-supplied definition digest would let a
// caller name one plan and run another.
type CompileRequest struct {
	Name   string
	Stages []StageSpec
}

// CompiledWorkflow is an immutable, addressable plan. Graph is derived from
// Workflow and carried alongside it so a reconciler never rebuilds the index.
type CompiledWorkflow struct {
	Workflow Workflow
	Graph    Graph
}

// Compile validates the stage graph, assigns identity, and seals the plan with a
// content digest.
//
// The digest covers every field that changes what executes, so two requests that
// differ anywhere in their stages produce different digests, and re-compiling an
// identical request reproduces the same digest. It deliberately excludes the
// workflow ID and name: those identify the plan, they do not define it, and
// including them would make an identical plan submitted twice look like two
// different computations.
func Compile(request CompileRequest, id identifiers.ID) (CompiledWorkflow, error) {
	if err := validateBoundedName(request.Name, "workflow_name", MaximumOutputNamespaceLength); err != nil {
		return CompiledWorkflow{}, err
	}
	if err := validateID(id.String(), "workflow", "workflow_id"); err != nil {
		return CompiledWorkflow{}, err
	}
	stages := append([]StageSpec(nil), request.Stages...)
	// Sorting before digesting makes the digest independent of submission
	// order, so a client that lists the same stages in a different order gets
	// the same plan rather than a spurious second one. The topological order is
	// what execution follows; this order only fixes the preimage.
	sort.Slice(stages, func(i, j int) bool { return stages[i].StageID < stages[j].StageID })
	workflow := Workflow{
		ID:               id.String(),
		Name:             request.Name,
		Stages:           stages,
		DefinitionDigest: definitionDigest(stages),
		SchemaVersion:    WorkflowSchemaVersion,
	}
	if err := workflow.ValidateIdentity(); err != nil {
		return CompiledWorkflow{}, err
	}
	graph, err := NewGraph(workflow)
	if err != nil {
		return CompiledWorkflow{}, err
	}
	return CompiledWorkflow{Workflow: workflow, Graph: graph}, nil
}

// definitionDigest hashes the canonical preimage of a stage list. Every field
// that alters execution appears; the unit separator that joins them is rejected
// by the field validators, so no two distinct stage lists can collide by
// re-partitioning the same bytes.
func definitionDigest(stages []StageSpec) identifiers.Digest {
	parts := make([]string, 0, len(stages)*10)
	parts = append(parts, strconv.FormatUint(uint64(WorkflowSchemaVersion), 10))
	for _, stage := range stages {
		dependencies := append([]string(nil), stage.Dependencies...)
		sort.Strings(dependencies)
		parts = append(parts,
			stage.StageID,
			string(stage.Kind),
			stage.Operation,
			stage.OutputNamespace,
			stage.ResolvedConfigDigest.String(),
			stage.ReferenceSnapshotDigest.String(),
			strconv.FormatInt(int64(stage.Timeout), 10),
			strconv.FormatUint(uint64(stage.MaximumAttempts), 10),
			canonicalJoin(dependencies...),
			canonicalJoin(artifactIdentities(stage.Inputs)...),
			budgetIdentity(stage.Budget),
		)
	}
	return identifiers.SHA256String(canonicalJoin(parts...))
}

func artifactIdentities(refs []artifacts.Ref) []string {
	identities := make([]string, 0, len(refs))
	for _, ref := range refs {
		identities = append(identities, canonicalJoin(
			ref.Digest.String(),
			strconv.FormatUint(ref.SizeBytes, 10),
			ref.MediaType,
			ref.LogicalKind,
			strconv.FormatUint(uint64(ref.SchemaVersion), 10),
		))
	}
	sort.Strings(identities)
	return identities
}

// budgetIdentity enumerates every budget dimension. A dimension omitted here
// would let two stages with different resource envelopes share a digest, so the
// list is exhaustive by intent rather than by convenience.
func budgetIdentity(budget runtime_authority.ExecutionBudget) string {
	return canonicalJoin(
		strconv.FormatUint(uint64(budget.CPUMillis), 10),
		strconv.FormatUint(budget.ResidentMemoryBytes, 10),
		strconv.FormatUint(budget.PinnedMemoryBytes, 10),
		strconv.FormatUint(budget.SharedMemoryBytes, 10),
		strconv.FormatUint(budget.LocalDiskBytes, 10),
		strconv.FormatUint(uint64(budget.OpenFileDescriptors), 10),
		strconv.FormatUint(uint64(budget.ObjectStoreRequests), 10),
		strconv.FormatUint(uint64(budget.QueuedOperations), 10),
		strconv.FormatUint(uint64(budget.ChildProcesses), 10),
		strconv.FormatUint(uint64(budget.CPUWorkerThreads), 10),
		strconv.FormatUint(budget.GPUMemoryEstimateBytes, 10),
		strconv.FormatUint(budget.CheckpointStagingBytes, 10),
		strconv.FormatUint(budget.TelemetrySpoolBytes, 10),
		strconv.FormatUint(budget.MaximumOutputBytes, 10),
	)
}

// CompileWorkload binds one stage attempt to a signed ticket and produces the
// envelope a worker executes.
//
// This is the generalization of control/ingestion's CompileWorkload: the same
// construction was already written there for ingestion plans, and every other
// stage kind needs it identically. The envelope's own Validate cross-checks
// every claim against the attempt, so a mismatch is caught here rather than at
// the node.
func CompileWorkload(
	workloadID string,
	runID string,
	jobID string,
	tenantID string,
	workspaceID string,
	attempt StageAttempt,
	ticket runtime_authority.ExecutionTicket,
	expectedOutputs []artifacts.Ref,
	resourceClass string,
	createdAt time.Time,
	now time.Time,
) (WorkloadEnvelope, error) {
	if err := attempt.Validate(); err != nil {
		return WorkloadEnvelope{}, err
	}
	if attempt.RunID != runID || attempt.JobID != jobID {
		return WorkloadEnvelope{}, invalid("stage_attempt_plan_mismatch", "stage attempt does not belong to the compiled plan", nil)
	}
	envelope := WorkloadEnvelope{
		WorkloadID:           workloadID,
		RunID:                runID,
		JobID:                jobID,
		StageID:              attempt.Spec.StageID,
		Attempt:              attempt.Attempt,
		TenantID:             tenantID,
		WorkspaceID:          workspaceID,
		ExecutionTicket:      ticket,
		Inputs:               attempt.Spec.Inputs,
		ExpectedOutputs:      expectedOutputs,
		ResolvedConfigDigest: attempt.Spec.ResolvedConfigDigest,
		ResourceClass:        resourceClass,
		CreatedAt:            createdAt,
		Deadline:             createdAt.Add(attempt.Spec.Timeout),
		StageKind:            attempt.Spec.Kind,
		Operation:            attempt.Spec.Operation,
	}
	if err := envelope.Validate(now); err != nil {
		return WorkloadEnvelope{}, err
	}
	return envelope, nil
}
