// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use mindclade_faults::Fault;
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct BridgeError {
    pub code: String,
    pub message: String,
}
impl From<Fault> for BridgeError {
    fn from(value: Fault) -> Self {
        Self {
            code: value.code().to_string(),
            message: value.message().to_owned(),
        }
    }
}
