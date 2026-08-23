// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Stage counters now have an exposition body. The zero-registration is the
//! part worth a test: an alert on `stage_failed` cannot fire against a series
//! that only comes into existence on the first failure.

use mindclade_node_agent::telemetry::NodeMetrics;

#[test]
fn stage_counters_are_published_at_zero_before_any_stage_runs() {
    assert_eq!(
        NodeMetrics::default().prometheus(),
        "# TYPE mindclade_node_agent_stage_completed_total counter\n\
         mindclade_node_agent_stage_completed_total 0\n\
         # TYPE mindclade_node_agent_stage_failed_total counter\n\
         mindclade_node_agent_stage_failed_total 0\n\
         # TYPE mindclade_node_agent_stage_started_total counter\n\
         mindclade_node_agent_stage_started_total 0\n"
    );
}

#[test]
fn stage_outcomes_reach_the_exposition() {
    let metrics = NodeMetrics::default();
    metrics.stage_started();
    metrics.stage_started();
    metrics.stage_failed();
    let body = metrics.prometheus();
    assert!(body.contains("\nmindclade_node_agent_stage_started_total 2\n"));
    assert!(body.contains("\nmindclade_node_agent_stage_failed_total 1\n"));
    assert!(body.contains("\nmindclade_node_agent_stage_completed_total 0\n"));
}
