// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use mindclade_faults::{Fault, FaultResult};

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct UnixEndpoint {
    path: String,
}
impl UnixEndpoint {
    pub fn new(path: impl Into<String>) -> FaultResult<Self> {
        let path = path.into();
        if path.is_empty()
            || path.len() > 100
            || !path.starts_with('/')
            || path.contains('\0')
            || path.split('/').any(|component| component == "..")
        {
            return Err(Fault::invalid_argument("unix endpoint path is invalid"));
        }
        Ok(Self { path })
    }
    #[must_use]
    pub fn path(&self) -> &str {
        &self.path
    }
}
