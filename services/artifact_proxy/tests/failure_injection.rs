// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Object-store outage: the retry is bounded and the outage reaches the caller.
//!
//! This file is named by a qualification gate --
//! `configs/qualification/failure_injection.toml`, scenario `object_store_unavailable`,
//! invariant `bounded_retry`. What it used to contain, in its entirety, was
//!
//!     let maximum_attempts = 5_u32;
//!     assert!(maximum_attempts > 0 && maximum_attempts <= 10);
//!
//! over a constant declared two lines above the assertion. No provider was involved, no
//! retry loop ran, and no proxy code was executed: the gate could not fail, whatever the
//! proxy did. The cases below drive the real retry policy and the real transfer path
//! against a store that is down, and assert the two properties the invariant names --
//! that the attempt count is bounded, and that an outage is never reported as success.

use bytes::Bytes;
use mindclade_artifact_cas::{ArtifactCas, CasConfig};
use mindclade_artifact_proxy::{
    AccessContext, LocalCache, ProxyMetrics, TransferEngine, ValidatedGrant,
};
use mindclade_bytes_io::{ByteRange, ByteSize};
use mindclade_content_digest::{Digest, hash_bytes};
use mindclade_faults::{Code, Fault, FaultResult};
use mindclade_identifiers::ResourceId;
use mindclade_object_store::{
    MemoryStore, ObjectMeta, ObjectPath, ObjectStore, PutCondition, PutResult,
};
use mindclade_runtime_core::{ManualClock, Policy, ResourceVersion, Sleeper, execute};
use mindclade_worker_protocol::ArtifactGrant;
use std::collections::BTreeSet;
use std::sync::atomic::{AtomicBool, AtomicU32, Ordering};
use std::sync::{Arc, Mutex, PoisonError};
use std::time::{Duration, Instant, SystemTime};

/// An object store that is unavailable until it is told otherwise.
///
/// The point of injecting the outage this low is that everything above it -- the CAS, the
/// proxy's cache, the transfer engine -- is the production code path, unmodified. A test
/// double at the proxy boundary would have proved something about the double.
#[derive(Debug)]
struct OutageStore {
    inner: MemoryStore,
    down: AtomicBool,
    reads: AtomicU32,
}

impl OutageStore {
    fn new() -> Self {
        Self {
            inner: MemoryStore::new(),
            down: AtomicBool::new(true),
            reads: AtomicU32::new(0),
        }
    }

    fn recover(&self) {
        self.down.store(false, Ordering::SeqCst);
    }

    fn reads(&self) -> u32 {
        self.reads.load(Ordering::SeqCst)
    }

    fn available(&self) -> FaultResult<()> {
        if self.down.load(Ordering::SeqCst) {
            return Err(Fault::new(Code::Unavailable, "object store is unavailable"));
        }
        Ok(())
    }
}

impl ObjectStore for OutageStore {
    fn head(&self, path: &ObjectPath) -> FaultResult<Option<ObjectMeta>> {
        self.available()?;
        self.inner.head(path)
    }

    fn get(&self, path: &ObjectPath, maximum_bytes: ByteSize) -> FaultResult<Vec<u8>> {
        self.reads.fetch_add(1, Ordering::SeqCst);
        self.available()?;
        self.inner.get(path, maximum_bytes)
    }

    fn get_range(&self, path: &ObjectPath, range: ByteRange) -> FaultResult<Vec<u8>> {
        self.available()?;
        self.inner.get_range(path, range)
    }

    fn put(
        &self,
        path: &ObjectPath,
        bytes: &[u8],
        condition: PutCondition,
    ) -> FaultResult<PutResult> {
        // Writes bypass the outage flag so a case can seed content and then take the store
        // down, which is the sequence a mid-run outage actually produces.
        self.inner.put(path, bytes, condition)
    }

    fn delete(&self, path: &ObjectPath, expected: Option<ResourceVersion>) -> FaultResult<bool> {
        self.available()?;
        self.inner.delete(path, expected)
    }

    fn list(&self, prefix: Option<&ObjectPath>, limit: usize) -> FaultResult<Vec<ObjectMeta>> {
        self.available()?;
        self.inner.list(prefix, limit)
    }
}

/// Records what the retry loop would have slept, so the bound can be asserted without
/// spending the wall-clock time to observe it.
#[derive(Debug, Default)]
struct RecordingSleeper {
    delays: Mutex<Vec<Duration>>,
}

impl RecordingSleeper {
    fn delays(&self) -> Vec<Duration> {
        self.delays
            .lock()
            .unwrap_or_else(PoisonError::into_inner)
            .clone()
    }
}

impl Sleeper for RecordingSleeper {
    fn sleep(&self, duration: Duration) {
        self.delays
            .lock()
            .unwrap_or_else(PoisonError::into_inner)
            .push(duration);
    }
}

fn clock() -> ManualClock {
    ManualClock::new(SystemTime::UNIX_EPOCH, Instant::now())
}

