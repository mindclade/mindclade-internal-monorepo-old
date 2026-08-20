// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Deterministic data-stream and provider-prefetch adapters.

use crate::ProviderSource;
use mindclade_bytes_io::ByteSize;
use mindclade_data_stream::{
    AsyncPrefetcher, PrefetchedShard, Prefetcher, Shard, StreamPlan, prefetch::PrefetchConfig,
};
use mindclade_faults::{Code, Fault, FaultResult};
use mindclade_object_store::ObjectStore;
use mindclade_runtime_core::{Policy, Sleeper};
use mindclade_worker_protocol::ExecutionTicket;
use std::sync::Arc;
use std::time::Duration;

#[derive(Clone, Debug)]
pub struct StreamWorker {
    pub maximum_shard_bytes: ByteSize,
    pub prefetch_depth: usize,
    pub retry_policy: Policy,
}

impl StreamWorker {
    pub fn validate(&self) -> FaultResult<()> {
        if self.maximum_shard_bytes.get() == 0
            || self.prefetch_depth == 0
            || self.prefetch_depth > 1_024
        {
            return Err(Fault::invalid_argument(
                "node data-stream worker configuration is invalid",
            ));
        }
        Ok(())
    }

    /// Start the synchronous/local-store prefetch mechanism used by hermetic
    /// and local data paths.
    pub fn start(
        &self,
        plan: StreamPlan,
        store: Arc<dyn ObjectStore>,
        sleeper: Arc<dyn Sleeper + Send + Sync>,
    ) -> FaultResult<Prefetcher> {
        self.validate()?;
        Prefetcher::start(
            plan,
            store,
            self.maximum_shard_bytes,
            self.prefetch_depth,
            self.retry_policy,
            sleeper,
        )
    }

    /// Start the production async prefetch path with independent fetch
    /// parallelism and delivery capacity.
    pub fn start_async(
        &self,
        plan: StreamPlan,
        store: Arc<dyn ObjectStore>,
        fetch_timeout: Duration,
    ) -> FaultResult<AsyncPrefetcher> {
        self.validate()?;
        let concurrency = self.prefetch_depth.min(4);
        AsyncPrefetcher::start(
            plan,
            store,
            PrefetchConfig {
                buffer_capacity: self.prefetch_depth,
                concurrency,
                maximum_shard_bytes: self.maximum_shard_bytes.get(),
                fetch_timeout,
            },
            self.retry_policy,
        )
    }

    /// Fetch one immutable provider shard through the execution ticket. This is
    /// the provider-backed primitive used by ingestion/training workers before
    /// higher-level prefetch scheduling forms a queue around it.
    pub async fn fetch_provider_shard(
        &self,
        source: &ProviderSource,
        ticket: &ExecutionTicket,
        index: usize,
        shard: Shard,
        now_unix_millis: u64,
    ) -> FaultResult<PrefetchedShard> {
        self.validate()?;
        if shard.size == 0 || shard.size > self.maximum_shard_bytes.get() {
            return Err(Fault::new(
                Code::ResourceExhausted,
                "dataset shard exceeds node stream byte limit",
            ));
        }
        let bytes = source
            .read_artifact(ticket, shard.digest, now_unix_millis)
            .await?;
        let actual_size = u64::try_from(bytes.len())
            .map_err(|_| Fault::new(Code::OutOfRange, "dataset shard length exceeds u64"))?;
        if actual_size != shard.size {
            return Err(Fault::data_loss("dataset shard size mismatch"));
        }
        shard.digest.verify(&bytes)?;
        Ok(PrefetchedShard {
            index,
            shard,
            bytes: bytes.to_vec(),
        })
    }
}
