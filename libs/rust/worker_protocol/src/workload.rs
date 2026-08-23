// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Rust view of `mindclade.orchestration.v1.WorkloadEnvelope`.
//!
//! Per ADR-0026 this type *is* the wire message: one name, one field set, one meaning in every
//! language. It previously declared `inputs: Vec<BufferDescriptor>` and
//! `expected_output_digests: Vec<Digest>` where the wire declares `repeated ArtifactRef inputs`
//! and `repeated ArtifactRef expected_outputs`. Those are different concepts wearing the same
//! field name: an [`ArtifactRef`] is content identity that outlives every lease (ADR-0004),
//! while a [`BufferDescriptor`](crate::BufferDescriptor) is leased local placement that is
//! meaningless to the control plane. Nothing decoded the message, so nothing failed -- but
//! `services/node_agent` called the divergent type "the canonical workload envelope", and the
//! first decoder written against the wire would have had to invent the difference.
//!
//! Materialized buffers now travel *beside* the envelope, which is how the wire already models
//! the node hop (`mindclade.runtime.v1.RuntimeExecuteRequest` carries the ticket, the operation
//! and `repeated BufferDescriptor inputs` as siblings). [`WorkloadEnvelope::bind_materialized`]
//! is the seam that ties the two together, and it is the check the split made possible: a
//! buffer whose content digest the envelope never authorized is refused before it is read.

use crate::{BufferDescriptor, ExecutionTicket};
use mindclade_content_digest::Digest;
use mindclade_faults::{Code, Fault, FaultResult};
use mindclade_identifiers::ResourceId;

/// Maximum artifacts an envelope may declare on either side. Peer-controlled, so bounded.
const MAX_ARTIFACTS: usize = 4_096;
const MAX_MEDIA_TYPE_BYTES: usize = 255;
const MAX_LOGICAL_KIND_BYTES: usize = 128;

/// The stage taxonomy. Named `WorkloadKind` for historical reasons; the *values* are
/// `mindclade.orchestration.v1.StageKind` and `test_worker_protocol.py` pins them against the
/// proto, Go, Python and TypeScript spellings in all four languages at once.
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

/// Rust view of `mindclade.common.v1.ArtifactRef`: what the bytes are, never where they sit.
///
/// ADR-0004 keeps identity independent of placement, so this deliberately carries no URI,
/// provider or generation. Replication may add a location; it can never change a reference.
#[derive(Clone, Debug, Eq, PartialEq, Ord, PartialOrd)]
pub struct ArtifactRef {
    pub digest: Digest,
    pub size_bytes: u64,
    pub media_type: String,
    pub logical_kind: String,
    pub schema_version: u32,
}

impl ArtifactRef {
    pub fn validate(&self) -> FaultResult<()> {
        // `schema_version` is `uint32` on the wire and must be positive: proto3 cannot tell a
        // zero from an absent scalar, so accepting zero would let an unset field pass as a
        // declared contract version.
        if self.media_type.is_empty()
            || self.media_type.len() > MAX_MEDIA_TYPE_BYTES
            || !self.media_type.contains('/')
            || self.logical_kind.is_empty()
            || self.logical_kind.len() > MAX_LOGICAL_KIND_BYTES
            || self.schema_version == 0
        {
            return Err(Fault::invalid_argument("artifact reference is invalid"));
        }
        Ok(())
    }
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
    pub execution_ticket: ExecutionTicket,
    pub inputs: Vec<ArtifactRef>,
    pub expected_outputs: Vec<ArtifactRef>,
    pub resolved_config_digest: Digest,
    pub resource_class: String,
    pub created_unix_millis: u64,
    pub deadline_unix_millis: u64,
    pub stage_kind: WorkloadKind,
    pub operation: String,
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
            || self.inputs.len() > MAX_ARTIFACTS
            || self.expected_outputs.len() > MAX_ARTIFACTS
        {
            return Err(Fault::new(
                Code::InvalidArgument,
                "workload envelope is invalid or outside bounds",
            ));
        }
        self.match_ticket()?;
        for artifact in self.inputs.iter().chain(&self.expected_outputs) {
            artifact.validate()?;
        }
        Ok(())
    }

    /// Cross-check the unsigned envelope against the signed claims it travels with.
    ///
    /// Everything here is duplicated between the two, and only the ticket is signed. Rust used
    /// to compare three of the seven (config digest, stage, attempt), so an envelope could name
    /// tenant A while its signed authority named tenant B and validation passed -- Go's
    /// `control/orchestration.WorkloadEnvelope.Validate` compared all seven, which is the
    /// asymmetry this closes.
    fn match_ticket(&self) -> FaultResult<()> {
        let claims = &self.execution_ticket.claims;
        // as_ref(), because `validate` takes &self and ok_or_else consumes its receiver --
        // without it this tries to move the id out of a borrowed ticket. The values are only
        // compared below, so a borrow is all this ever needed.
        let matches = |claimed: Option<&ResourceId>, expected: &ResourceId| {
            claimed.is_some_and(|value| value == expected)
        };
        if self.resolved_config_digest != claims.resolved_config_digest
            || self.attempt != claims.attempt
            || self.tenant_id != claims.tenant_id
            || self.workspace_id != claims.workspace_id
            || !matches(claims.run_id.as_ref(), &self.run_id)
            || !matches(claims.job_id.as_ref(), &self.job_id)
            || !matches(claims.stage_id.as_ref(), &self.stage_id)
        {
            return Err(Fault::new(
                Code::PermissionDenied,
                "workload envelope does not match execution ticket",
            ));
        }
        Ok(())
    }

    /// Bind node-materialized buffers to the artifacts this envelope authorized.
    ///
    /// The node receives placement (`BufferDescriptor`) for inputs the control plane named by
    /// identity (`ArtifactRef`). Nothing previously connected the two: the node validated that
    /// each buffer's lease was live and then read it, so a buffer for content the envelope
    /// never listed was indistinguishable from one it did. Splitting the two concepts is what
    /// makes this check expressible at all.
    pub fn bind_materialized(
        &self,
        materialized: &[BufferDescriptor],
        now_unix_millis: u64,
    ) -> FaultResult<()> {
        if materialized.len() > MAX_ARTIFACTS {
            return Err(Fault::new(
                Code::ResourceExhausted,
                "materialized input count exceeds limit",
            ));
        }
        for descriptor in materialized {
            descriptor.validate(now_unix_millis)?;
            if !self
                .inputs
                .iter()
                .any(|artifact| artifact.digest == descriptor.digest)
            {
                return Err(Fault::new(
                    Code::PermissionDenied,
                    "materialized buffer is not an authorized workload input",
                ));
            }
        }
        Ok(())
    }
}
