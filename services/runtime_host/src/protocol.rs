// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Protobuf transport conversion for the runtime-host control edge.
//!
//! Wire messages are generated from `protocols/`; this module converts them
//! into the bounded, validated runtime-domain types before any policy or
//! resource decision is made.

use mindclade_bytes_io::ByteRange;
use mindclade_content_digest::Digest;
use mindclade_faults::{Fault, FaultResult};
use mindclade_identifiers::ResourceId;
use mindclade_protocols::runtime::v1 as wire;
use mindclade_runtime_core::{FencingToken, ResourceKind, ResourceVector};
use mindclade_worker_protocol::{
    ArtifactGrant, BufferAccess, BufferDescriptor, BufferTransport, DetachedSignature,
    ExecutionBudget, ExecutionTicket, ExecutionTicketClaims, RevocationSnapshot,
    RevocationSnapshotClaims, WorkerState, WorkerStatus,
};
use std::collections::BTreeSet;
use std::str::FromStr;

pub fn execution_ticket(message: wire::ExecutionTicket) -> FaultResult<ExecutionTicket> {
    let claims = message
        .claims
        .ok_or_else(|| Fault::invalid_argument("execution ticket claims are missing"))?;
    let signature = message
        .signature
        .ok_or_else(|| Fault::invalid_argument("execution ticket signature is missing"))?;
    let artifacts = claims
        .artifacts
        .ok_or_else(|| Fault::invalid_argument("execution ticket artifact grant is missing"))?;
    let budget = claims
        .budget
        .ok_or_else(|| Fault::invalid_argument("execution ticket budget is missing"))?;

    Ok(ExecutionTicket {
        claims: ExecutionTicketClaims {
            ticket_id: parse_id(&claims.ticket_id, "ticket")?,
            issuer: claims.issuer,
            tenant_id: parse_id(&claims.tenant_id, "tenant")?,
            workspace_id: parse_id(&claims.workspace_id, "workspace")?,
            run_id: parse_optional_id(&claims.run_id, "run")?,
            job_id: parse_optional_id(&claims.job_id, "job")?,
            stage_id: parse_optional_id(&claims.stage_id, "stage")?,
            request_id: parse_optional_id(&claims.request_id, "request")?,
            attempt: claims.attempt,
            fencing_token: FencingToken::new(claims.fencing_token)?,
            model_bundle: parse_optional_digest(&claims.model_bundle_digest)?,
            engine_bundle: parse_optional_digest(&claims.engine_bundle_digest)?,
            resolved_config_digest: parse_digest(&claims.resolved_config_digest)?,
            reference_snapshot: parse_optional_digest(&claims.reference_snapshot_digest)?,
            artifacts: artifact_grant(artifacts)?,
            budget: execution_budget(budget),
            execution_class: claims.execution_class,
            accelerator_capability: claims.accelerator_capability,
            not_before_unix_millis: claims.not_before_unix_millis,
            deadline_unix_millis: claims.deadline_unix_millis,
            expires_unix_millis: claims.expires_unix_millis,
            policy_epoch: claims.policy_epoch,
            route_snapshot_version: claims.route_snapshot_version,
            revocation_epoch: claims.revocation_epoch,
            idempotency_key: claims.idempotency_key,
        },
        signature: detached_signature(signature),
    })
}

pub fn revocation_snapshot(message: wire::RevocationSnapshot) -> FaultResult<RevocationSnapshot> {
    let claims = message
        .claims
        .ok_or_else(|| Fault::invalid_argument("revocation snapshot claims are missing"))?;
    let signature = message
        .signature
        .ok_or_else(|| Fault::invalid_argument("revocation snapshot signature is missing"))?;
    Ok(RevocationSnapshot {
        claims: RevocationSnapshotClaims {
            epoch: claims.epoch,
            created_unix_millis: claims.created_unix_millis,
            expires_unix_millis: claims.expires_unix_millis,
            revoked_grant_ids: claims.revoked_grant_ids.into_iter().collect(),
            revoked_ticket_ids: claims.revoked_ticket_ids.into_iter().collect(),
            revoked_deployment_ids: claims.revoked_deployment_ids.into_iter().collect(),
            revoked_bundle_digests: claims.revoked_bundle_digests.into_iter().collect(),
        },
        signature: detached_signature(signature),
    })
}

