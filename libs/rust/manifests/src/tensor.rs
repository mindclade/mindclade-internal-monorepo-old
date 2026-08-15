// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use crate::{ArtifactRef, validation};
use mindclade_faults::{Code, Fault, FaultResult};

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct TensorManifest {
    pub schema_version: u32,
    pub artifact: ArtifactRef,
    pub dtype: String,
    pub shape: Vec<u64>,
    pub strides: Option<Vec<u64>>,
    pub byte_offset: u64,
}

impl TensorManifest {
    pub fn validate(&self) -> FaultResult<()> {
        validation::validate_schema_version(self.schema_version)?;
        self.artifact.validate()?;
        if self.dtype.is_empty()
            || self.shape.is_empty()
            || self.shape.len() > 16
            || self.shape.contains(&0)
        {
            return Err(Fault::invalid_argument("tensor dtype/shape is invalid"));
        }
        if let Some(s) = &self.strides {
            if s.len() != self.shape.len() {
                return Err(Fault::invalid_argument("tensor strides do not match rank"));
            }
        }
        let elements = self.shape.iter().try_fold(1u128, |n, d| {
            n.checked_mul(u128::from(*d))
                .ok_or_else(|| Fault::new(Code::OutOfRange, "tensor element count overflow"))
        })?;
        if elements > u128::from(u64::MAX) {
            return Err(Fault::new(Code::OutOfRange, "tensor is too large"));
        }
        Ok(())
    }
}
