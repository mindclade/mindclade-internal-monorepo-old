// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Declarative resource-limit builder.
use super::{ResourceKind, ResourceVector};
#[derive(Clone, Debug, Default, Eq, PartialEq)]
pub struct ResourceLimits {
    values: ResourceVector,
}
impl ResourceLimits {
    #[must_use]
    pub fn new() -> Self {
        Self::default()
    }
    #[must_use]
    pub fn limit(mut self, kind: ResourceKind, amount: u64) -> Self {
        self.values = self.values.set(kind, amount);
        self
    }
    #[must_use]
    pub fn as_vector(&self) -> ResourceVector {
        self.values.clone()
    }
    #[must_use]
    pub fn into_vector(self) -> ResourceVector {
        self.values
    }
}
