// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use crate::{ContextValue, Fault, RetryHint};

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct WireContext {
    pub key: String,
    pub value: String,
}
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct WireFault {
    pub code: String,
    pub message: String,
    pub context: Vec<WireContext>,
    pub retry_after_millis: Option<u64>,
}

impl From<&Fault> for WireFault {
    fn from(fault: &Fault) -> Self {
        let context = fault
            .context()
            .iter()
            .map(|(key, value)| WireContext {
                key: key.to_owned(),
                value: match value {
                    ContextValue::Sensitive => "[REDACTED]".to_owned(),
                    _ => value.to_string(),
                },
            })
            .collect();
        // A RetryHint larger than the wire representation is treated as
        // non-retryable rather than silently clamped to an unrelated delay.
        let retry_after_millis = match fault.retry_hint() {
            RetryHint::After(duration) => u64::try_from(duration.as_millis()).ok(),
            RetryHint::Immediate => Some(0),
            RetryHint::Never => None,
        };
        Self {
            code: fault.code().as_str().to_owned(),
            message: fault.message().to_owned(),
            context,
            retry_after_millis,
        }
    }
}
