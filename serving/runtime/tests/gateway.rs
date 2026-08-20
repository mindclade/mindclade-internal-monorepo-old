// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use mindclade_identifiers::ResourceId;
use mindclade_serving_runtime::{AdmissionLedger, AdmissionRequest};
use mindclade_worker_protocol::AdmissionGrantClaims;
use std::collections::BTreeSet;

fn id(kind: &str, suffix: &str) -> ResourceId {
    format!("{kind}_01890f2c7b7a70008{suffix}")
        .parse()
        .expect("valid resource id")
}

fn grant() -> AdmissionGrantClaims {
    AdmissionGrantClaims {
        grant_id: id("grant", "000000000000001"),
        tenant_id: id("tenant", "000000000000002"),
        principal_id: "principal:test".into(),
        allowed_deployments: BTreeSet::new(),
        allowed_capabilities: BTreeSet::new(),
        region: "us".into(),
        maximum_concurrency: 2,
        maximum_requests: 4,
        maximum_input_units: 100,
        maximum_output_units: 100,
        not_before_unix_millis: 1,
        expires_unix_millis: 100,
        policy_epoch: 1,
        revocation_epoch: 1,
    }
}

fn request() -> AdmissionRequest {
    AdmissionRequest {
        request_key: b"request".to_vec(),
        deployment_hint: None,
        required_capabilities: BTreeSet::new(),
        input_units: 1,
        output_units: 1,
    }
}

#[test]
fn drain_is_linearized_with_local_admission() {
    let ledger = AdmissionLedger::new(2, 2).expect("ledger");
    let first = ledger.reserve(&grant(), &request()).expect("first permit");
    ledger.begin_drain();
    assert!(ledger.reserve(&grant(), &request()).is_err());
    assert_eq!(ledger.active(), 1);
    drop(first);
    assert_eq!(ledger.active(), 0);
    ledger.resume();
    assert!(ledger.reserve(&grant(), &request()).is_ok());
}
