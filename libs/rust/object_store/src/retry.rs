// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use mindclade_faults::{Code, Fault};
#[must_use]
pub const fn retryable(code: Code) -> bool {
    matches!(
        code,
        Code::Unavailable
            | Code::Aborted
            | Code::ResourceExhausted
            | Code::DeadlineExceeded
            | Code::Conflict
    )
}
#[must_use]
pub fn classify(fault: &Fault) -> bool {
    retryable(fault.code())
}
