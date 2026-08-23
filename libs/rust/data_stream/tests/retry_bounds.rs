// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! A provider-supplied `RetryHint::After` is untrusted input. For a network
//! object store it is a remote `Retry-After`, so a peer must not be able to
//! choose how long a node sleeps between shard-fetch attempts.

use mindclade_bytes_io::{ByteRange, ByteSize};
use mindclade_content_digest::hash_bytes;
use mindclade_data_stream::prefetch::PrefetchConfig;
use mindclade_data_stream::{AsyncPrefetcher, Prefetcher, Shard, StreamPlan};
use mindclade_faults::{Code, Fault, FaultResult, RetryHint};
use mindclade_identifiers::Name;
use mindclade_object_store::{ObjectMeta, ObjectPath, ObjectStore, PutCondition, PutResult};
use mindclade_runtime_core::{Policy, ResourceVersion, Sleeper};
use std::sync::{Arc, Mutex};
use std::time::{Duration, Instant};

/// A remote `Retry-After` far beyond anything the policy would ever schedule.
const HOSTILE_RETRY_AFTER: Duration = Duration::from_hours(1);
const MAXIMUM_DELAY: Duration = Duration::from_millis(20);
const INITIAL_DELAY: Duration = Duration::from_millis(1);

/// Always fails with a retryable fault carrying a caller-chosen retry hint.
#[derive(Debug)]
struct StallingStore {
    hint: Duration,
}

impl ObjectStore for StallingStore {
    fn head(&self, _path: &ObjectPath) -> FaultResult<Option<ObjectMeta>> {
        Ok(None)
    }
    fn get(&self, _path: &ObjectPath, _maximum_bytes: ByteSize) -> FaultResult<Vec<u8>> {
        Err(Fault::new(Code::Unavailable, "provider is throttling")
            .with_retry_hint(RetryHint::After(self.hint)))
    }
    fn get_range(&self, _path: &ObjectPath, _range: ByteRange) -> FaultResult<Vec<u8>> {
        Err(Fault::new(Code::Unimplemented, "range reads are not used"))
    }
    fn put(
        &self,
        _path: &ObjectPath,
        _bytes: &[u8],
        _condition: PutCondition,
    ) -> FaultResult<PutResult> {
        Err(Fault::new(Code::Unimplemented, "writes are not used"))
    }
    fn delete(&self, _path: &ObjectPath, _expected: Option<ResourceVersion>) -> FaultResult<bool> {
        Err(Fault::new(Code::Unimplemented, "deletes are not used"))
    }
    fn list(&self, _prefix: Option<&ObjectPath>, _limit: usize) -> FaultResult<Vec<ObjectMeta>> {
        Ok(Vec::new())
    }
}

/// Records requested sleeps instead of performing them, so the synchronous
/// retry bound can be asserted exactly rather than by wall clock.
#[derive(Debug, Default)]
struct RecordingSleeper {
    requested: Mutex<Vec<Duration>>,
}

impl Sleeper for RecordingSleeper {
    fn sleep(&self, duration: Duration) {
        self.requested
            .lock()
            .unwrap_or_else(std::sync::PoisonError::into_inner)
            .push(duration);
    }
}

fn plan() -> StreamPlan {
    let bytes = b"shard".to_vec();
    let shards = vec![Shard {
        name: Name::new("shard-0").expect("name"),
        path: ObjectPath::new("dataset/shard-0").expect("path"),
        digest: hash_bytes(&bytes),
        size: u64::try_from(bytes.len()).expect("size"),
        records: 1,
    }];
    StreamPlan::new(Name::new("dataset").expect("dataset"), 1, 7, 1, 0, shards).expect("plan")
}

fn policy() -> Policy {
    Policy {
        max_attempts: 3,
        initial_delay: INITIAL_DELAY,
        maximum_delay: MAXIMUM_DELAY,
        ..Policy::default()
    }
}

fn recorded_delays(hint: Duration) -> Vec<Duration> {
    let sleeper = Arc::new(RecordingSleeper::default());
    let store: Arc<dyn ObjectStore> = Arc::new(StallingStore { hint });
    let mut prefetcher = Prefetcher::start(
        plan(),
        store,
        ByteSize::new(1024),
        2,
        policy(),
        sleeper.clone(),
    )
    .expect("prefetcher starts");
    let result = prefetcher.next().expect("the shard result is delivered");
    assert!(result.is_err(), "the stalling provider never succeeds");
    drop(prefetcher);
    let delays = sleeper
        .requested
        .lock()
        .unwrap_or_else(std::sync::PoisonError::into_inner)
        .clone();
    assert!(!delays.is_empty(), "the retry loop must have backed off");
    delays
}

#[test]
fn synchronous_prefetch_caps_a_provider_supplied_retry_delay() {
    let requested = recorded_delays(HOSTILE_RETRY_AFTER);
    for delay in &requested {
        assert!(
            *delay <= MAXIMUM_DELAY,
            "a remote peer chose a {delay:?} sleep, above the configured {MAXIMUM_DELAY:?} bound"
        );
    }
    let total: Duration = requested.iter().sum();
    assert!(
        total <= MAXIMUM_DELAY * policy().max_attempts,
        "total retry duration {total:?} is not bounded by the policy"
    );
}

/// The bound is two-sided. A remote `Retry-After: 0` must not buy an
/// unthrottled burst of every remaining attempt against a provider that is
/// already struggling.
#[test]
fn synchronous_prefetch_floors_a_provider_supplied_retry_delay() {
    for delay in recorded_delays(Duration::ZERO) {
        assert!(
            delay >= INITIAL_DELAY,
            "a remote peer suppressed backoff entirely with a {delay:?} sleep"
        );
    }
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
async fn async_prefetch_caps_a_provider_supplied_retry_delay() {
    let store: Arc<dyn ObjectStore> = Arc::new(StallingStore {
        hint: HOSTILE_RETRY_AFTER,
    });
    let config = PrefetchConfig {
        buffer_capacity: 2,
        concurrency: 1,
        maximum_shard_bytes: 1024,
        fetch_timeout: Duration::from_secs(5),
    };
    let mut prefetch =
        AsyncPrefetcher::start(plan(), store, config, policy()).expect("prefetch starts");

    // The whole retry loop must finish inside a budget far below the hostile
    // hint: attempts are bounded by the policy and each backoff is clamped to
    // `maximum_delay`, so total sleep cannot exceed a few tens of milliseconds.
    let started = Instant::now();
    let delivered = tokio::time::timeout(Duration::from_secs(2), prefetch.next())
        .await
        .expect("a remote retry hint must not stall the prefetch pipeline");
    let elapsed = started.elapsed();
    assert!(
        delivered.is_some_and(|result| result.is_err()),
        "the stalling provider never succeeds"
    );
    assert!(
        elapsed < HOSTILE_RETRY_AFTER,
        "the prefetcher honoured the remote {HOSTILE_RETRY_AFTER:?} hint"
    );
    prefetch
        .shutdown(Duration::from_secs(1))
        .await
        .expect("bounded shutdown");
}