pub fn buffer_descriptor(message: wire::BufferDescriptor) -> FaultResult<BufferDescriptor> {
    let access = match wire::AccessMode::try_from(message.access_mode).ok() {
        Some(wire::AccessMode::ReadOnly) => BufferAccess::ReadOnly,
        Some(wire::AccessMode::ReadWrite) => BufferAccess::ReadWrite,
        _ => return Err(Fault::invalid_argument("buffer access mode is invalid")),
    };
    let transport = match wire::Transport::try_from(message.transport).ok() {
        Some(wire::Transport::SharedMemory) => BufferTransport::SharedMemory,
        Some(wire::Transport::FileDescriptor) => BufferTransport::FileDescriptor,
        Some(wire::Transport::LocalFile) => BufferTransport::LocalFile,
        Some(wire::Transport::ArtifactRef) => BufferTransport::Artifact,
        _ => return Err(Fault::invalid_argument("buffer transport is invalid")),
    };
    Ok(BufferDescriptor {
        segment_id: message.segment_id,
        generation: message.generation,
        range: ByteRange::new(message.offset_bytes, message.length_bytes)?,
        element_type: message.element_type,
        shape: message.shape,
        digest: parse_digest(&message.content_digest)?,
        owner_process: message.owner_process,
        lease_expires_unix_millis: message.lease_expires_unix_millis,
        access,
        transport,
        locator: message.locator,
    })
}

pub fn worker_status(message: &WorkerStatus) -> wire::WorkerStatus {
    wire::WorkerStatus {
        sequence: message.sequence,
        ticket_id: message.ticket_id.clone(),
        fencing_token: message.fencing_token.get(),
        state: worker_state(message.state) as i32,
        observed_unix_millis: message.observed_unix_millis,
        message: message.message.clone(),
        outputs: message.outputs.iter().map(buffer_descriptor_wire).collect(),
        diagnostic_artifact_digest: message
            .diagnostic_artifact
            .map(|digest| digest.to_string())
            .unwrap_or_default(),
    }
}

fn buffer_descriptor_wire(message: &BufferDescriptor) -> wire::BufferDescriptor {
    wire::BufferDescriptor {
        segment_id: message.segment_id.clone(),
        generation: message.generation,
        offset_bytes: message.range.start(),
        length_bytes: message.range.length(),
        element_type: message.element_type.clone(),
        shape: message.shape.clone(),
        content_digest: message.digest.to_string(),
        owner_process: message.owner_process.clone(),
        lease_expires_unix_millis: message.lease_expires_unix_millis,
        access_mode: match message.access {
            BufferAccess::ReadOnly => wire::AccessMode::ReadOnly as i32,
            BufferAccess::ReadWrite => wire::AccessMode::ReadWrite as i32,
        },
        transport: match message.transport {
            BufferTransport::SharedMemory => wire::Transport::SharedMemory as i32,
            BufferTransport::FileDescriptor => wire::Transport::FileDescriptor as i32,
            BufferTransport::LocalFile => wire::Transport::LocalFile as i32,
            BufferTransport::Artifact => wire::Transport::ArtifactRef as i32,
        },
        locator: message.locator.clone(),
    }
}

fn execution_budget(message: wire::ExecutionBudget) -> ExecutionBudget {
    let resources = ResourceVector::new()
        .set(ResourceKind::CpuMillis, u64::from(message.cpu_millis))
        .set(
            ResourceKind::ResidentMemoryBytes,
            message.resident_memory_bytes,
        )
        .set(ResourceKind::PinnedMemoryBytes, message.pinned_memory_bytes)
        .set(ResourceKind::SharedMemoryBytes, message.shared_memory_bytes)
        .set(ResourceKind::LocalDiskBytes, message.local_disk_bytes)
        .set(
            ResourceKind::OpenFileDescriptors,
            u64::from(message.open_file_descriptors),
        )
        .set(
            ResourceKind::ObjectStoreRequests,
            u64::from(message.object_store_requests),
        )
        .set(
            ResourceKind::QueuedRequests,
            u64::from(message.queued_operations),
        )
        .set(ResourceKind::Processes, u64::from(message.child_processes))
        .set(
            ResourceKind::CpuThreads,
            u64::from(message.cpu_worker_threads),
        )
        .set(
            ResourceKind::GpuMemoryEstimateBytes,
            message.gpu_memory_estimate_bytes,
        )
        .set(
            ResourceKind::CheckpointStagingBytes,
            message.checkpoint_staging_bytes,
        )
        .set(
            ResourceKind::TelemetrySpoolBytes,
            message.telemetry_spool_bytes,
        )
        .set(
            ResourceKind::MaximumOutputBytes,
            message.maximum_output_bytes,
        );
    ExecutionBudget {
        resources,
        maximum_output_bytes: message.maximum_output_bytes,
    }
}

