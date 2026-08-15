// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Stable machine-readable error codes.

use core::fmt;
use core::str::FromStr;

/// Stable classes used across process and protocol boundaries.
#[derive(Clone, Copy, Debug, Eq, Hash, Ord, PartialEq, PartialOrd)]
#[non_exhaustive]
pub enum Code {
    InvalidArgument,
    NotFound,
    AlreadyExists,
    FailedPrecondition,
    Aborted,
    OutOfRange,
    Unimplemented,
    Internal,
    Unavailable,
    DataLoss,
    Unauthenticated,
    PermissionDenied,
    ResourceExhausted,
    DeadlineExceeded,
    Cancelled,
    Conflict,
}

impl Code {
    /// Returns the stable snake-case wire representation.
    #[must_use]
    pub const fn as_str(self) -> &'static str {
        match self {
            Self::InvalidArgument => "invalid_argument",
            Self::NotFound => "not_found",
            Self::AlreadyExists => "already_exists",
            Self::FailedPrecondition => "failed_precondition",
            Self::Aborted => "aborted",
            Self::OutOfRange => "out_of_range",
            Self::Unimplemented => "unimplemented",
            Self::Internal => "internal",
            Self::Unavailable => "unavailable",
            Self::DataLoss => "data_loss",
            Self::Unauthenticated => "unauthenticated",
            Self::PermissionDenied => "permission_denied",
            Self::ResourceExhausted => "resource_exhausted",
            Self::DeadlineExceeded => "deadline_exceeded",
            Self::Cancelled => "cancelled",
            Self::Conflict => "conflict",
        }
    }
    /// Whether failures with this code are commonly transient.
    #[must_use]
    pub const fn is_transient(self) -> bool {
        matches!(
            self,
            Self::Aborted
                | Self::Unavailable
                | Self::ResourceExhausted
                | Self::DeadlineExceeded
                | Self::Conflict
        )
    }
}

impl fmt::Display for Code {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter.write_str(self.as_str())
    }
}

/// Returned when an unknown wire code is parsed.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ParseCodeError(String);

impl fmt::Display for ParseCodeError {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(formatter, "unknown error code: {}", self.0)
    }
}

impl std::error::Error for ParseCodeError {}

impl FromStr for Code {
    type Err = ParseCodeError;
    fn from_str(value: &str) -> Result<Self, Self::Err> {
        let code = match value {
            "invalid_argument" => Self::InvalidArgument,
            "not_found" => Self::NotFound,
            "already_exists" => Self::AlreadyExists,
            "failed_precondition" => Self::FailedPrecondition,
            "aborted" => Self::Aborted,
            "out_of_range" => Self::OutOfRange,
            "unimplemented" => Self::Unimplemented,
            "internal" => Self::Internal,
            "unavailable" => Self::Unavailable,
            "data_loss" => Self::DataLoss,
            "unauthenticated" => Self::Unauthenticated,
            "permission_denied" => Self::PermissionDenied,
            "resource_exhausted" => Self::ResourceExhausted,
            "deadline_exceeded" => Self::DeadlineExceeded,
            "cancelled" => Self::Cancelled,
            "conflict" => Self::Conflict,
            other => return Err(ParseCodeError(other.to_owned())),
        };
        Ok(code)
    }
}
