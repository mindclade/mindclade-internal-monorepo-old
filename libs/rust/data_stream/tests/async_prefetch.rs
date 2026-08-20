// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use mindclade_bytes_io::{ByteRange, ByteSize};
use mindclade_content_digest::hash_bytes;
use mindclade_data_stream::prefetch::PrefetchConfig;
use mindclade_data_stream::{AsyncPrefetcher, Shard, StreamPlan};
use mindclade_faults::FaultResult;
use mindclade_identifiers::Name;
use mindclade_object_store::{
    MemoryStore, ObjectMeta, ObjectPath, ObjectStore, PutCondition, PutResult,
};
use mindclade_runtime_core::{Policy, ResourceVersion};
use std::sync::atomic::{AtomicUsize, Ordering};
use std::sync::{Arc, Condvar, Mutex};
use std::time::Duration;
use tokio::sync::mpsc;

type Gate = Arc<(Mutex<bool>, Condvar)>;

struct TrackingStore {
    inner: MemoryStore,
    gate: Gate,
    active: AtomicUsize,
    maximum_active: AtomicUsize,
    started: mpsc::UnboundedSender<()>,
    finished: mpsc::UnboundedSender<()>,
}

impl TrackingStore {
    fn record_maximum(&self, active: usize) {
        let mut maximum = self.maximum_active.load(Ordering::Acquire);
        while active > maximum {
            match self.maximum_active.compare_exchange_weak(
                maximum,
                active,
                Ordering::AcqRel,
                Ordering::Acquire,
            ) {
                Ok(_) => break,
                Err(observed) => maximum = observed,
            }
        }
    }
}

impl ObjectStore for TrackingStore {
    fn head(&self, path: &ObjectPath) -> FaultResult<Option<ObjectMeta>> {
        self.inner.head(path)
    }

    fn get(&self, path: &ObjectPath, maximum_bytes: ByteSize) -> FaultResult<Vec<u8>> {
        let active = self.active.fetch_add(1, Ordering::AcqRel) + 1;
        self.record_maximum(active);
        let _ = self.started.send(());
        let (lock, ready) = &*self.gate;
        let mut released = lock
            .lock()
            .unwrap_or_else(std::sync::PoisonError::into_inner);
        while !*released {
            released = ready
                .wait(released)
                .unwrap_or_else(std::sync::PoisonError::into_inner);
        }
        drop(released);
        let result = self.inner.get(path, maximum_bytes);
        self.active.fetch_sub(1, Ordering::AcqRel);
        let _ = self.finished.send(());
        result
    }

    fn get_range(&self, path: &ObjectPath, range: ByteRange) -> FaultResult<Vec<u8>> {
        self.inner.get_range(path, range)
    }

    fn put(
        &self,
        path: &ObjectPath,
        bytes: &[u8],
        condition: PutCondition,
    ) -> FaultResult<PutResult> {
        self.inner.put(path, bytes, condition)
    }

    fn delete(&self, path: &ObjectPath, expected: Option<ResourceVersion>) -> FaultResult<bool> {
        self.inner.delete(path, expected)
    }

    fn list(&self, prefix: Option<&ObjectPath>, limit: usize) -> FaultResult<Vec<ObjectMeta>> {
        self.inner.list(prefix, limit)
    }
}

struct Fixture {
    plan: StreamPlan,
    tracking: Arc<TrackingStore>,
    started: mpsc::UnboundedReceiver<()>,
    finished: mpsc::UnboundedReceiver<()>,
    gate: Gate,
}

fn fixture(count: usize) -> Fixture {
    let inner = MemoryStore::new();
    let mut shards = Vec::with_capacity(count);
    for index in 0..count {
        let bytes = format!("shard-{index}").into_bytes();
        let path = ObjectPath::new(format!("dataset/shard-{index}")).expect("path");
        inner
            .put(&path, &bytes, PutCondition::CreateOnly)
            .expect("fixture object");
        shards.push(Shard {
            name: Name::new(format!("shard-{index}")).expect("name"),
            path,
            digest: hash_bytes(&bytes),
            size: u64::try_from(bytes.len()).expect("size"),
            records: 1,
        });
    }
    let plan =
        StreamPlan::new(Name::new("dataset").expect("dataset"), 1, 7, 1, 0, shards).expect("plan");
    let gate = Arc::new((Mutex::new(false), Condvar::new()));
    let (started_tx, started_rx) = mpsc::unbounded_channel();
    let (finished_tx, finished_rx) = mpsc::unbounded_channel();
    let store = Arc::new(TrackingStore {
        inner,
        gate: gate.clone(),
        active: AtomicUsize::new(0),
        maximum_active: AtomicUsize::new(0),
        started: started_tx,
        finished: finished_tx,
    });
    Fixture {
        plan,
        tracking: store,
        started: started_rx,
        finished: finished_rx,
        gate,
    }
}

fn release(gate: &Gate) {
    let (lock, ready) = &**gate;
    *lock
        .lock()
        .unwrap_or_else(std::sync::PoisonError::into_inner) = true;
    ready.notify_all();
}

fn config(concurrency: usize) -> PrefetchConfig {
    PrefetchConfig {
        buffer_capacity: 4,
        concurrency,
        maximum_shard_bytes: 1024,
        fetch_timeout: Duration::from_secs(5),
    }
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
async fn bounded_parallel_fetches_are_delivered_in_plan_order() {
    let Fixture {
        plan,
        tracking,
        mut started,
        finished: _,
        gate,
    } = fixture(4);
    let store: Arc<dyn ObjectStore> = tracking.clone();
    let mut prefetch = AsyncPrefetcher::start(
        plan,
        store,
        config(3),
        Policy {
            max_attempts: 1,
            ..Policy::default()
        },
    )
    .expect("prefetch");

    for _ in 0..3 {
        tokio::time::timeout(Duration::from_secs(2), started.recv())
            .await
            .expect("parallel fetch should start")
            .expect("start signal");
    }
    assert_eq!(tracking.maximum_active.load(Ordering::Acquire), 3);
    release(&gate);

    let mut indices = Vec::new();
    while let Some(result) = tokio::time::timeout(Duration::from_secs(2), prefetch.next())
        .await
        .expect("prefetch result")
    {
        indices.push(result.expect("verified shard").index);
    }
    assert_eq!(indices, vec![0, 1, 2, 3]);
    prefetch
        .shutdown(Duration::from_secs(1))
        .await
        .expect("bounded shutdown");
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
async fn shutdown_does_not_wait_for_blocking_object_store_calls() {
    let Fixture {
        plan,
        tracking,
        mut started,
        mut finished,
        gate,
    } = fixture(4);
    let store: Arc<dyn ObjectStore> = tracking.clone();
    let mut prefetch =
        AsyncPrefetcher::start(plan, store, config(2), Policy::default()).expect("prefetch");

    for _ in 0..2 {
        tokio::time::timeout(Duration::from_secs(2), started.recv())
            .await
            .expect("bounded fetch should start")
            .expect("start signal");
    }
    prefetch
        .shutdown(Duration::from_secs(1))
        .await
        .expect("shutdown should cancel async supervision");
    assert_eq!(tracking.maximum_active.load(Ordering::Acquire), 2);
    assert!(started.try_recv().is_err());

    release(&gate);
    for _ in 0..2 {
        tokio::time::timeout(Duration::from_secs(2), finished.recv())
            .await
            .expect("blocking call should finish after release")
            .expect("finish signal");
    }
}