fn artifact_grant(message: wire::ArtifactGrant) -> FaultResult<ArtifactGrant> {
    let readable_digests = message
        .readable_digests
        .into_iter()
        .map(|value| parse_digest(&value))
        .collect::<FaultResult<BTreeSet<_>>>()?;
    Ok(ArtifactGrant {
        readable_digests,
        writable_namespaces: message.writable_namespaces.into_iter().collect(),
        maximum_read_bytes: message.maximum_read_bytes,
        maximum_write_bytes: message.maximum_write_bytes,
        allow_range_reads: message.allow_range_reads,
        allow_multipart_writes: message.allow_multipart_writes,
    })
}

fn detached_signature(message: wire::DetachedSignature) -> DetachedSignature {
    DetachedSignature {
        algorithm: message.algorithm,
        key_id: message.key_id,
        value: message.value,
    }
}

fn parse_id(value: &str, expected_kind: &str) -> FaultResult<ResourceId> {
    let id = ResourceId::parse(value).map_err(|error| {
        Fault::invalid_argument("runtime resource id is invalid").with_source(error)
    })?;
    if id.kind() != expected_kind {
        return Err(Fault::invalid_argument(
            "runtime resource id has unexpected kind",
        ));
    }
    Ok(id)
}

fn parse_optional_id(value: &str, expected_kind: &str) -> FaultResult<Option<ResourceId>> {
    if value.is_empty() {
        Ok(None)
    } else {
        parse_id(value, expected_kind).map(Some)
    }
}

fn parse_digest(value: &str) -> FaultResult<Digest> {
    Digest::from_str(value)
        .map_err(|error| Fault::invalid_argument("runtime digest is invalid").with_source(error))
}

fn parse_optional_digest(value: &str) -> FaultResult<Option<Digest>> {
    if value.is_empty() {
        Ok(None)
    } else {
        parse_digest(value).map(Some)
    }
}

fn worker_state(state: WorkerState) -> wire::WorkerState {
    match state {
        WorkerState::Created => wire::WorkerState::Created,
        WorkerState::Starting => wire::WorkerState::Starting,
        WorkerState::Ready => wire::WorkerState::Ready,
        WorkerState::Leased => wire::WorkerState::Leased,
        WorkerState::Running => wire::WorkerState::Running,
        WorkerState::Draining => wire::WorkerState::Draining,
        WorkerState::Committing => wire::WorkerState::Committing,
        WorkerState::Completed => wire::WorkerState::Completed,
        WorkerState::Recovering => wire::WorkerState::Recovering,
        WorkerState::Cancelling => wire::WorkerState::Cancelling,
        WorkerState::Cancelled => wire::WorkerState::Cancelled,
        WorkerState::Failed => wire::WorkerState::Failed,
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn rejects_unspecified_buffer_enums() {
        let descriptor = wire::BufferDescriptor {
            segment_id: "seg".into(),
            generation: 1,
            offset_bytes: 0,
            length_bytes: 1,
            element_type: "u8".into(),
            shape: vec![1],
            content_digest: format!("sha256:{}", "0".repeat(64)),
            owner_process: "worker".into(),
            lease_expires_unix_millis: 10,
            access_mode: wire::AccessMode::Unspecified as i32,
            transport: wire::Transport::LocalFile as i32,
            locator: "/tmp/value".into(),
        };
        assert!(buffer_descriptor(descriptor).is_err());
    }
}
