// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use mindclade_faults::{Fault, FaultResult};
use std::collections::BTreeSet;

const MAX_EXECUTABLE_BYTES: usize = 4096;
const MAX_ARGUMENTS: usize = 256;
const MAX_ARGUMENT_BYTES: usize = 16 * 1024;
const MAX_ENVIRONMENT: usize = 256;
const MAX_ENVIRONMENT_BYTES: usize = 64 * 1024;

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ModelProcessSpec {
    pub executable: String,
    pub args: Vec<String>,
    pub environment: Vec<(String, String)>,
}

impl ModelProcessSpec {
    pub fn validate(&self) -> FaultResult<()> {
        if self.executable.is_empty()
            || self.executable.len() > MAX_EXECUTABLE_BYTES
            || self.executable.contains('\0')
            || self.args.len() > MAX_ARGUMENTS
            || self.environment.len() > MAX_ENVIRONMENT
        {
            return Err(Fault::invalid_argument(
                "model process specification is invalid",
            ));
        }
        let argument_bytes = self.args.iter().try_fold(0usize, |total, argument| {
            if argument.contains('\0') {
                return Err(Fault::invalid_argument(
                    "model process argument contains NUL",
                ));
            }
            total
                .checked_add(argument.len())
                .ok_or_else(|| Fault::invalid_argument("model process argument bytes overflow"))
        })?;
        if argument_bytes > MAX_ARGUMENT_BYTES {
            return Err(Fault::invalid_argument(
                "model process arguments exceed byte budget",
            ));
        }
        let mut names = BTreeSet::new();
        let environment_bytes =
            self.environment
                .iter()
                .try_fold(0usize, |total, (key, value)| {
                    if key.is_empty()
                        || key.len() > 256
                        || key.contains('=')
                        || key.contains('\0')
                        || value.contains('\0')
                        || !names.insert(key.as_str())
                    {
                        return Err(Fault::invalid_argument(
                            "model process environment is invalid",
                        ));
                    }
                    total
                        .checked_add(key.len())
                        .and_then(|size| size.checked_add(value.len()))
                        .ok_or_else(|| {
                            Fault::invalid_argument("model process environment bytes overflow")
                        })
                })?;
        if environment_bytes > MAX_ENVIRONMENT_BYTES {
            return Err(Fault::invalid_argument(
                "model process environment exceeds byte budget",
            ));
        }
        Ok(())
    }
}
