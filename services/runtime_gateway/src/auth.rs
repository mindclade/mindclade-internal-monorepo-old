// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Authentication/authorization boundary for pre-issued admission grants.
//!
//! Credential/session authentication is provided by the network leaf adapter;
//! this module validates the signed bounded grant consumed by the hot path.

use crate::tickets::PolicyCache;
use mindclade_faults::FaultResult;
use mindclade_worker_protocol::AdmissionGrant;

pub fn validate_admission_grant(
    policy: &PolicyCache,
    grant: &AdmissionGrant,
    now_unix_millis: u64,
) -> FaultResult<()> {
    policy.validate_grant(grant, now_unix_millis)
}
