// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! The Prometheus exposition moved out of `services/ai_gateway_proxy` and into
//! the shared registry, so it is now the one renderer every Rust service uses.
//! These tests pin the rendering and the admission rules that keep it
//! well-formed and bounded.

use mindclade_telemetry::{CounterRegistry, prometheus_series_name, valid_counter_name};

#[test]
fn exposition_matches_the_format_the_gateway_already_serves() {
    let registry = CounterRegistry::default();
    assert!(registry.register("ai_gateway.accepted"));
    assert!(registry.register("ai_gateway.rejected"));
    assert!(registry.add("ai_gateway.accepted", 3));

    // Byte-for-byte the shape `GatewayMetrics::prometheus` produced by hand,
    // including the `# TYPE` line and the `mindclade_`/`_total` affixes, so
    // lifting it into the shared crate did not change what a scrape sees.
    assert_eq!(
        registry.prometheus_text(),
        "# TYPE mindclade_ai_gateway_accepted_total counter\n\
         mindclade_ai_gateway_accepted_total 3\n\
         # TYPE mindclade_ai_gateway_rejected_total counter\n\
         mindclade_ai_gateway_rejected_total 0\n"
    );
}

#[test]
fn registration_exports_a_zero_series_and_never_resets_one() {
    let registry = CounterRegistry::default();
    assert!(registry.register("node_agent.stage_failed"));
    // An absent series is indistinguishable from missing instrumentation, and
    // a series that first appears mid-window makes its own first `rate()`
    // sample wrong. Registering publishes the zero.
    assert_eq!(
        registry.snapshot().get("node_agent.stage_failed").copied(),
        Some(0)
    );

    assert!(registry.add("node_agent.stage_failed", 7));
    assert!(registry.register("node_agent.stage_failed"));
    assert_eq!(
        registry.snapshot().get("node_agent.stage_failed").copied(),
        Some(7),
        "re-registering must not reset an accumulating counter"
    );
}

#[test]
fn counter_names_follow_the_go_tier_rule() {
    // Same rule as `libs/go/observability.validMetricName`, so one fleet
    // metric is nameable from both tiers.
    for good in [
        "a",
        "ai_gateway.accepted",
        "artifact_proxy.read_bytes",
        "stage0.count",
    ] {
        assert!(valid_counter_name(good), "{good} should be valid");
    }
    for bad in [
        "",
        "AI_gateway.accepted",   // uppercase
        "0leading.digit",        // must start with a letter
        "trailing.",             // trailing separator
        "double__separator",     // adjacent separators
        "has space",             // not in [a-z0-9._]
        "has-hyphen",            // Prometheus cannot spell a hyphen
        "inject\nmindclade_x 1", // a newline would forge a sample line
    ] {
        assert!(!valid_counter_name(bad), "{bad:?} should be rejected");
    }
}

#[test]
fn a_rejected_name_never_reaches_the_exposition() {
    let registry = CounterRegistry::default();
    // The load-bearing case: a name carrying a newline would otherwise render
    // as a second, forged sample line in the scrape body.
    assert!(!registry.add("forged\nmindclade_injected_total 99", 1));
    assert!(registry.is_empty());
    assert_eq!(registry.prometheus_text(), "");
}

#[test]
fn colliding_series_names_are_refused_rather_than_interleaved() {
    let registry = CounterRegistry::default();
    assert!(registry.add("stage.failed", 1));
    // `.` folds to `_` for Prometheus, so `stage_failed` would render as the
    // same series. Two counters sharing one series is invisible at the scrape,
    // so the second name is refused at admission instead.
    assert_eq!(
        prometheus_series_name("stage.failed"),
        prometheus_series_name("stage_failed")
    );
    assert!(!registry.add("stage_failed", 1));
    assert_eq!(registry.len(), 1);
}

#[test]
fn cardinality_and_overflow_are_bounded() {
    let registry = CounterRegistry::default();
    for index in 0..CounterRegistry::MAX_COUNTERS {
        assert!(registry.add(&format!("bounded.n{index}"), 1));
    }
    assert_eq!(registry.len(), CounterRegistry::MAX_COUNTERS);
    // A registry backs a `/metrics` body. An unbounded name space is an
    // unbounded response and an unbounded server-side map.
    assert!(!registry.add("bounded.overflow", 1));
    assert_eq!(registry.len(), CounterRegistry::MAX_COUNTERS);
    // An existing counter still accepts increments once the cap is reached.
    assert!(registry.add("bounded.n0", 1));
    assert_eq!(registry.snapshot().get("bounded.n0").copied(), Some(2));

    let saturating = CounterRegistry::default();
    assert!(saturating.add("bounded.limit", u64::MAX));
    // Never wrap and never saturate: a counter that silently rolls over lies
    // to every `rate()` computed across the rollover.
    assert!(!saturating.add("bounded.limit", 1));
    assert_eq!(
        saturating.snapshot().get("bounded.limit").copied(),
        Some(u64::MAX)
    );
}

#[test]
fn a_name_longer_than_the_bound_is_refused() {
    let registry = CounterRegistry::default();
    let long = format!("a{}", "b".repeat(CounterRegistry::MAX_NAME_LEN));
    assert!(!registry.add(&long, 1));
    assert!(registry.add(&long[..CounterRegistry::MAX_NAME_LEN], 1));
}
