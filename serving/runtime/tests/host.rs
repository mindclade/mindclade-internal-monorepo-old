// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use mindclade_content_digest::hash_bytes;
use mindclade_serving_runtime::{BatchCompatibilityKey, BatchEnvelope};

#[test]
fn batch_envelopes_fail_closed_on_incomplete_routing_identity() {
    let mut batch = BatchEnvelope {
        request_id: "request-1".into(),
        key: BatchCompatibilityKey {
            deployment_id: "deployment-1".into(),
            model_bundle: hash_bytes(b"model"),
            execution_class: "gpu".into(),
            precision_class: "bf16".into(),
        },
        estimated_input_units: 10,
        estimated_output_units: 20,
    };
    assert!(batch.validate().is_ok());
    batch.key.deployment_id.clear();
    assert!(batch.validate().is_err());
}
