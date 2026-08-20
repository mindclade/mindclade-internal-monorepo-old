// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Built-in ticketed stage adapters owned by the node agent.
//!
//! These adapters deliberately cover only node/data-plane mechanics.  They do
//! not interpret scientific features, training state, or model semantics.

use crate::checkpoint_transfer::CheckpointTransfer;
use crate::data_stream::StreamWorker;
use crate::{ProviderSource, StageContext, StageExecutor, StageFuture, StageResult};
use mindclade_bytes_io::ByteRange;
use mindclade_content_digest::{Digest, hash_bytes};
use mindclade_data_stream::Shard;
use mindclade_faults::{Code, Fault, FaultResult};
use mindclade_identifiers::Name;
use mindclade_ipc_os::BulkBufferBroker;
use mindclade_object_store::ObjectPath;
use mindclade_worker_protocol::{BufferAccess, BufferDescriptor, BufferTransport, ExecutionTicket};
use std::sync::Arc;

pub const CHECKPOINT_COPY_OPERATION: &str = "checkpoint.copy";
pub const DATA_STREAM_FETCH_OPERATION: &str = "data-stream.fetch";

/// Copies ticket-readable checkpoint components through the node staging
/// budget and republishes them through a ticket-writable namespace.
///
/// This is the reusable implementation behind the planned checkpoint-transfer
/// worker.  A standalone daemon is unnecessary while node-agent owns the same
/// ticket, budget, cache, and provider lifecycle.
#[derive(Debug)]
pub struct CheckpointCopyExecutor {
    transfer: CheckpointTransfer,
    source: ProviderSource,
    output_namespace: String,
    owner_process: String,
}

impl CheckpointCopyExecutor {
    pub fn new(
        transfer: CheckpointTransfer,
        source: ProviderSource,
        output_namespace: impl Into<String>,
        owner_process: impl Into<String>,
    ) -> FaultResult<Self> {
        let output_namespace = output_namespace.into();
        let owner_process = owner_process.into();
        validate_namespace(&output_namespace)?;
        validate_owner(&owner_process)?;
        Ok(Self {
            transfer,
            source,
            output_namespace,
            owner_process,
        })
    }
}

impl StageExecutor for CheckpointCopyExecutor {
    fn execute<'a>(
        &'a self,
        operation: &'a str,
        inputs: &'a [BufferDescriptor],
        ticket: &'a ExecutionTicket,
        context: &'a StageContext,
    ) -> StageFuture<'a> {
        Box::pin(async move {
            if operation != CHECKPOINT_COPY_OPERATION {
                return Err(Fault::new(
                    Code::Unimplemented,
                    "checkpoint executor does not implement requested operation",
                ));
            }
            if inputs.is_empty() || inputs.len() > 1_024 {
                return Err(Fault::new(
                    Code::ResourceExhausted,
                    "checkpoint transfer input count is outside bounds",
                ));
            }

            let mut outputs = Vec::with_capacity(inputs.len());
            for input in inputs {
                let now = context.ensure_active()?;
                require_artifact_input(input)?;
                let staged = self
                    .transfer
                    .stage_component(&self.source, ticket, input.digest, now)
                    .await?;
                let length = u64::try_from(staged.bytes().len()).map_err(|_| {
                    Fault::new(Code::OutOfRange, "checkpoint component length exceeds u64")
                })?;
                if input.range.start() != 0 || input.range.length() != length {
                    return Err(Fault::data_loss(
                        "checkpoint descriptor length does not match immutable component",
                    ));
                }

                let published = self
                    .transfer
                    .publish_component(
                        &self.source,
                        ticket,
                        &self.output_namespace,
                        staged.bytes().clone(),
                        context.ensure_active()?,
                    )
                    .await?;
                if published != staged.digest() {
                    return Err(Fault::data_loss(
                        "checkpoint transfer changed content digest",
                    ));
                }
                outputs.push(artifact_descriptor(
                    published,
                    length,
                    ticket,
                    &self.owner_process,
                    context.ensure_active()?,
                )?);
            }

            Ok(StageResult {
                outputs,
                message: format!("copied {} checkpoint component(s)", inputs.len()),
            })
        })
    }
}

/// Fetches immutable ticket-authorized dataset shards and materializes them as
/// bounded local bulk buffers for a downstream Python/training worker.
#[derive(Debug)]
pub struct DataStreamFetchExecutor {
    worker: StreamWorker,
    source: ProviderSource,
    bulk: Arc<BulkBufferBroker>,
    owner_process: String,
}

impl DataStreamFetchExecutor {
    pub fn new(
        worker: StreamWorker,
        source: ProviderSource,
        bulk: Arc<BulkBufferBroker>,
        owner_process: impl Into<String>,
    ) -> FaultResult<Self> {
        worker.validate()?;
        let owner_process = owner_process.into();
        validate_owner(&owner_process)?;
        Ok(Self {
            worker,
            source,
            bulk,
            owner_process,
        })
    }

