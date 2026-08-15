// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

pub use crate::digest::Algorithm;
#[must_use]
pub const fn default_algorithm() -> Algorithm {
    Algorithm::Sha256
}
