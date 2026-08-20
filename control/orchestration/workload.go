// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package orchestration

import (
	"go.mindclade.dev/control/artifacts"
	"go.mindclade.dev/control/runtime_authority"
	"go.mindclade.dev/libs/go/identifiers"
	"time"
)

type WorkloadEnvelope struct {
	WorkloadID, RunID, JobID, StageID string
	Attempt                           uint32
	TenantID, WorkspaceID             string
	ExecutionTicket                   runtime_authority.ExecutionTicket
	Inputs, ExpectedOutputs           []artifacts.Ref
	ResolvedConfigDigest              identifiers.Digest
	ResourceClass                     string
	CreatedAt, Deadline               time.Time
	StageKind                         StageKind
	Operation                         string
}

func (w WorkloadEnvelope) Validate(now time.Time) error {
	for name, value := range map[string]string{"workload_id": w.WorkloadID, "run_id": w.RunID, "job_id": w.JobID, "stage_id": w.StageID, "tenant_id": w.TenantID, "workspace_id": w.WorkspaceID} {
		if _, err := identifiers.ParseID(value); err != nil {
			return invalid(name+"_invalid", name+" must be canonical", err)
		}
	}
	if w.Attempt == 0 || !w.StageKind.Valid() || w.Operation == "" || w.ResourceClass == "" || !w.ResolvedConfigDigest.Valid() || w.CreatedAt.IsZero() || !w.Deadline.After(now) || !w.Deadline.After(w.CreatedAt) {
		return invalid("workload_invalid", "workload envelope is incomplete or expired", nil)
	}
	if err := w.ExecutionTicket.Claims.ValidateStatic(); err != nil {
		return err
	}
	claims := w.ExecutionTicket.Claims
	if claims.RunID != w.RunID || claims.JobID != w.JobID || claims.StageID != w.StageID ||
		claims.Attempt != w.Attempt || claims.TenantID != w.TenantID || claims.WorkspaceID != w.WorkspaceID ||
		!claims.ResolvedConfigDigest.Equal(w.ResolvedConfigDigest) {
		return invalid("workload_ticket_mismatch", "workload envelope does not match execution ticket", nil)
	}
	if now.Before(w.ExecutionTicket.Claims.NotBefore) || !now.Before(w.ExecutionTicket.Claims.Expires) || now.After(w.ExecutionTicket.Claims.Deadline) {
		return invalid("workload_ticket_inactive", "workload execution ticket is not active", nil)
	}
	for _, ref := range append(append([]artifacts.Ref{}, w.Inputs...), w.ExpectedOutputs...) {
		if err := ref.Validate(); err != nil {
			return err
		}
	}
	return nil
}
