// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use mindclade_node_agent::{DEFAULT_MAXIMUM_DIAGNOSTIC_BYTES, NodeDiagnosticsBundle};
use mindclade_runtime_core::{Budget, ResourceKind, ResourceVector};
use std::collections::BTreeMap;

fn bundle() -> NodeDiagnosticsBundle {
    let root = Budget::root(
        "node",
        ResourceVector::new().set(ResourceKind::ResidentMemoryBytes, 1024),
    );
    NodeDiagnosticsBundle {
        node_id: "node-1".into(),
        runtime_version: "0.0.0".into(),
        generated_unix_millis: 1,
        budget: root.tree_snapshot(),
        active_ticket_ids: vec!["ticket-1".into()],
        process_exits: vec![],
        cache_bytes: 64,
        telemetry_spool_bytes: 32,
        attributes: BTreeMap::from([("region".into(), "us-central1".into())]),
    }
}

#[test]
fn diagnostics_are_bounded_and_deterministic() {
    let bundle = bundle();
    let first = bundle
        .encode(DEFAULT_MAXIMUM_DIAGNOSTIC_BYTES)
        .expect("encode");
    let second = bundle
        .encode(DEFAULT_MAXIMUM_DIAGNOSTIC_BYTES)
        .expect("encode");
    assert_eq!(first, second);
    assert_eq!(
        bundle.estimated_bytes().expect("size"),
        u64::try_from(first.len()).expect("diagnostic length fits u64")
    );
}

#[test]
fn diagnostics_reject_sensitive_attributes() {
    let mut bundle = bundle();
    bundle
        .attributes
        .insert("private_key_id".into(), "forbidden".into());
    assert!(bundle.validate().is_err());
}
