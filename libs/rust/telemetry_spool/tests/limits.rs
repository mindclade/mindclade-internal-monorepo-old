// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use mindclade_bytes_io::ByteSize;
use mindclade_faults::Code;
use mindclade_runtime_core::ManualClock;
use mindclade_telemetry_spool::{DeliveryBatch, SpoolConfig, TelemetrySpool};
use std::fs;
use std::path::{Path, PathBuf};
use std::sync::Arc;
use std::time::{Instant, SystemTime};

const TOTAL_BUDGET: u64 = 16 * 1024;

fn scratch(name: &str) -> PathBuf {
    let root = std::env::temp_dir().join(format!(
        "mindclade-spool-limits-{}-{name}",
        std::process::id()
    ));
    let _ = fs::remove_dir_all(&root);
    root
}

fn config() -> SpoolConfig {
    SpoolConfig {
        maximum_event_bytes: ByteSize::new(1024),
        maximum_segment_bytes: ByteSize::new(4096),
        maximum_total_bytes: ByteSize::new(TOTAL_BUDGET),
    }
}

fn clock() -> Arc<ManualClock> {
    Arc::new(ManualClock::new(SystemTime::UNIX_EPOCH, Instant::now()))
}

fn spool_bytes(root: &Path) -> u64 {
    fs::read_dir(root)
        .expect("spool root is readable")
        .map(|entry| {
            let entry = entry.expect("spool entry is readable");
            let path = entry.path();
            let is_segment = entry.file_name().to_string_lossy().starts_with("segment-")
                && path
                    .extension()
                    .is_some_and(|extension| extension.eq_ignore_ascii_case("mcrd"));
            if is_segment {
                entry.metadata().expect("segment is readable").len()
            } else {
                0
            }
        })
        .sum()
}

#[test]
fn empty_batch_is_valid() {
    let b = DeliveryBatch::new(Vec::new(), 0).unwrap();
    assert!(b.highest_sequence().is_none());
}

/// The spool's overflow policy is explicit backpressure — reject the newest
/// append with `ResourceExhausted` — rather than unbounded growth. Drive past
/// the disk budget with an unavailable sink (nothing is ever acknowledged) and
/// assert the spool stops accepting instead of filling the node, then assert
/// that draining the backlog restores admission.
#[test]
fn disk_budget_rejects_appends_instead_of_growing_without_limit() {
    let root = scratch("disk-budget");
    let spool = TelemetrySpool::open(&root, config(), clock()).expect("open spool");
    let payload = [0_u8; 512];

    let mut accepted = 0_u64;
    let mut rejection = None;
    for _ in 0..1024 {
        match spool.append("training.step", &payload) {
            Ok(_) => accepted += 1,
            Err(fault) => {
                rejection = Some(fault);
                break;
            }
        }
        assert!(
            spool_bytes(&root) <= TOTAL_BUDGET,
            "spool exceeded its disk budget after {accepted} appends"
        );
    }

    let rejection = rejection.expect("the spool must stop accepting at its disk budget");
    assert_eq!(rejection.code(), Code::ResourceExhausted);
    assert!(accepted > 0, "the budget must admit some events");
    assert!(spool_bytes(&root) <= TOTAL_BUDGET);

    // Draining the backlog must return budget: the spool is bounded, not
    // permanently wedged.
    let batch = DeliveryBatch::new(
        spool.replay_after(0, 100_000).expect("replay succeeds"),
        u64::MAX,
    )
    .expect("spooled sequences are strictly increasing");
    let highest = batch.highest_sequence().expect("batch is not empty");
    spool.acknowledge(highest).expect("acknowledge succeeds");
    assert!(spool.compact().expect("compaction succeeds") > 0);
    spool
        .append("training.step", &payload)
        .expect("a drained spool admits new events again");
    let _ = fs::remove_dir_all(&root);
}

/// A single event larger than `maximum_event_bytes` is rejected before any
/// encoding or disk work, so an oversized producer cannot burst the segment
/// bound.
#[test]
fn oversized_event_is_rejected_before_it_reaches_a_segment() {
    let root = scratch("oversized-event");
    let spool = TelemetrySpool::open(&root, config(), clock()).expect("open spool");
    let oversized = vec![0_u8; 1025];
    let fault = spool
        .append("training.step", &oversized)
        .expect_err("an oversized event must be rejected");
    assert_eq!(fault.code(), Code::ResourceExhausted);
    assert_eq!(spool_bytes(&root), 0);
    let _ = fs::remove_dir_all(&root);
}
