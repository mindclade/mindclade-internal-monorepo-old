// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use mindclade_bytes_io::ByteSize;
use mindclade_faults::Code;
use mindclade_runtime_core::ManualClock;
use mindclade_telemetry_spool::journal::JournalPosition;
use mindclade_telemetry_spool::{DeliveryBatch, SpoolConfig, TelemetrySpool};
use std::fs;
use std::io::{Seek, SeekFrom, Write};
use std::path::{Path, PathBuf};
use std::sync::Arc;
use std::time::{Instant, SystemTime};

fn scratch(name: &str) -> PathBuf {
    let root = std::env::temp_dir().join(format!(
        "mindclade-spool-recovery-{}-{name}",
        std::process::id()
    ));
    let _ = fs::remove_dir_all(&root);
    root
}

fn config() -> SpoolConfig {
    SpoolConfig {
        maximum_event_bytes: ByteSize::new(1024),
        maximum_segment_bytes: ByteSize::new(4096),
        maximum_total_bytes: ByteSize::new(16 * 1024),
    }
}

fn clock() -> Arc<ManualClock> {
    Arc::new(ManualClock::new(SystemTime::UNIX_EPOCH, Instant::now()))
}

fn segment_files(root: &Path) -> Vec<PathBuf> {
    let mut paths = fs::read_dir(root)
        .expect("spool root is readable")
        .filter_map(|entry| {
            let path = entry.expect("spool entry is readable").path();
            let name = path.file_name()?.to_string_lossy().into_owned();
            let is_segment = name.starts_with("segment-")
                && path
                    .extension()
                    .is_some_and(|extension| extension.eq_ignore_ascii_case("mcrd"));
            is_segment.then_some(path)
        })
        .collect::<Vec<_>>();
    paths.sort();
    paths
}

#[test]
fn pending_is_exact_and_inconsistent_positions_fail() {
    let position = JournalPosition {
        acknowledged: 5,
        next_sequence: 9,
    };
    assert_eq!(position.pending().expect("valid position"), 3);
    let corrupt = JournalPosition {
        acknowledged: 9,
        next_sequence: 9,
    };
    assert!(corrupt.pending().is_err());
    let overflow = JournalPosition {
        acknowledged: u64::MAX,
        next_sequence: u64::MAX,
    };
    assert!(overflow.pending().is_err());
}

/// `append` publishes the sequence counter *after* the record frame it
/// describes is durable. A crash inside that window leaves the counter behind
/// the spool contents. If `open` trusted it verbatim the spool would re-issue a
/// sequence that already exists on disk, every replay would fail
/// `DeliveryBatch`'s strict-monotonicity check, nothing would ever be
/// acknowledged, compaction would reclaim nothing, and the spool would wedge at
/// its disk budget for the life of the node.
#[test]
fn crash_before_counter_publication_does_not_reissue_a_durable_sequence() {
    let root = scratch("counter-window");
    let spool = TelemetrySpool::open(&root, config(), clock()).expect("open spool");
    let first = spool
        .append("training.step", b"one")
        .expect("first append succeeds");
    assert_eq!(first.sequence, 1);
    drop(spool);

    // The record frame reached the disk; the counter publication did not.
    fs::remove_file(root.join("sequence")).expect("counter file exists");

    let spool = TelemetrySpool::open(&root, config(), clock()).expect("reopen spool");
    let second = spool
        .append("training.step", b"two")
        .expect("second append succeeds");
    assert!(
        second.sequence > first.sequence,
        "reopened spool re-issued sequence {} which is already durable",
        second.sequence
    );

    let replayed = spool.replay_after(0, 100).expect("replay succeeds");
    assert_eq!(replayed.len(), 2);
    let batch = DeliveryBatch::new(replayed, 1 << 20)
        .expect("replay after the crash window is still strictly increasing");
    let highest = batch.highest_sequence().expect("batch is not empty");

    // The delivery path is not poisoned: the batch can still be acknowledged
    // and the acknowledged segments reclaimed.
    spool.acknowledge(highest).expect("acknowledge succeeds");
    spool.compact().expect("compaction succeeds");
    let _ = fs::remove_dir_all(&root);
}

/// A crash can also interrupt `RecordWriter::write` partway through a frame.
/// Open-time sequence recovery reads that segment, so it must treat an
/// unreadable trailing frame as an expected on-disk state rather than a reason
/// to fail: a spool that cannot be opened is a spool that can never drain.
///
/// This pins only that `open` stays non-fatal. It deliberately does not claim
/// the spool is healthy afterwards — `replay_after` and `compact` remain strict
/// about a torn frame, so repairing one is a separate concern from the counter
/// window this module's first test covers.
#[test]
fn torn_trailing_frame_keeps_the_spool_openable() {
    let root = scratch("torn-tail");
    let spool = TelemetrySpool::open(&root, config(), clock()).expect("open spool");
    spool
        .append("training.step", b"one")
        .expect("first append succeeds");
    let second = spool
        .append("training.step", b"two")
        .expect("second append succeeds");
    drop(spool);

    let segments = segment_files(&root);
    let segment = segments.last().expect("a segment was written");
    let length = fs::metadata(segment).expect("segment is readable").len();
    assert!(length > 8, "segment is large enough to truncate");
    let file = fs::OpenOptions::new()
        .write(true)
        .open(segment)
        .expect("segment is writable");
    // Drop the final bytes so the trailing frame is structurally incomplete.
    file.set_len(length - 8).expect("segment truncates");
    file.sync_all().expect("truncation is durable");
    drop(file);

    let spool = TelemetrySpool::open(&root, config(), clock())
        .expect("a torn trailing frame must not make the spool unopenable");
    assert!(
        spool
            .append("training.step", b"three")
            .expect("append after recovery succeeds")
            .sequence
            > second.sequence
    );
    let _ = fs::remove_dir_all(&root);
}

/// Compaction decides whether a segment may be deleted, so unlike open-time
/// recovery it must refuse to treat an unreadable frame as absent — otherwise a
/// segment holding unacknowledged events could look fully acknowledged and be
/// discarded.
#[test]
fn compaction_refuses_to_delete_a_segment_it_cannot_fully_read() {
    let root = scratch("compact-corrupt");
    let spool = TelemetrySpool::open(&root, config(), clock()).expect("open spool");
    spool
        .append("training.step", b"one")
        .expect("first append succeeds");
    let second = spool
        .append("training.step", b"two")
        .expect("second append succeeds");
    spool
        .acknowledge(second.sequence)
        .expect("acknowledge succeeds");
    drop(spool);

    let segments = segment_files(&root);
    let segment = segments.last().expect("a segment was written");
    let mut file = fs::OpenOptions::new()
        .write(true)
        .open(segment)
        .expect("segment is writable");
    // Corrupt the magic of the first frame so the whole segment is unreadable.
    file.seek(SeekFrom::Start(0)).expect("segment seeks");
    file.write_all(b"XXXX").expect("segment is writable");
    file.sync_all().expect("corruption is durable");
    drop(file);

    let spool = TelemetrySpool::open(&root, config(), clock()).expect("reopen spool");
    let fault = spool
        .compact()
        .expect_err("compaction must not delete a segment it cannot read");
    assert_eq!(fault.code(), Code::DataLoss);
    assert!(
        segment.exists(),
        "the unreadable segment must survive compaction"
    );
    let _ = fs::remove_dir_all(&root);
}