    #[must_use]
    pub fn release(&self, segment_id: &str) -> bool {
        self.bulk.release(segment_id)
    }

    #[must_use]
    pub fn reap_expired(&self, now_unix_millis: u64) -> usize {
        self.bulk.reap_expired(now_unix_millis)
    }
}

impl StageExecutor for DataStreamFetchExecutor {
    fn execute<'a>(
        &'a self,
        operation: &'a str,
        inputs: &'a [BufferDescriptor],
        ticket: &'a ExecutionTicket,
        context: &'a StageContext,
    ) -> StageFuture<'a> {
        Box::pin(async move {
            if operation != DATA_STREAM_FETCH_OPERATION {
                return Err(Fault::new(
                    Code::Unimplemented,
                    "data-stream executor does not implement requested operation",
                ));
            }
            if inputs.is_empty() || inputs.len() > 4_096 {
                return Err(Fault::new(
                    Code::ResourceExhausted,
                    "data-stream input count is outside bounds",
                ));
            }

            let mut outputs = Vec::with_capacity(inputs.len());
            for (index, input) in inputs.iter().enumerate() {
                let now = context.ensure_active()?;
                require_artifact_input(input)?;
                if input.range.start() != 0 {
                    return Err(Fault::invalid_argument(
                        "provider shard descriptor must cover the complete artifact",
                    ));
                }
                let shard = shard_from_descriptor(index, input)?;
                let prefetched = self
                    .worker
                    .fetch_provider_shard(&self.source, ticket, index, shard, now)
                    .await?;
                context.ensure_active()?;

                let name = segment_name(ticket, index, prefetched.shard.digest);
                let descriptor = self.bulk.publish(
                    &name,
                    &prefetched.bytes,
                    ticket.claims.fencing_token.get(),
                    &self.owner_process,
                    ticket.claims.expires_unix_millis,
                    now,
                )?;
                outputs.push(descriptor);
            }

            Ok(StageResult {
                outputs,
                message: format!("materialized {} data shard(s)", inputs.len()),
            })
        })
    }
}

fn require_artifact_input(input: &BufferDescriptor) -> FaultResult<()> {
    if input.transport != BufferTransport::Artifact || input.access != BufferAccess::ReadOnly {
        return Err(Fault::invalid_argument(
            "node transfer input must be a read-only artifact descriptor",
        ));
    }
    Ok(())
}

fn shard_from_descriptor(index: usize, input: &BufferDescriptor) -> FaultResult<Shard> {
    let name = Name::new(format!("shard-{index}")).map_err(|error| {
        Fault::invalid_argument("generated shard name is invalid").with_source(error)
    })?;
    let path = ObjectPath::new(format!("artifact/{index}")).map_err(|error| {
        Fault::invalid_argument("generated shard path is invalid").with_source(error)
    })?;
    Ok(Shard {
        name,
        path,
        digest: input.digest,
        size: input.range.length(),
        records: 0,
    })
}

fn artifact_descriptor(
    digest: Digest,
    length: u64,
    ticket: &ExecutionTicket,
    owner_process: &str,
    now_unix_millis: u64,
) -> FaultResult<BufferDescriptor> {
    let descriptor = BufferDescriptor {
        segment_id: format!("artifact:{digest}"),
        generation: ticket.claims.fencing_token.get(),
        range: ByteRange::new(0, length)?,
        element_type: "bytes".to_owned(),
        shape: vec![length],
        digest,
        owner_process: owner_process.to_owned(),
        lease_expires_unix_millis: ticket.claims.expires_unix_millis,
        access: BufferAccess::ReadOnly,
        transport: BufferTransport::Artifact,
        locator: format!("artifact:{digest}"),
    };
    descriptor.validate(now_unix_millis)?;
    Ok(descriptor)
}

fn segment_name(ticket: &ExecutionTicket, index: usize, digest: Digest) -> String {
    let ticket_hash = hash_bytes(ticket.claims.ticket_id.to_string().as_bytes()).to_string();
    let digest_text = digest.to_string();
    let ticket_suffix = ticket_hash.strip_prefix("sha256:").unwrap_or(&ticket_hash);
    let digest_suffix = digest_text.strip_prefix("sha256:").unwrap_or(&digest_text);
    format!(
        "stream-{}-{index}-{}",
        &ticket_suffix[..12],
        &digest_suffix[..12]
    )
}

fn validate_namespace(value: &str) -> FaultResult<()> {
    if value.is_empty() || value.len() > 1_024 || value.starts_with('/') || value.contains("..") {
        return Err(Fault::invalid_argument(
            "checkpoint output namespace is invalid",
        ));
    }
    Ok(())
}

fn validate_owner(value: &str) -> FaultResult<()> {
    if value.is_empty() || value.len() > 256 {
        return Err(Fault::invalid_argument("stage owner process is invalid"));
    }
    Ok(())
}