fn outage_policy() -> Policy {
    Policy {
        max_attempts: 4,
        initial_delay: Duration::from_millis(10),
        maximum_delay: Duration::from_millis(80),
        multiplier_milli: 2_000,
        jitter_permyriad: 2_000,
    }
}

fn resource_id(value: &str) -> ResourceId {
    value.parse().expect("resource id")
}

fn grant(digest: Digest) -> ValidatedGrant {
    ValidatedGrant {
        access: AccessContext {
            tenant_id: resource_id("tenant_019c0000000070008000000000000002"),
            workspace_id: resource_id("workspace_019c0000000070008000000000000003"),
            principal_id: "service-account:failure-injection".to_owned(),
        },
        grant: ArtifactGrant {
            readable_digests: BTreeSet::from([digest]),
            writable_namespaces: BTreeSet::from(["runs/outage".to_owned()]),
            maximum_read_bytes: 4096,
            maximum_write_bytes: 4096,
            allow_range_reads: false,
            allow_multipart_writes: false,
        },
        ticket_id: "ticket_019c0000000070008000000000000001".to_owned(),
        expires_unix_millis: 100_000,
    }
}

#[test]
fn object_store_outage_is_bounded() {
    // A total outage: every attempt fails. The invariant is that the loop stops at
    // max_attempts and hands the fault back, rather than retrying until something else
    // gives up.
    let policy = outage_policy();
    let clock = clock();
    let sleeper = RecordingSleeper::default();
    let attempts = AtomicU32::new(0);

    let outcome: FaultResult<Vec<u8>> = execute(policy, &clock, &sleeper, None, 0x0f1e, |_| {
        attempts.fetch_add(1, Ordering::SeqCst);
        Err(Fault::new(Code::Unavailable, "object store is unavailable"))
    });

    let fault = outcome.expect_err("a total outage must never be reported as success");
    assert_eq!(fault.code(), Code::Unavailable);
    assert_eq!(
        attempts.load(Ordering::SeqCst),
        policy.max_attempts,
        "the retry loop must attempt exactly max_attempts times"
    );

    let delays = sleeper.delays();
    let expected_sleeps = usize::try_from(policy.max_attempts).expect("attempt count") - 1;
    assert_eq!(delays.len(), expected_sleeps, "one sleep between attempts");
    for delay in delays {
        assert!(
            delay <= policy.maximum_delay,
            "backoff must stay under the policy ceiling, got {delay:?}"
        );
    }
}

#[test]
fn a_permanent_failure_is_not_retried() {
    // The other half of "bounded": a fault the store will never recover from must cost one
    // attempt, not four. Without this, a policy that retried everything would still satisfy
    // the test above.
    let policy = outage_policy();
    let clock = clock();
    let sleeper = RecordingSleeper::default();
    let attempts = AtomicU32::new(0);

    let outcome: FaultResult<Vec<u8>> = execute(policy, &clock, &sleeper, None, 0x0f1e, |_| {
        attempts.fetch_add(1, Ordering::SeqCst);
        Err(Fault::new(Code::PermissionDenied, "grant is not valid"))
    });

    assert_eq!(
        outcome.expect_err("permission denial must surface").code(),
        Code::PermissionDenied
    );
    assert_eq!(attempts.load(Ordering::SeqCst), 1);
    assert!(sleeper.delays().is_empty());
}

#[test]
fn an_outage_surfaces_through_the_transfer_engine_and_leaves_no_stale_entry() {
    // The proxy path, not a simulation of it: TransferEngine -> LocalCache -> ArtifactCas ->
    // ObjectStore, with the outage injected at the store.
    let payload = b"artifact-under-outage";
    let digest = hash_bytes(payload);
    let store = Arc::new(OutageStore::new());
    let cas = ArtifactCas::new(
        store.clone(),
        Arc::new(clock()),
        CasConfig::default(),
    )
    .expect("cas");
    cas.put_blob(payload).expect("seed the blob before the outage");

    let cache = Arc::new(LocalCache::new(1 << 20, 16).expect("cache"));
    let engine = TransferEngine::new(cas, cache, ProxyMetrics::default(), 1 << 20);
    let grant = grant(digest);

    let fault = engine
        .read(&grant, digest, 1_000)
        .expect_err("a read during an object-store outage must not succeed");
    assert_eq!(fault.code(), Code::Unavailable);
    assert_eq!(store.reads(), 1, "one read attempt reached the store");

    // Nothing was cached from the failed read: the outage did not leave a partial or empty
    // entry that a later reader would serve as if it were the artifact.
    store.recover();
    let recovered = engine
        .read(&grant, digest, 2_000)
        .expect("the read must succeed once the store is back");
    assert_eq!(recovered.as_slice(), payload.as_slice());
    assert_eq!(store.reads(), 2, "the recovered read went to the store");

    // And the second read is served from the cache, which proves the assertion above was
    // about cache *absence* rather than about the cache never being populated at all.
    let cached = engine.read(&grant, digest, 3_000).expect("cached read");
    assert_eq!(Bytes::from(cached), Bytes::from_static(payload));
    assert_eq!(store.reads(), 2, "the cached read did not reach the store");
}
