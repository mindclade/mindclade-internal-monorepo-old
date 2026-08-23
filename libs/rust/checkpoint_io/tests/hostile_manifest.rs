// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Hostile encoded manifests: a small message must never drive a large
//! reservation.
//!
//! `decode` pre-allocates `Vec::with_capacity(shard_count)` straight from a
//! field the sender controls. `CheckpointShard` is ~72 bytes, so a declared
//! million shards reserved ~72 MB — from a message of a few dozen bytes that
//! then failed on the first shard. The bound now comes from the bytes the
//! message actually carries.

use mindclade_checkpoint_io::{CHECKPOINT_SCHEMA, CheckpointManifest, MAX_SHARDS};
use mindclade_content_digest::hash_bytes;
use mindclade_identifiers::ResourceId;
use mindclade_record_io::Encoder;
use mindclade_runtime_core::ManualClock;
use std::time::{Instant, SystemTime};

/// Encodes a manifest prefix that is well-formed right up to the shard count,
/// then declares `shard_count` shards and stops.
fn truncated_after_shard_count(shard_count: u32) -> Option<Vec<u8>> {
    let clock = ManualClock::new(SystemTime::UNIX_EPOCH, Instant::now());
    let checkpoint_id = ResourceId::generate("checkpoint", &clock).ok()?;
    let run_id = ResourceId::generate("run", &clock).ok()?;

    let mut encoder = Encoder::new();
    encoder.u16(CHECKPOINT_SCHEMA);
    encoder.string(&checkpoint_id.to_string()).ok()?;
    encoder.string(&run_id.to_string()).ok()?;
    encoder.u64(10);
    encoder.u32(1);
    encoder.bytes(hash_bytes(b"plan").as_bytes()).ok()?;
    encoder.u32(shard_count);
    Some(encoder.into_bytes())
}

#[test]
fn a_small_message_declaring_a_million_shards_is_rejected() {
    let hostile = truncated_after_shard_count(1_000_000).expect("prefix encodes");
    // The whole attack fits in well under a kilobyte.
    assert!(
        hostile.len() < 1024,
        "fixture grew to {} bytes; it is meant to be tiny",
        hostile.len()
    );
    CheckpointManifest::decode(&hostile)
        .expect_err("a sub-kilobyte message cannot contain a million shards");
}

#[test]
fn every_hostile_shard_declaration_is_rejected_without_a_matching_body() {
    for declared in [1_u32, 2, 1_000, 100_000, 1_000_000, u32::MAX] {
        let hostile = truncated_after_shard_count(declared).expect("prefix encodes");
        assert!(
            CheckpointManifest::decode(&hostile).is_err(),
            "declared {declared} shards with no shard bytes, but decode succeeded"
        );
    }
}

#[test]
fn an_empty_shard_list_is_rejected_before_the_body_is_walked() {
    let hostile = truncated_after_shard_count(0).expect("prefix encodes");
    CheckpointManifest::decode(&hostile).expect_err("a checkpoint with no shards is invalid");
}

#[test]
fn the_declared_count_ceiling_matches_what_validate_enforces() {
    // The decode-time ceiling and the post-decode invariant must not drift
    // apart; that drift is what let the reservation happen before the check.
    assert_eq!(MAX_SHARDS, 1_000_000);
}
