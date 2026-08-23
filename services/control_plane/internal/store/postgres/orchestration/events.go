// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package orchestrationpostgres

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"go.mindclade.dev/control/orchestration"
	"go.mindclade.dev/libs/go/audit"
	"go.mindclade.dev/libs/go/auth"
	"go.mindclade.dev/libs/go/identifiers"
	"go.mindclade.dev/libs/go/requestmeta"
)

const (
	// The topics are the domain's published join contract. They are versioned
	// in the name rather than only in the payload so a consumer that cannot
	// read v2 keeps reading v1 instead of silently mis-parsing it.
	WorkflowEventTopic     = "control.orchestration.workflow.v1"
	StageEventTopic        = "control.orchestration.stage.v1"
	AttemptEventTopic      = "control.orchestration.attempt.v1"
	CancellationEventTopic = "control.orchestration.cancellation.v1"

	WorkflowTargetType     = "orchestration_workflow"
	StageTargetType        = "orchestration_stage"
	AttemptTargetType      = "orchestration_attempt"
	CancellationTargetType = "orchestration_cancellation"

	// The lineage headers let a maintenance probe join the relational audit
	// columns to the outbox row without decoding either the JSON body or the
	// bytea payload. They mirror the admission adapter's header names on
	// purpose: one probe reads both domains.
	LineageAuditEventIDHeader    = "audit-event-id"
	LineageAuditActionHeader     = "audit-action"
	LineageTargetTypeHeader      = "audit-target-type"
	LineageTargetIDHeader        = "audit-target-id"
	LineageResourceVersionHeader = "resource-version"
	LineageSchemaVersionHeader   = "schema-version"

	EventSchemaVersion = 1
)

// workflowEvent is deliberately not the workflow. A compiled plan carries up to
// MaximumStageCount stage specs with their inputs and budgets, and publishing
// that on every workflow publication would put a megabyte-scale body in the
// outbox for a fact the consumer can already resolve by id. The digest is the
// plan's identity; a consumer that needs the plan calls GetWorkflow.
type workflowEvent struct {
	SchemaVersion    int    `json:"schema_version"`
	WorkflowID       string `json:"workflow_id"`
	Name             string `json:"name"`
	DefinitionDigest string `json:"definition_digest"`
	WorkflowSchema   uint32 `json:"workflow_schema_version"`
	StageCount       int    `json:"stage_count"`
}

type stageEvent struct {
	SchemaVersion   int    `json:"schema_version"`
	RunID           string `json:"run_id"`
	JobID           string `json:"job_id"`
	StageID         string `json:"stage_id"`
	State           string `json:"state"`
	Attempts        uint32 `json:"attempts"`
	UpdatedAt       string `json:"updated_at"`
	ResourceVersion string `json:"resource_version"`
}

type attemptEvent struct {
	SchemaVersion     int    `json:"schema_version"`
	RunID             string `json:"run_id"`
	JobID             string `json:"job_id"`
	StageID           string `json:"stage_id"`
	Attempt           uint32 `json:"attempt"`
	State             string `json:"state"`
	FencingToken      uint64 `json:"fencing_token"`
	ExecutionTicketID string `json:"execution_ticket_id"`
	ExecutionClass    string `json:"execution_class"`
	Sequence          uint64 `json:"sequence"`
	FailureCode       string `json:"failure_code,omitempty"`
	FailureReason     string `json:"failure_reason,omitempty"`
	UpdatedAt         string `json:"updated_at"`
	ResourceVersion   string `json:"resource_version"`
}

type cancellationEvent struct {
	SchemaVersion    int    `json:"schema_version"`
	RunID            string `json:"run_id"`
	StageID          string `json:"stage_id,omitempty"`
	Origin           string `json:"origin"`
	Reason           string `json:"reason"`
	IdempotencyScope string `json:"idempotency_scope"`
	IdempotencyKey   string `json:"idempotency_key"`
	RequestedAt      string `json:"requested_at"`
}

func (store *Store) emitWorkflow(ctx context.Context, workflow orchestration.Workflow) error {
	const operation = "orchestration.postgres.emitWorkflow"
	id, err := identifiers.ParseID(workflow.ID)
	if err != nil {
		return internal(ctx, err, operation, "orchestration_event_identity_invalid")
	}
	payload, err := json.Marshal(workflowEvent{
		SchemaVersion: EventSchemaVersion, WorkflowID: workflow.ID, Name: workflow.Name,
		DefinitionDigest: workflow.DefinitionDigest.String(), WorkflowSchema: workflow.SchemaVersion,
		StageCount: len(workflow.Stages),
	})
	if err != nil {
		return internal(ctx, err, operation, "orchestration_event_encoding_failed")
	}
	fields := audit.Fields{
		"workflow_id": workflow.ID, "definition_digest": workflow.DefinitionDigest.String(),
		"stage_count": strconv.Itoa(len(workflow.Stages)),
	}
	return store.emit(ctx, "orchestration.workflow.put", WorkflowTargetType, id, workflow.Name,
		WorkflowEventTopic, workflow.ID, "", payload, fields)
}

