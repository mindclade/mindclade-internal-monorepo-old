// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Declared-item-count bounds for untrusted encoded messages.
//!
//! Every caller of `item_count` pre-allocates from the value it returns, so the
//! accepted count *is* the allocation bound. These tests assert that bound
//! directly rather than trying to measure the heap: a tracking global allocator
//! would need `unsafe impl GlobalAlloc`, and `unsafe_code` is denied workspace
//! wide.

use mindclade_record_io::{Decoder, Encoder};

const MESSAGE_LIMIT: usize = 64 * 1024 * 1024;

/// Encodes a declared count followed by `payload_bytes` bytes of item payload.
fn declared(count: u32, payload_bytes: usize) -> Vec<u8> {
    let mut encoder = Encoder::new();
    encoder.u32(count);
    for _ in 0..payload_bytes {
        encoder.u8(7);
    }
    encoder.into_bytes()
}

#[test]
fn a_tiny_message_cannot_declare_a_huge_item_count() {
    // The amplifier: four bytes of count plus one byte of payload claiming a
    // million items. The caller would reserve for a million records and only
    // then fail to decode item 2.
    let bytes = declared(1_000_000, 1);
    assert_eq!(bytes.len(), 5);
    let mut decoder = Decoder::new(&bytes, MESSAGE_LIMIT).expect("decoder");
    decoder
        .item_count()
        .expect_err("a 5-byte message cannot hold a million items");
}

#[test]
fn accepted_item_count_never_exceeds_the_bytes_left_to_encode_it() {
    // The property that makes `Vec::with_capacity(count)` safe at every call
    // site: whatever count survives, the message had at least that many bytes
    // left, so the reservation is bounded by the message rather than by a
    // constant ceiling.
    for declared_count in [0_u32, 1, 2, 10, 1_000, 100_000, 1_000_000, u32::MAX] {
        for payload_bytes in [0_usize, 1, 8, 64] {
            let bytes = declared(declared_count, payload_bytes);
            let mut decoder = Decoder::new(&bytes, MESSAGE_LIMIT).expect("decoder");
            if let Ok(count) = decoder.item_count() {
                assert!(
                    count <= payload_bytes,
                    "accepted {count} items from {payload_bytes} remaining bytes \
                     (declared {declared_count})"
                );
            }
        }
    }
}

#[test]
fn a_well_formed_count_is_still_accepted_at_the_boundary() {
    // One byte per item is the tightest a message can legitimately be, so this
    // is the exact edge the bound must not over-reject.
    let bytes = declared(64, 64);
    let mut decoder = Decoder::new(&bytes, MESSAGE_LIMIT).expect("decoder");
    assert_eq!(decoder.item_count().expect("boundary count"), 64);

    let empty = declared(0, 0);
    let mut decoder = Decoder::new(&empty, MESSAGE_LIMIT).expect("decoder");
    assert_eq!(decoder.item_count().expect("empty list"), 0);
}

#[test]
fn the_absolute_ceiling_still_applies_to_a_large_message() {
    // A message big enough to satisfy the remaining-bytes bound must still be
    // refused by `max_items`, so the new bound only ever tightens the old one.
    let decoder = Decoder::new(&[], MESSAGE_LIMIT).expect("decoder");
    let ceiling = decoder.max_items();
    let bytes = declared(
        u32::try_from(ceiling).expect("ceiling fits u32") + 1,
        ceiling + 8,
    );
    let mut decoder = Decoder::new(&bytes, MESSAGE_LIMIT).expect("decoder");
    decoder
        .item_count()
        .expect_err("a count above max_items is refused even with the bytes to back it");
}
