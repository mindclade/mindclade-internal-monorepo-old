// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Deterministic, resumable data streaming.
#![forbid(unsafe_code)]
pub mod cache;
pub mod metrics;
pub mod prefetch;
pub mod ranges;
pub mod reader;
pub mod resume;
pub mod shuffle;
use mindclade_bytes_io::ByteSize;
use mindclade_content_digest::{Digest, hash_bytes};
use mindclade_faults::{Code, Fault, FaultResult, RetryHint};
use mindclade_identifiers::Name;
use mindclade_object_store::{ObjectPath, ObjectStore};
use mindclade_record_io::{Decoder, Encoder};
use mindclade_runtime_core::{Policy, Sleeper, execute};
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Arc, mpsc};
use std::thread::{self, JoinHandle};
use std::time::Duration;
use tokio::sync::{mpsc as async_mpsc, watch};
use tokio::task::{JoinHandle as AsyncJoinHandle, JoinSet};

pub const CURSOR_SCHEMA: u16 = 1;

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Shard {
    pub name: Name,
    pub path: ObjectPath,
    pub digest: Digest,
    pub size: u64,
    pub records: u64,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct StreamPlan {
    pub dataset: Name,
    pub epoch: u64,
    pub seed: u64,
    pub world_size: u32,
    pub rank: u32,
    pub shards: Vec<Shard>,
    pub plan_digest: Digest,
}

impl StreamPlan {
    pub fn new(
        dataset: Name,
        epoch: u64,
        seed: u64,
        world_size: u32,
        rank: u32,
        shards: Vec<Shard>,
    ) -> FaultResult<Self> {
        if world_size == 0 || rank >= world_size || shards.is_empty() || shards.len() > 1_000_000 {
            return Err(Fault::invalid_argument(
                "stream topology or shard count is invalid",
            ));
        }
        let mut order: Vec<usize> = (0..shards.len()).collect();
        shuffle::deterministic_shuffle(&mut order, seed ^ epoch.rotate_left(17))?;
        let world_size_usize = usize::try_from(world_size)
            .map_err(|_| Fault::new(Code::OutOfRange, "world size exceeds platform usize"))?;
        let rank_usize = usize::try_from(rank)
            .map_err(|_| Fault::new(Code::OutOfRange, "rank exceeds platform usize"))?;
        let mut shards: Vec<Option<Shard>> = shards.into_iter().map(Some).collect();
        let assigned = order
            .into_iter()
            .enumerate()
            .filter(|&(position, _index)| position % world_size_usize == rank_usize)
            .filter_map(|(_position, index)| shards[index].take())
            .collect::<Vec<_>>();
        let digest = plan_digest(&dataset, epoch, seed, world_size, rank, &assigned)?;
        Ok(Self {
            dataset,
            epoch,
            seed,
            world_size,
            rank,
            shards: assigned,
            plan_digest: digest,
        })
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Cursor {
    pub plan_digest: Digest,
    pub shard_index: u32,
    pub record_index: u64,
    pub byte_offset: u64,
}

impl Cursor {
    #[must_use]
    pub const fn start(plan_digest: Digest) -> Self {
        Self {
            plan_digest,
            shard_index: 0,
            record_index: 0,
            byte_offset: 0,
        }
    }
    pub fn encode(&self) -> FaultResult<Vec<u8>> {
        let mut encoder = Encoder::new();
        encoder.u16(CURSOR_SCHEMA);
        encoder.bytes(self.plan_digest.as_bytes())?;
        encoder.u32(self.shard_index);
        encoder.u64(self.record_index);
        encoder.u64(self.byte_offset);
        Ok(encoder.into_bytes())
    }
    pub fn decode(bytes: &[u8]) -> FaultResult<Self> {
        let mut decoder = Decoder::new(bytes, 1024)?;
        if decoder.u16()? != CURSOR_SCHEMA {
            return Err(Fault::new(
                Code::FailedPrecondition,
                "stream cursor schema is unsupported",
            ));
        }
        let digest = Digest::from_bytes(
            <[u8; 32]>::try_from(decoder.bytes()?)
                .map_err(|_| Fault::data_loss("stream cursor digest length is invalid"))?,
        );
        let value = Self {
            plan_digest: digest,
            shard_index: decoder.u32()?,
            record_index: decoder.u64()?,
            byte_offset: decoder.u64()?,
        };
        decoder.finish()?;
        Ok(value)
    }
    pub fn validate_for(&self, plan: &StreamPlan) -> FaultResult<()> {
        if self.plan_digest == plan.plan_digest
            && usize::try_from(self.shard_index).is_ok_and(|index| index <= plan.shards.len())
        {
            Ok(())
        } else {
            Err(Fault::new(
                Code::FailedPrecondition,
                "stream cursor does not match plan",
            ))
        }
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct PrefetchedShard {
    pub index: usize,
    pub shard: Shard,
    pub bytes: Vec<u8>,
}

pub struct AsyncPrefetcher {
    receiver: async_mpsc::Receiver<FaultResult<PrefetchedShard>>,
    cancellation: watch::Sender<bool>,
    worker: Option<AsyncJoinHandle<()>>,
}

impl core::fmt::Debug for AsyncPrefetcher {
    fn fmt(&self, formatter: &mut core::fmt::Formatter<'_>) -> core::fmt::Result {
        formatter
            .debug_struct("AsyncPrefetcher")
            .field("receiver_closed", &self.receiver.is_closed())
            .field("cancelled", &*self.cancellation.borrow())
            .field("worker_active", &self.worker.is_some())
            .finish()
    }
}

impl AsyncPrefetcher {
    pub fn start(
        plan: StreamPlan,
        store: Arc<dyn ObjectStore>,
        config: prefetch::PrefetchConfig,
        retry_policy: Policy,
    ) -> FaultResult<Self> {
        let config = config.validate()?;
        retry_policy.validate()?;
        let maximum_shard_bytes = ByteSize::new(config.maximum_shard_bytes);
        let (sender, receiver) = async_mpsc::channel(config.buffer_capacity);
        let (cancellation, cancelled) = watch::channel(false);
        let worker = tokio::spawn(run_async_prefetch(
            plan,
            store,
            maximum_shard_bytes,
            config,
            retry_policy,
            sender,
            cancelled,
        ));
        Ok(Self {
            receiver,
            cancellation,
            worker: Some(worker),
        })
    }

    pub async fn next(&mut self) -> Option<FaultResult<PrefetchedShard>> {
        self.receiver.recv().await
    }

    pub async fn shutdown(&mut self, maximum_wait: Duration) -> FaultResult<()> {
        let _ = self.cancellation.send(true);
        self.receiver.close();
        let Some(mut worker) = self.worker.take() else {
            return Ok(());
        };
        match tokio::time::timeout(maximum_wait, &mut worker).await {
            Ok(Ok(())) => Ok(()),
            Ok(Err(error)) => {
                Err(Fault::internal("async prefetch supervisor failed").with_source(error))
            }
            Err(_) => {
                worker.abort();
                Err(Fault::new(
                    Code::DeadlineExceeded,
                    "async prefetch shutdown exceeded its deadline",
                ))
            }
        }
    }
}

impl Drop for AsyncPrefetcher {
    fn drop(&mut self) {
        let _ = self.cancellation.send(true);
        self.receiver.close();
        if let Some(worker) = self.worker.take() {
            worker.abort();
        }
    }
}

pub struct Prefetcher {
    receiver: Option<mpsc::Receiver<FaultResult<PrefetchedShard>>>,
    cancelled: Arc<AtomicBool>,
    worker: Option<JoinHandle<()>>,
}

impl core::fmt::Debug for Prefetcher {
    fn fmt(&self, formatter: &mut core::fmt::Formatter<'_>) -> core::fmt::Result {
        formatter
            .debug_struct("Prefetcher")
            .field("receiver_open", &self.receiver.is_some())
            .field("cancelled", &self.cancelled.load(Ordering::Acquire))
            .field("worker_active", &self.worker.is_some())
            .finish()
    }
}

impl Prefetcher {
    pub fn start(
        plan: StreamPlan,
        store: Arc<dyn ObjectStore>,
        maximum_shard_bytes: ByteSize,
        depth: usize,
        retry_policy: Policy,
        sleeper: Arc<dyn Sleeper + Send + Sync>,
    ) -> FaultResult<Self> {
        if depth == 0 || depth > 1024 {
            return Err(Fault::invalid_argument("prefetch depth is invalid"));
        }
        let (sender, receiver) = mpsc::sync_channel(depth);
        let cancelled = Arc::new(AtomicBool::new(false));
        let worker_cancelled = Arc::clone(&cancelled);
        let worker = thread::Builder::new()
            .name("mindclade-data-prefetch".to_owned())
            .spawn(move || {
                for (index, shard) in plan.shards.into_iter().enumerate() {
                    if worker_cancelled.load(Ordering::Acquire) {
                        break;
                    }
                    let result = (|| {
                        let retry_seed = u64::try_from(index).map_err(|_| {
                            Fault::new(Code::OutOfRange, "stream shard index exceeds u64")
                        })?;
                        let bytes = execute(
                            retry_policy,
                            &SystemClockAdapter,
                            sleeper.as_ref(),
                            None,
                            retry_seed,
                            |_| store.get(&shard.path, maximum_shard_bytes),
                        )?;
                        shard.digest.verify(&bytes)?;
                        let actual_size = u64::try_from(bytes.len()).map_err(|_| {
                            Fault::new(Code::OutOfRange, "stream shard size exceeds u64")
                        })?;
                        if actual_size != shard.size {
                            return Err(Fault::data_loss("stream shard size mismatch"));
                        }
                        Ok(PrefetchedShard {
                            index,
                            shard,
                            bytes,
                        })
                    })();
                    if sender.send(result).is_err() {
                        return;
                    }
                }
            })
            .map_err(|error| {
                Fault::internal("failed to spawn prefetch worker").with_source(error)
            })?;
        Ok(Self {
            receiver: Some(receiver),
            cancelled,
            worker: Some(worker),
        })
    }
}

impl Iterator for Prefetcher {
    type Item = FaultResult<PrefetchedShard>;
    fn next(&mut self) -> Option<Self::Item> {
        self.receiver
            .as_ref()
            .and_then(|receiver| receiver.recv().ok())
    }
}

impl Drop for Prefetcher {
    fn drop(&mut self) {
        self.cancelled.store(true, Ordering::Release);
        self.receiver.take();
        // A synchronous ObjectStore call cannot be interrupted safely. Drop
        // therefore detaches the compatibility worker after signalling
        // cancellation instead of waiting without a deadline.
        self.worker.take();
    }
}

async fn run_async_prefetch(
    plan: StreamPlan,
    store: Arc<dyn ObjectStore>,
    maximum_shard_bytes: ByteSize,
    config: prefetch::PrefetchConfig,
    retry_policy: Policy,
    sender: async_mpsc::Sender<FaultResult<PrefetchedShard>>,
    mut cancelled: watch::Receiver<bool>,
) {
    let mut shards = plan.shards.into_iter().enumerate();
    let mut fetches = JoinSet::new();
    let mut completed = std::collections::BTreeMap::new();
    let mut next_delivery = 0_usize;

    loop {
        while fetches.len() < config.concurrency {
            let Some((index, shard)) = shards.next() else {
                break;
            };
            let store = Arc::clone(&store);
            let child_cancelled = cancelled.clone();
            fetches.spawn(fetch_shard_async(
                index,
                shard,
                store,
                maximum_shard_bytes,
                config.fetch_timeout,
                retry_policy,
                child_cancelled,
            ));
        }

        if fetches.is_empty() {
            break;
        }

        let joined = tokio::select! {
            changed = cancelled.changed() => {
                if changed.is_err() || *cancelled.borrow() {
                    fetches.abort_all();
                    return;
                }
                continue;
            }
            joined = fetches.join_next() => joined,
        };
        let Some(joined) = joined else {
            break;
        };
        let result = match joined {
            Ok((index, result)) => (index, result),
            Err(error) => {
                let fault = Fault::internal("async shard fetch task failed").with_source(error);
                let _ = sender.send(Err(fault)).await;
                fetches.abort_all();
                return;
            }
        };
        completed.insert(result.0, result.1);

        while let Some(result) = completed.remove(&next_delivery) {
            let failed = result.is_err();
            let delivered = tokio::select! {
                changed = cancelled.changed() => {
                    changed.is_ok() && !*cancelled.borrow()
                }
                sent = sender.send(result) => sent.is_ok(),
            };
            if !delivered || failed {
                fetches.abort_all();
                return;
            }
            next_delivery = if let Some(value) = next_delivery.checked_add(1) {
                value
            } else {
                let _ = sender
                    .send(Err(Fault::new(
                        Code::OutOfRange,
                        "prefetch delivery index overflow",
                    )))
                    .await;
                fetches.abort_all();
                return;
            };
        }
    }
}

async fn fetch_shard_async(
    index: usize,
    shard: Shard,
    store: Arc<dyn ObjectStore>,
    maximum_shard_bytes: ByteSize,
    fetch_timeout: Duration,
    retry_policy: Policy,
    mut cancelled: watch::Receiver<bool>,
) -> (usize, FaultResult<PrefetchedShard>) {
    let result = async {
        let retry_seed = u64::try_from(index)
            .map_err(|_| Fault::new(Code::OutOfRange, "stream shard index exceeds u64"))?;
        for attempt in 1..=retry_policy.max_attempts {
            if *cancelled.borrow() {
                return Err(Fault::new(Code::Cancelled, "shard prefetch was cancelled"));
            }
            let operation_store = Arc::clone(&store);
            let path = shard.path.clone();
            let fetched = tokio::time::timeout(
                fetch_timeout,
                tokio::task::spawn_blocking(move || {
                    operation_store.get(&path, maximum_shard_bytes)
                }),
            )
            .await;
            let fetched = match fetched {
                Ok(Ok(result)) => result,
                Ok(Err(error)) => {
                    return Err(Fault::internal("blocking shard fetch task failed")
                        .with_source(error));
                }
                Err(_) => {
                    return Err(Fault::new(
                        Code::DeadlineExceeded,
                        "individual shard fetch exceeded its deadline",
                    ));
                }
            };
            match fetched {
                Ok(bytes) => {
                    shard.digest.verify(&bytes)?;
                    let actual_size = u64::try_from(bytes.len()).map_err(|_| {
                        Fault::new(Code::OutOfRange, "stream shard size exceeds u64")
                    })?;
                    if actual_size != shard.size {
                        return Err(Fault::data_loss("stream shard size mismatch"));
                    }
                    return Ok(PrefetchedShard {
                        index,
                        shard,
                        bytes,
                    });
                }
                Err(fault) => {
                    if attempt == retry_policy.max_attempts
                        || !fault.retry_hint().is_retryable()
                    {
                        return Err(fault.with_context("attempt", u64::from(attempt)));
                    }
                    let delay = match fault.retry_hint() {
                        RetryHint::After(value) => value,
                        RetryHint::Immediate => retry_policy.delay(attempt, retry_seed)?,
                        RetryHint::Never => Duration::ZERO,
                    };
                    tokio::select! {
                        changed = cancelled.changed() => {
                            if changed.is_err() || *cancelled.borrow() {
                                return Err(Fault::new(Code::Cancelled, "shard prefetch was cancelled"));
                            }
                        }
                        () = tokio::time::sleep(delay) => {}
                    }
                }
            }
        }
        Err(Fault::internal("async prefetch retry loop was exhausted"))
    }
    .await;
    (index, result)
}

#[derive(Clone, Copy, Debug)]
struct SystemClockAdapter;

impl mindclade_runtime_core::Clock for SystemClockAdapter {
    fn system_now(&self) -> std::time::SystemTime {
        std::time::SystemTime::now()
    }
    fn monotonic_now(&self) -> std::time::Instant {
        std::time::Instant::now()
    }
}

fn plan_digest(
    dataset: &Name,
    epoch: u64,
    seed: u64,
    world_size: u32,
    rank: u32,
    shards: &[Shard],
) -> FaultResult<Digest> {
    let mut encoder = Encoder::new();
    encoder.string(dataset.as_str())?;
    encoder.u64(epoch);
    encoder.u64(seed);
    encoder.u32(world_size);
    encoder.u32(rank);
    encoder.u32(
        u32::try_from(shards.len())
            .map_err(|_| Fault::new(Code::OutOfRange, "stream shard count exceeds u32"))?,
    );
    for shard in shards {
        encoder.string(shard.name.as_str())?;
        encoder.string(shard.path.as_str())?;
        encoder.bytes(shard.digest.as_bytes())?;
        encoder.u64(shard.size);
        encoder.u64(shard.records);
    }
    Ok(hash_bytes(&encoder.into_bytes()))
}