func (store *Store) emitStage(ctx context.Context, action string, record orchestration.StageRecord) error {
	const operation = "orchestration.postgres.emitStage"
	id, err := identifiers.ParseID(record.StageID)
	if err != nil {
		return internal(ctx, err, operation, "orchestration_event_identity_invalid")
	}
	payload, err := json.Marshal(stageEvent{
		SchemaVersion: EventSchemaVersion, RunID: record.RunID, JobID: record.JobID,
		StageID: record.StageID, State: string(record.State), Attempts: record.Attempts,
		UpdatedAt: record.UpdatedAt.UTC().Format(time.RFC3339Nano), ResourceVersion: record.Version.String(),
	})
	if err != nil {
		return internal(ctx, err, operation, "orchestration_event_encoding_failed")
	}
	fields := audit.Fields{
		"run_id": record.RunID, "job_id": record.JobID, "stage_id": record.StageID,
		"state": string(record.State), "attempts": strconv.FormatUint(uint64(record.Attempts), 10),
		"resource_version": record.Version.String(),
	}
	// The run is the partition key for every stage and attempt event: ordering
	// only has to hold within one run, and partitioning by stage would let two
	// stages of the same run be observed out of order by a consumer that
	// rebuilds run state.
	return store.emit(ctx, action, StageTargetType, id, record.RunID,
		StageEventTopic, record.RunID, record.Version.String(), payload, fields)
}

func (store *Store) emitAttempt(ctx context.Context, action string, record orchestration.AttemptRecord) error {
	const operation = "orchestration.postgres.emitAttempt"
	id, err := identifiers.ParseID(record.StageID)
	if err != nil {
		return internal(ctx, err, operation, "orchestration_event_identity_invalid")
	}
	payload, err := json.Marshal(attemptEvent{
		SchemaVersion: EventSchemaVersion, RunID: record.RunID, JobID: record.JobID,
		StageID: record.StageID, Attempt: record.Attempt, State: string(record.State),
		FencingToken: record.FencingToken, ExecutionTicketID: record.ExecutionTicketID,
		ExecutionClass: record.ExecutionClass, Sequence: record.Sequence,
		FailureCode: string(record.Failure.Code), FailureReason: record.Failure.Reason,
		UpdatedAt: record.UpdatedAt.UTC().Format(time.RFC3339Nano), ResourceVersion: record.Version.String(),
	})
	if err != nil {
		return internal(ctx, err, operation, "orchestration_event_encoding_failed")
	}
	fields := audit.Fields{
		"run_id": record.RunID, "job_id": record.JobID, "stage_id": record.StageID,
		"attempt": strconv.FormatUint(uint64(record.Attempt), 10), "state": string(record.State),
		"resource_version": record.Version.String(),
	}
	return store.emit(ctx, action, AttemptTargetType, id, record.RunID,
		AttemptEventTopic, record.RunID, record.Version.String(), payload, fields)
}

func (store *Store) emitCancellation(ctx context.Context, intent orchestration.CancellationIntent) error {
	const operation = "orchestration.postgres.emitCancellation"
	id, err := identifiers.ParseID(intent.RunID)
	if err != nil {
		return internal(ctx, err, operation, "orchestration_event_identity_invalid")
	}
	payload, err := json.Marshal(cancellationEvent{
		SchemaVersion: EventSchemaVersion, RunID: intent.RunID, StageID: intent.StageID,
		Origin: string(intent.Origin), Reason: intent.Reason,
		IdempotencyScope: intent.Idempotency.Scope.String(), IdempotencyKey: intent.Idempotency.Key.String(),
		RequestedAt: intent.RequestedAt.UTC().Format(time.RFC3339Nano),
	})
	if err != nil {
		return internal(ctx, err, operation, "orchestration_event_encoding_failed")
	}
	fields := audit.Fields{
		"run_id": intent.RunID, "stage_id": intent.StageID, "origin": string(intent.Origin),
		"idempotency_scope": intent.Idempotency.Scope.String(),
	}
	return store.emit(ctx, "orchestration.cancellation.record", CancellationTargetType, id, intent.RunID,
		CancellationEventTopic, intent.RunID, "", payload, fields)
}

// emit appends the audit record and the outbox message. Both use the ambient
// transaction, so this call is part of the caller's commit rather than a
// separate write that a crash could leave half-applied.
func (store *Store) emit(
	ctx context.Context, action, targetType string, id identifiers.ID, name,
	topic, partitionKey, resourceVersion string, payload []byte, fields audit.Fields,
) error {
	target, err := audit.NewTarget(targetType, audit.WithTargetID(id), audit.WithTargetName(name))
	if err != nil {
		return err
	}
	actor := store.actor
	if principal, ok := auth.PrincipalFromContext(ctx); ok {
		actor, err = audit.ActorFromPrincipal(principal)
		if err != nil {
			return err
		}
	}
	metadata, hasMetadata := requestmeta.FromContext(ctx)
	options := []audit.EventOption{audit.WithFields(fields)}
	if hasMetadata {
		options = append(options, audit.WithRequestMetadata(metadata))
	}
	event, err := store.audits.Create(audit.MustParseAction(action), actor, target,
		audit.OutcomeSucceeded, options...)
	if err != nil {
		return err
	}
	headers := map[string]string{
		LineageSchemaVersionHeader: strconv.Itoa(EventSchemaVersion),
		LineageAuditEventIDHeader:  event.ID().String(),
		LineageAuditActionHeader:   event.Action().String(),
		LineageTargetTypeHeader:    target.Type(),
		LineageTargetIDHeader:      id.String(),
	}
	// A workflow and a cancellation intent are immutable facts and carry no
	// resource version. Emitting the header with an empty value would make a
	// lineage probe unable to tell "no version" from "version lost".
	if resourceVersion != "" {
		headers[LineageResourceVersionHeader] = resourceVersion
	}
	if err := audit.Record(ctx, store.recorder, event); err != nil {
		return err
	}
	message, err := store.events.Create(topic, partitionKey, "application/json", payload,
		headers, metadata, time.Time{})
	if err != nil {
		return err
	}
	return store.messages.Append(ctx, message)
}
