// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Framework-independent artifact-proxy request core.
use crate::{ProxyConfig, ProxyHealth, ProxyMetrics, RangeRequest, TransferEngine, ValidatedGrant};
use mindclade_artifact_cas::gc::{GarbageCollectionPlan, SweepReport};
use mindclade_content_digest::Digest;
use mindclade_faults::{Code, Fault, FaultResult};
use std::sync::Arc;

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ReadResult {
    pub digest: Digest,
    pub bytes: Vec<u8>,
    pub ranged: bool,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct WriteResult {
    pub digest: Digest,
    pub size: u64,
}

#[derive(Debug)]
pub struct ArtifactProxyCore {
    config: ProxyConfig,
    transfer: TransferEngine,
    health: Arc<ProxyHealth>,
    metrics: ProxyMetrics,
}

impl ArtifactProxyCore {
    pub fn new(
        config: ProxyConfig,
        transfer: TransferEngine,
        health: Arc<ProxyHealth>,
        metrics: ProxyMetrics,
    ) -> FaultResult<Self> {
        config.validate()?;
        Ok(Self {
            config,
            transfer,
            health,
            metrics,
        })
    }
    fn begin(&self) -> FaultResult<TransferGuard<'_>> {
        if !self.health.snapshot().accepting {
            self.metrics.rejected();
            return Err(Fault::new(Code::Unavailable, "artifact proxy is draining"));
        }
        if !self
            .health
            .try_begin(self.config.maximum_concurrent_transfers)
        {
            self.metrics.rejected();
            return if self.health.snapshot().accepting {
                Err(Fault::new(
                    Code::ResourceExhausted,
                    "artifact proxy transfer limit reached",
                ))
            } else {
                Err(Fault::new(Code::Unavailable, "artifact proxy is draining"))
            };
        }
        Ok(TransferGuard {
            health: self.health.as_ref(),
        })
    }
    pub fn read(
        &self,
        grant: &ValidatedGrant,
        digest: Digest,
        now_unix_millis: u64,
    ) -> FaultResult<ReadResult> {
        let _guard = self.begin()?;
        let bytes = self.transfer.read(grant, digest, now_unix_millis)?;
        Ok(ReadResult {
            digest,
            bytes,
            ranged: false,
        })
    }
    pub fn read_range(
        &self,
        grant: &ValidatedGrant,
        digest: Digest,
        range: RangeRequest,
        now_unix_millis: u64,
    ) -> FaultResult<ReadResult> {
        let _guard = self.begin()?;
        let bytes = self
            .transfer
            .read_range(grant, digest, range, now_unix_millis)?;
        Ok(ReadResult {
            digest,
            bytes,
            ranged: true,
        })
    }
    pub fn write(
        &self,
        grant: &ValidatedGrant,
        namespace: &str,
        bytes: &[u8],
        now_unix_millis: u64,
    ) -> FaultResult<WriteResult> {
        let _guard = self.begin()?;
        let size = u64::try_from(bytes.len())
            .map_err(|_| Fault::new(Code::OutOfRange, "artifact proxy write size exceeds u64"))?;
        if size > self.config.maximum_write_bytes {
            return Err(Fault::new(
                Code::ResourceExhausted,
                "artifact proxy write exceeds service limit",
            ));
        }
        let digest = self
            .transfer
            .write(grant, namespace, bytes, now_unix_millis)?;
        Ok(WriteResult { digest, size })
    }
    /// Execute a version-bound GC plan produced by the Go artifact-control
    /// domain. This internal administrative path never derives eligibility or
    /// reconstructs object locations locally.
    pub fn sweep_garbage_collection(
        &self,
        plan: &GarbageCollectionPlan,
    ) -> FaultResult<SweepReport> {
        let _guard = self.begin()?;
        self.transfer.sweep_garbage_collection(plan)
    }

    pub fn drain(&self) {
        self.health.drain();
    }
    #[must_use]
    pub fn health_snapshot(&self) -> crate::ProxyHealthSnapshot {
        self.health.snapshot()
    }
}

struct TransferGuard<'a> {
    health: &'a ProxyHealth,
}

impl Drop for TransferGuard<'_> {
    fn drop(&mut self) {
        let _ = self.health.end();
    }
}
