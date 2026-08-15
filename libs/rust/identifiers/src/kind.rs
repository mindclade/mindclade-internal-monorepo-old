// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use mindclade_faults::{Fault, FaultResult};

#[derive(Clone, Debug, Eq, Hash, Ord, PartialEq, PartialOrd)]
pub struct ResourceKind(String);

impl ResourceKind {
    pub fn parse(value: impl Into<String>) -> FaultResult<Self> {
        let v = value.into();
        if v.is_empty()
            || v.len() > 48
            || !v.bytes().enumerate().all(|(i, b)| {
                b.is_ascii_lowercase() || b.is_ascii_digit() || (i > 0 && matches!(b, b'_' | b'-'))
            })
        {
            return Err(Fault::invalid_argument("resource kind is invalid"));
        }
        Ok(Self(v))
    }
    #[must_use]
    pub fn as_str(&self) -> &str {
        &self.0
    }
}
