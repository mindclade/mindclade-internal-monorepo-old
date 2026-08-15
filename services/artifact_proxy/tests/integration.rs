// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use mindclade_artifact_proxy::{LocalCache, ProxyConfig, ProxyHealth};

#[test]
fn config_and_cache_are_bounded() {
    let config = ProxyConfig {
        maximum_read_bytes: 1024,
        maximum_write_bytes: 1024,
        maximum_range_bytes: 512,
        maximum_cache_bytes: 1024,
        maximum_cache_entries: 2,
        maximum_concurrent_transfers: 4,
    };
    assert!(config.validate().is_ok());
    let cache = LocalCache::new(1024, 2).expect("cache");
    assert_eq!(cache.entries(), 0);
    let health = ProxyHealth::new();
    assert!(health.snapshot().accepting);
    health.drain();
    assert!(!health.snapshot().accepting);
}

#[test]
fn transfer_permits_are_strictly_bounded() {
    let health = ProxyHealth::new();
    assert!(health.try_begin(2));
    assert!(health.try_begin(2));
    assert!(!health.try_begin(2));
    assert_eq!(health.snapshot().active_transfers, 2);
    let _ = health.end();
    assert!(health.try_begin(2));
    let _ = health.end();
    let _ = health.end();
    assert_eq!(health.snapshot().active_transfers, 0);
}

#[test]
fn transfer_accounting_underflow_fails_closed() {
    let health = ProxyHealth::new();
    assert!(!health.end());
    let snapshot = health.snapshot();
    assert!(!snapshot.accounting_healthy);
    assert!(!snapshot.accepting);
    assert!(!health.begin());
}
