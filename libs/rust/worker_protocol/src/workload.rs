// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Canonical internal workload envelope shared by durable stage classes.

use crate::{BufferDescriptor, ExecutionTicket};
use mindclade_content_digest::Digest;
use mindclade_faults::{Code, Fault, FaultResult};
use mindclade_identifiers::ResourceId;

/// Rust projection of `mindclade.orchestration.v1.StageKind`.
///
/// The variant names track the proto value names exactly. They used to read
/// `Curation` and `Preprocessing` while the proto, Go (`control/orchestration`),
/// Python (`libs/python/worker_runtime`), and the TypeScript SDK all spell those
/// members `curate` and `preprocess`. Nothing caught it: this crate has no
/// protobuf dependency, so the taxonomy is a hand-written fourth copy that no
/// build step compares with the other three, and the two odd names had no
/// callers to fail. A decoder written against the wire enum would have had to
/// map `STAGE_KIND_CURATE` onto a differently spelled variant, which is how a
/// stage silently ends up classified as the wrong kind.
/// `tests/integration/cross_language/test_worker_protocol.py` now compares all
/// four copies mechanically.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum WorkloadKind {
    Ingestion,
    Curate,
    Preprocess,
    ReferenceBuild,
    BatchInference,
    Evaluation,
    Training,
    CheckpointTransfer,
    ArtifactTransfer,
    Rollout,
    Simulation,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct WorkloadEnvelope {
    pub workload_id: ResourceId,
    pub run_id: ResourceId,
    pub job_id: ResourceId,
    pub stage_id: ResourceId,
    pub attempt: u32,
    pub tenant_id: ResourceId,
    pub workspace_id: ResourceId,
    pub ticket: ExecutionTicket,
    pub kind: WorkloadKind,
    pub operation: String,
    pub inputs: Vec<BufferDescriptor>,
    pub expected_output_digests: Vec<Digest>,
    pub resolved_config_digest: Digest,
    pub resource_class: String,
    pub created_unix_millis: u64,
    pub deadline_unix_millis: u64,
}

impl WorkloadEnvelope {
    pub fn validate(&self, now_unix_millis: u64) -> FaultResult<()> {
        if self.workload_id.kind() != "workload"
            || self.run_id.kind() != "run"
            || self.job_id.kind() != "job"
            || self.stage_id.kind() != "stage"
            || self.tenant_id.kind() != "tenant"
            || self.workspace_id.kind() != "workspace"
        {
            return Err(Fault::invalid_argument(
                "workload envelope identifier kind is invalid",
            ));
        }
        if self.attempt == 0
            || self.operation.is_empty()
            || self.operation.len() > 256
            || self.resource_class.is_empty()
            || self.resource_class.len() > 128
            || self.created_unix_millis == 0
            || self.created_unix_millis >= self.deadline_unix_millis
            || now_unix_millis >= self.deadline_unix_millis
            || self.inputs.len() > 4_096
            || self.expected_output_digests.len() > 4_096
        {
            return Err(Fault::new(
                Code::InvalidArgument,
                "workload envelope is invalid or outside bounds",
            ));
        }
        // as_ref(), because `validate` takes &self and ok_or_else consumes its receiver —
        // without it this tries to move the stage id out of a borrowed ticket. The value is
        // only compared below, so a borrow is all this ever needed.
        let ticket_stage = self.ticket.claims.stage_id.as_ref().ok_or_else(|| {
            Fault::invalid_argument("execution ticket does not identify workload stage")
        })?;
        if self.resolved_config_digest != self.ticket.claims.resolved_config_digest
            || self.stage_id != *ticket_stage
            || self.attempt != self.ticket.claims.attempt
        {
            return Err(Fault::new(
                Code::PermissionDenied,
                "workload envelope does not match execution ticket",
            ));
        }
        for input in &self.inputs {
            input.validate(now_unix_millis)?;
        }
        Ok(())
    }
}
