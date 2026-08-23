// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! `/metrics` is served from `GatewayMetrics::prometheus`, whose body moved
//! from fifteen hand-written lines in this crate to the shared counter
//! registry. These assertions pin the body so the move cannot change what a
//! scrape sees.

use mindclade_ai_gateway_proxy::telemetry::GatewayMetrics;

#[test]
fn every_series_is_published_before_any_traffic() {
    let metrics = GatewayMetrics::default();
    let body = metrics.prometheus();
    for name in [
        "accepted",
        "rejected",
        "dispatched",
        "committed",
        "reconciliation_pending",
        "reconciled",
    ] {
        let series = format!("mindclade_ai_gateway_{name}_total");
        assert!(
            body.contains(&format!("# TYPE {series} counter\n")),
            "missing TYPE line for {series}"
        );
        assert!(
            body.contains(&format!("\n{series} 0\n")) || body.starts_with(&format!("{series} 0\n")),
            "missing zero sample for {series}"
        );
    }
}

#[test]
fn the_exposition_body_is_exact_and_deterministic() {
    let metrics = GatewayMetrics::default();
    metrics.accepted();
    metrics.accepted();
    metrics.dispatched();

    // Same affixes, same `# TYPE` lines, and the same six series the
    // hand-rolled renderer produced. Ordering is the registry's deterministic
    // key order rather than the old declaration order; Prometheus does not
    // depend on ordering across metric families, and a determinstic order is
    // what makes this assertion stable.
    assert_eq!(
        metrics.prometheus(),
        "# TYPE mindclade_ai_gateway_accepted_total counter\n\
         mindclade_ai_gateway_accepted_total 2\n\
         # TYPE mindclade_ai_gateway_committed_total counter\n\
         mindclade_ai_gateway_committed_total 0\n\
         # TYPE mindclade_ai_gateway_dispatched_total counter\n\
         mindclade_ai_gateway_dispatched_total 1\n\
         # TYPE mindclade_ai_gateway_reconciled_total counter\n\
         mindclade_ai_gateway_reconciled_total 0\n\
         # TYPE mindclade_ai_gateway_reconciliation_pending_total counter\n\
         mindclade_ai_gateway_reconciliation_pending_total 0\n\
         # TYPE mindclade_ai_gateway_rejected_total counter\n\
         mindclade_ai_gateway_rejected_total 0\n"
    );
    assert_eq!(metrics.prometheus(), metrics.prometheus());
}

#[test]
fn the_snapshot_keys_callers_already_use_are_unchanged() {
    // `tests/integration.rs` reads `ai_gateway.committed` and
    // `ai_gateway.reconciled` from the snapshot; the registry keys keep their
    // dotted spelling and only the rendered series name folds `.` to `_`.
    let metrics = GatewayMetrics::default();
    metrics.committed();
    assert_eq!(metrics.snapshot().get("ai_gateway.committed"), Some(&1));
}
