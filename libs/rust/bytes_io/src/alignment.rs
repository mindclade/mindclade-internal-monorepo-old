// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

pub use crate::spec::Alignment;
pub fn align_up(value: u64, alignment: Alignment) -> mindclade_faults::FaultResult<u64> {
    alignment.align_up(value)
}
