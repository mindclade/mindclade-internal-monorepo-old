// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use crate::LocalStore;
use mindclade_faults::FaultResult;
use std::path::Path;
pub fn open(root: &Path) -> FaultResult<LocalStore> {
    LocalStore::new(root)
}
