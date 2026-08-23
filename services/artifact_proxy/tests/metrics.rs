// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! The proxy kept these counters from the start and had no way to export
//! them: `/metrics` existed on exactly one Rust service. The exposition body
//! now exists here too.

use mindclade_artifact_proxy::ProxyMetrics;

#[test]
fn every_series_is_published_before_any_traffic() {
    let body = ProxyMetrics::default().prometheus();
    for series in [
        "mindclade_artifact_proxy_read_requests_total",
        "mindclade_artifact_proxy_read_bytes_total",
        "mindclade_artifact_proxy_write_requests_total",
        "mindclade_artifact_proxy_write_bytes_total",
        "mindclade_artifact_proxy_cache_hits_total",
        "mindclade_artifact_proxy_rejected_total",
    ] {
        assert!(
            body.contains(&format!("# TYPE {series} counter\n{series} 0\n")),
            "missing zero series for {series}"
        );
    }
}

#[test]
fn transfer_accounting_reaches_the_exposition() {
    let metrics = ProxyMetrics::default();
    metrics.read(4096);
    metrics.read(1024);
    metrics.cache_hit();
    let body = metrics.prometheus();
    assert!(body.contains("\nmindclade_artifact_proxy_read_requests_total 2\n"));
    assert!(body.contains("\nmindclade_artifact_proxy_read_bytes_total 5120\n"));
    assert!(body.contains("\nmindclade_artifact_proxy_cache_hits_total 1\n"));
    // Untouched counters still report, rather than vanishing from the scrape.
    assert!(body.contains("\nmindclade_artifact_proxy_rejected_total 0\n"));
    assert_eq!(
        metrics.snapshot().get("artifact_proxy.read_bytes"),
        Some(&5120)
    );
}
