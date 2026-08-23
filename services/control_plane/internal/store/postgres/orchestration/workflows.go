// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package orchestrationpostgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"go.mindclade.dev/control/orchestration"
	"go.mindclade.dev/libs/go/faults"
)

type workflowResult struct {
	workflow orchestration.Workflow
	replayed bool
}

// PutWorkflow publishes an immutable plan.
//
// Immutability is enforced against durable state, not against the caller's
// belief about it: the row is locked, and republishing the identical
// DefinitionDigest replays while a different digest under the same id is
// refused. Allowing the second case would silently change what an already
// running graph means, which is the one failure a compiled plan exists to make
// impossible.
//
// Only the Workflow is stored. CompiledWorkflow.Graph is a derived index whose
// fields are unexported, so it is rebuilt by GetWorkflow rather than persisted;
// storing a derived index would create a second copy that could disagree with
// the plan it indexes.
func (store *Store) PutWorkflow(ctx context.Context, compiled orchestration.CompiledWorkflow, now time.Time) (orchestration.Workflow, bool, error) {
	const operation = "orchestration.postgres.PutWorkflow"
	if err := store.validate(ctx, operation); err != nil {
		return orchestration.Workflow{}, false, err
	}
	if err := compiled.Workflow.ValidateIdentity(); err != nil {
		return orchestration.Workflow{}, false, err
	}
	if now.IsZero() {
		return orchestration.Workflow{}, false, domainError(ctx, faults.CodeInvalidArgument,
			"workflow_time_invalid", "workflow publication time is required", operation)
	}
	written := now.Round(0).UTC()
	document, err := marshalDocument(ctx, compiled.Workflow, operation)
	if err != nil {
		return orchestration.Workflow{}, false, err
	}
	schemaVersion, err := sqlUint(ctx, uint64(compiled.Workflow.SchemaVersion), "schema_version", operation)
	if err != nil {
		return orchestration.Workflow{}, false, err
	}
	result, err := runMutation(ctx, store, operation, func(txContext context.Context) (workflowResult, error) {
		existing, found, lookupErr := store.lockWorkflow(txContext, compiled.Workflow.ID, operation)
		if lookupErr != nil {
			return workflowResult{}, lookupErr
		}
		if found {
			if !existing.DefinitionDigest.Equal(compiled.Workflow.DefinitionDigest) {
				return workflowResult{}, domainError(txContext, faults.CodeConflict,
					"workflow_immutable", "a published workflow cannot be redefined", operation)
			}
			return workflowResult{workflow: existing, replayed: true}, nil
		}
		query := fmt.Sprintf(`INSERT INTO %s (
workflow_id,name,definition_digest,schema_version,stage_count,document,written_at
) VALUES ($1,$2,$3,$4,$5,$6::jsonb,$7)`, store.workflows)
		if _, execErr := store.executor(txContext).ExecContext(txContext, query,
			compiled.Workflow.ID, compiled.Workflow.Name, compiled.Workflow.DefinitionDigest.String(),
			schemaVersion, int64(len(compiled.Workflow.Stages)), document, written); execErr != nil {
			return workflowResult{}, provider(txContext, execErr, operation)
		}
		if emitErr := store.emitWorkflow(txContext, compiled.Workflow); emitErr != nil {
			return workflowResult{}, emitErr
		}
		return workflowResult{workflow: compiled.Workflow}, nil
	})
	if err != nil {
		return orchestration.Workflow{}, false, err
	}
	return result.workflow, result.replayed, nil
}

// GetWorkflow reconstructs the compiled plan.
//
// The graph is rebuilt from the stored workflow on every read. NewGraph
// revalidates the whole stage list -- acyclicity, resolvable dependencies,
// every stage spec -- so a row that was edited into something the compiler
// could never have produced fails here rather than reaching a reconciler.
func (store *Store) GetWorkflow(ctx context.Context, id string) (orchestration.CompiledWorkflow, error) {
	const operation = "orchestration.postgres.GetWorkflow"
	if err := store.validate(ctx, operation); err != nil {
		return orchestration.CompiledWorkflow{}, err
	}
	var document []byte
	err := store.executor(ctx).QueryRowContext(ctx,
		fmt.Sprintf(`SELECT document FROM %s WHERE workflow_id=$1`, store.workflows), id).Scan(&document)
	if errors.Is(err, sql.ErrNoRows) {
		return orchestration.CompiledWorkflow{}, domainError(ctx, faults.CodeNotFound,
			"workflow_not_found", "workflow was not found", operation)
	}
	if err != nil {
		return orchestration.CompiledWorkflow{}, provider(ctx, err, operation)
	}
	return decodeCompiledWorkflow(ctx, document, operation)
}

func (store *Store) lockWorkflow(ctx context.Context, id, operation string) (orchestration.Workflow, bool, error) {
	var document []byte
	err := store.executor(ctx).QueryRowContext(ctx,
		fmt.Sprintf(`SELECT document FROM %s WHERE workflow_id=$1 FOR UPDATE`, store.workflows), id).Scan(&document)
	if errors.Is(err, sql.ErrNoRows) {
		return orchestration.Workflow{}, false, nil
	}
	if err != nil {
		return orchestration.Workflow{}, false, provider(ctx, err, operation+".Lock")
	}
	// ValidateIdentity rather than Validate: a stored workflow is a published
	// one, and a row that lost its id, name, or digest is not a plan whose
	// immutability can be compared against anything.
	workflow, err := decodeDocument[identityValidatedWorkflow](ctx, document, operation)
	if err != nil {
		return orchestration.Workflow{}, false, err
	}
	return orchestration.Workflow(workflow), true, nil
}

func decodeCompiledWorkflow(ctx context.Context, document []byte, operation string) (orchestration.CompiledWorkflow, error) {
	workflow, err := decodeDocument[identityValidatedWorkflow](ctx, document, operation)
	if err != nil {
		return orchestration.CompiledWorkflow{}, err
	}
	graph, err := orchestration.NewGraph(orchestration.Workflow(workflow))
	if err != nil {
		return orchestration.CompiledWorkflow{}, internal(ctx, err, operation, "orchestration_workflow_graph_invalid")
	}
	return orchestration.CompiledWorkflow{Workflow: orchestration.Workflow(workflow), Graph: graph}, nil
}

// identityValidatedWorkflow adapts Workflow to decodeDocument's constraint.
// Workflow.Validate checks only the graph, because control/ingestion reuses it
// for a stage list that has no identity; a durable row must meet the stricter
// contract, so the alias routes Validate to ValidateIdentity.
type identityValidatedWorkflow orchestration.Workflow

func (workflow identityValidatedWorkflow) Validate() error {
	return orchestration.Workflow(workflow).ValidateIdentity()
}
