// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Checkpoint staging, transfer, and repair helpers owned by the node agent.

use crate::ProviderSource;
use bytes::Bytes;
use mindclade_bytes_io::{ByteSize, Reservation};
use mindclade_checkpoint_io::{RepairPlan, StagingBudget, VerificationReport};
use mindclade_content_digest::Digest;
use mindclade_faults::{Code, Fault, FaultResult};
use mindclade_worker_protocol::ExecutionTicket;

/// Provider bytes whose host-memory/staging reservation remains live until the
/// consumer drops the staged component.
#[derive(Debug)]
pub struct StagedComponent {
    digest: Digest,
    bytes: Bytes,
    _reservation: Reservation,
}

impl StagedComponent {
    #[must_use]
    pub const fn digest(&self) -> Digest {
        self.digest
    }

    #[must_use]
    pub fn bytes(&self) -> &Bytes {
        &self.bytes
    }
}

#[derive(Debug)]
pub struct CheckpointTransfer {
    staging: StagingBudget,
}

impl CheckpointTransfer {
    #[must_use]
    pub fn new(staging: StagingBudget) -> Self {
        Self { staging }
    }

    pub fn reserve(&self, bytes: u64) -> FaultResult<Reservation> {
        self.staging.reserve(ByteSize::new(bytes))
    }

    /// Download and verify one immutable checkpoint component while retaining
    /// its staging-memory reservation for the lifetime of the returned bytes.
    pub async fn stage_component(
        &self,
        source: &ProviderSource,
        ticket: &ExecutionTicket,
        digest: Digest,
        now_unix_millis: u64,
    ) -> FaultResult<StagedComponent> {
        let bytes = source
            .read_artifact(ticket, digest, now_unix_millis)
            .await?;
        let size = u64::try_from(bytes.len())
            .map_err(|_| Fault::new(Code::OutOfRange, "checkpoint component length exceeds u64"))?;
        let reservation = self.reserve(size)?;
        Ok(StagedComponent {
            digest,
            bytes,
            _reservation: reservation,
        })
    }

    /// Publish an immutable checkpoint component through the ticket-authorized
    /// content-addressed provider path.
    pub async fn publish_component(
        &self,
        source: &ProviderSource,
        ticket: &ExecutionTicket,
        namespace: &str,
        bytes: Bytes,
        now_unix_millis: u64,
    ) -> FaultResult<Digest> {
        source
            .publish_artifact(ticket, namespace, bytes, now_unix_millis)
            .await
    }

    #[must_use]
    pub fn used_bytes(&self) -> u64 {
        self.staging.used().get()
    }

    #[must_use]
    pub fn repair_plan(report: &VerificationReport) -> RepairPlan {
        let missing_or_corrupt_shards = report
            .failures
            .iter()
            .filter_map(|failure| failure.split(':').next())
            .map(str::to_owned)
            .collect();
        RepairPlan {
            missing_or_corrupt_shards,
        }
    }
}
